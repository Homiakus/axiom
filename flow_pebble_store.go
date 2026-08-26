package axiom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	pebbledb "github.com/cockroachdb/pebble"
)

// PebbleFlowStore is the built-in crash-durable store for typed Flow state,
// append-only history, and durable effect outbox intents. Each
// SaveStateAndAppend call is committed as one synchronously flushed Pebble
// batch, satisfying DurableFlowStore.
type PebbleFlowStore struct {
	mu sync.Mutex
	db *pebbledb.DB
}

// OpenPebbleFlowStore opens a dedicated Pebble database for typed Flow state.
// Do not open the same directory concurrently through another Pebble handle.
func OpenPebbleFlowStore(path string) (*PebbleFlowStore, error) {
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		return nil, err
	}
	return &PebbleFlowStore{db: db}, nil
}

func (s *PebbleFlowStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

func (*PebbleFlowStore) Durability() StoreDurability { return StoreDurabilitySynchronous }
func (*PebbleFlowStore) AtomicFlowCommit()           {}

func (s *PebbleFlowStore) Load(ctx context.Context, flow, id string) ([]byte, []FlowHistoryEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, length, found, err := s.loadStateLocked(flow, id)
	if err != nil || !found {
		return state, nil, found, err
	}
	history, err := s.loadHistoryLocked(flow, id, length)
	return state, history, true, err
}

func (s *PebbleFlowStore) Save(ctx context.Context, flow, id string, state []byte, history []FlowHistoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := encodeFlowHistory(history, 0)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.db.NewBatch()
	defer batch.Close()

	prefix := flowHistoryPrefix(flow, id)
	if err := batch.DeleteRange([]byte(prefix), flowPrefixEnd(prefix), pebbledb.NoSync); err != nil {
		return err
	}
	if err := batch.Set([]byte(flowStateKey(flow, id)), append([]byte(nil), state...), pebbledb.NoSync); err != nil {
		return err
	}
	for i, data := range encoded {
		if err := batch.Set([]byte(flowHistoryKey(flow, id, i+1)), data, pebbledb.NoSync); err != nil {
			return err
		}
	}
	if err := batch.Set([]byte(flowLengthKey(flow, id)), []byte(strconv.Itoa(len(history))), pebbledb.NoSync); err != nil {
		return err
	}
	return batch.Commit(pebbledb.Sync)
}

func (s *PebbleFlowStore) LoadState(ctx context.Context, flow, id string) ([]byte, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadStateLocked(flow, id)
}

func (s *PebbleFlowStore) SaveStateAndAppend(ctx context.Context, flow, id string, state []byte, entries []FlowHistoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, currentLength, _, err := s.loadStateLocked(flow, id)
	if err != nil {
		return err
	}
	encoded, err := encodeFlowHistory(entries, currentLength)
	if err != nil {
		return err
	}

	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Set([]byte(flowStateKey(flow, id)), append([]byte(nil), state...), pebbledb.NoSync); err != nil {
		return err
	}
	for index, data := range encoded {
		seq := currentLength + index + 1
		if err := batch.Set([]byte(flowHistoryKey(flow, id, seq)), data, pebbledb.NoSync); err != nil {
			return err
		}
	}
	newLength := currentLength + len(entries)
	if err := batch.Set([]byte(flowLengthKey(flow, id)), []byte(strconv.Itoa(newLength)), pebbledb.NoSync); err != nil {
		return err
	}
	return batch.Commit(pebbledb.Sync)
}

func (s *PebbleFlowStore) LoadHistory(ctx context.Context, flow, id string) ([]FlowHistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, length, _, err := s.loadStateLocked(flow, id)
	if err != nil {
		return nil, err
	}
	return s.loadHistoryLocked(flow, id, length)
}

func (s *PebbleFlowStore) loadStateLocked(flow, id string) ([]byte, int, bool, error) {
	state, closer, err := s.db.Get([]byte(flowStateKey(flow, id)))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	stateCopy := append([]byte(nil), state...)
	if err := closer.Close(); err != nil {
		return nil, 0, false, err
	}

	lengthBytes, lengthCloser, err := s.db.Get([]byte(flowLengthKey(flow, id)))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return stateCopy, 0, true, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	lengthCopy := append([]byte(nil), lengthBytes...)
	if err := lengthCloser.Close(); err != nil {
		return nil, 0, false, err
	}
	length, err := strconv.Atoi(string(lengthCopy))
	if err != nil || length < 0 {
		return nil, 0, false, fmt.Errorf("axiom: invalid Pebble Flow history length %q", lengthCopy)
	}
	return stateCopy, length, true, nil
}

func (s *PebbleFlowStore) loadHistoryLocked(flow, id string, expectedLength int) ([]FlowHistoryEntry, error) {
	prefix := flowHistoryPrefix(flow, id)
	iter, err := s.db.NewIter(&pebbledb.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: flowPrefixEnd(prefix),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	history := make([]FlowHistoryEntry, 0, expectedLength)
	for iter.First(); iter.Valid(); iter.Next() {
		var entry FlowHistoryEntry
		if err := json.Unmarshal(iter.Value(), &entry); err != nil {
			return nil, err
		}
		want := len(history) + 1
		if entry.Sequence != want {
			return nil, fmt.Errorf("axiom: Pebble Flow history sequence %d, want %d", entry.Sequence, want)
		}
		history = append(history, entry)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	if len(history) != expectedLength {
		return nil, fmt.Errorf("axiom: Pebble Flow history length %d, metadata reports %d", len(history), expectedLength)
	}
	return history, nil
}

func encodeFlowHistory(entries []FlowHistoryEntry, currentLength int) ([][]byte, error) {
	encoded := make([][]byte, len(entries))
	for index, entry := range entries {
		want := currentLength + index + 1
		if entry.Sequence != want {
			return nil, fmt.Errorf("axiom: Flow history sequence %d, want %d", entry.Sequence, want)
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("axiom: encode Flow history sequence %d: %w", entry.Sequence, err)
		}
		encoded[index] = data
	}
	return encoded, nil
}

func flowKeyPart(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func flowRecordPrefix(flow, id string) string {
	return flowKeyPart(flow) + "/" + flowKeyPart(id)
}

func flowStateKey(flow, id string) string {
	return "flow/state/" + flowRecordPrefix(flow, id)
}

func flowLengthKey(flow, id string) string {
	return "flow/length/" + flowRecordPrefix(flow, id)
}

func flowHistoryPrefix(flow, id string) string {
	return "flow/history/" + flowRecordPrefix(flow, id) + "/"
}

func flowHistoryKey(flow, id string, sequence int) string {
	return fmt.Sprintf("%s%020d", flowHistoryPrefix(flow, id), sequence)
}

func flowPrefixEnd(prefix string) []byte {
	return append(append([]byte(nil), prefix...), 0xff)
}

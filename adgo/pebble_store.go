package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/syncx"
	pebbledb "github.com/cockroachdb/pebble"
)

type PebbleStoreOption func(*PebbleStore)

func WithPebbleNoSync() PebbleStoreOption {
	return func(store *PebbleStore) { store.syncWrites = false }
}

// PebbleStore is a high-throughput local durable backend. Every execution
// commit is stored twice in one atomic batch: immutable version snapshot + latest
// pointer value. Inbox events and execution catalog entries share the same DB.
// Pebble itself owns the process-level database lock, while Store operations are guarded
// by fine-grained per-execution locks and a catalog RWMutex to eliminate contention.
type PebbleStore struct {
	execLocks  *syncx.KeyedLocker
	catalogMu  sync.RWMutex
	db         *pebbledb.DB
	syncWrites bool
}

func OpenPebbleStore(path string, options ...PebbleStoreOption) (*PebbleStore, error) {
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		return nil, err
	}
	store := &PebbleStore{
		db:         db,
		syncWrites: true,
		execLocks:  syncx.NewKeyedLocker(),
	}
	for _, option := range options {
		option(store)
	}
	if err := store.ensureStoreFormat(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return store, nil
}

func (s *PebbleStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PebbleStore) writeOptions() *pebbledb.WriteOptions {
	if s.syncWrites {
		return pebbledb.Sync
	}
	return pebbledb.NoSync
}

func (s *PebbleStore) Create(ctx context.Context, execution *Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if execution == nil || execution.ID == "" {
		return fmt.Errorf("adgo: execution is required")
	}
	unlock := s.execLocks.Lock(execution.ID)
	defer unlock()

	if _, err := s.loadUnlocked(execution.ID); err == nil {
		return ErrExecutionExists
	} else if !errors.Is(err, ErrExecutionNotFound) {
		return err
	}
	copy, err := cloneExecution(execution)
	if err != nil {
		return err
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeExecutionBatch(batch, copy); err != nil {
		return err
	}
	if err := batch.Set(pebbleCatalogKey(copy.ID), []byte(copy.ID), nil); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *PebbleStore) Load(ctx context.Context, id string) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()
	return s.loadUnlocked(id)
}

func (s *PebbleStore) loadUnlocked(id string) (*Execution, error) {
	data, closer, err := s.db.Get(pebbleLatestKey(id))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	var execution Execution
	if err := json.Unmarshal(data, &execution); err != nil {
		return nil, err
	}
	ensureExecution(&execution)
	return &execution, nil
}

func (s *PebbleStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Execution) error) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()

	current, err := s.loadUnlocked(id)
	if err != nil {
		return nil, err
	}
	if current.Version != expected {
		return nil, ErrConflict
	}
	next, err := cloneExecution(current)
	if err != nil {
		return nil, err
	}
	if err := mutate(next); err != nil {
		return nil, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeExecutionBatch(batch, next); err != nil {
		return nil, err
	}
	if err := batch.Commit(s.writeOptions()); err != nil {
		return nil, err
	}
	return cloneExecution(next)
}

func (s *PebbleStore) writeExecutionBatch(batch *pebbledb.Batch, execution *Execution) error {
	data, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	if err := batch.Set(pebbleVersionKey(execution.ID, execution.Version), data, nil); err != nil {
		return err
	}
	return batch.Set(pebbleLatestKey(execution.ID), data, nil)
}

func (s *PebbleStore) PutInbox(ctx context.Context, id string, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()

	if _, err := s.loadUnlocked(id); err != nil {
		return err
	}
	if event.ID == "" {
		return fmt.Errorf("adgo: event id is required")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	key := pebbleInboxKey(id, event.ID)
	if _, closer, err := s.db.Get(key); err == nil {
		if closeErr := closer.Close(); closeErr != nil {
			return closeErr
		}
		return nil
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.db.Set(key, data, s.writeOptions())
}

func (s *PebbleStore) ListInbox(ctx context.Context, id string) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()

	if _, err := s.loadUnlocked(id); err != nil {
		return nil, err
	}
	prefix := pebbleInboxPrefix(id)
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixEnd(prefix)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := []Event{}
	for iter.First(); iter.Valid(); iter.Next() {
		var event Event
		if err := json.Unmarshal(iter.Value(), &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	sortEvents(out)
	return out, nil
}

func (s *PebbleStore) AckInbox(ctx context.Context, id string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()

	batch := s.db.NewBatch()
	defer batch.Close()
	for _, eventID := range ids {
		if err := batch.Delete(pebbleInboxKey(id, eventID), nil); err != nil {
			return err
		}
	}
	return batch.Commit(s.writeOptions())
}

func (s *PebbleStore) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.catalogMu.RLock()
	defer s.catalogMu.RUnlock()

	prefix := []byte("adgo/c/")
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixEnd(prefix)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	ids := []string{}
	for iter.First(); iter.Valid(); iter.Next() {
		ids = append(ids, string(append([]byte(nil), iter.Value()...)))
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *PebbleStore) ListVersions(ctx context.Context, id string) ([]*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock := s.execLocks.Lock(id)
	defer unlock()

	if _, err := s.loadUnlocked(id); err != nil {
		return nil, err
	}
	prefix := pebbleVersionPrefix(id)
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixEnd(prefix)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := []*Execution{}
	for iter.First(); iter.Valid(); iter.Next() {
		var execution Execution
		if err := json.Unmarshal(iter.Value(), &execution); err != nil {
			return nil, err
		}
		ensureExecution(&execution)
		out = append(out, &execution)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func pebbleExecutionHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
func pebbleExecutionPrefix(id string) []byte { return []byte("adgo/e/" + pebbleExecutionHash(id) + "/") }
func pebbleLatestKey(id string) []byte       { return append(pebbleExecutionPrefix(id), []byte("latest")...) }
func pebbleVersionPrefix(id string) []byte   { return append(pebbleExecutionPrefix(id), []byte("v/")...) }
func pebbleVersionKey(id string, version uint64) []byte {
	return append(pebbleVersionPrefix(id), []byte(fmt.Sprintf("%020d", version))...)
}
func pebbleInboxPrefix(id string) []byte { return append(pebbleExecutionPrefix(id), []byte("inbox/")...) }
func pebbleInboxKey(id, eventID string) []byte {
	sum := sha256.Sum256([]byte(eventID))
	return append(pebbleInboxPrefix(id), []byte(hex.EncodeToString(sum[:]))...)
}
func pebbleCatalogKey(id string) []byte { return []byte("adgo/c/" + pebbleExecutionHash(id)) }

func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

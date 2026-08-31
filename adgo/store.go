package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store interface {
	Create(context.Context, *Execution) error
	Load(context.Context, string) (*Execution, error)
	Commit(context.Context, string, uint64, func(*Execution) error) (*Execution, error)
	PutInbox(context.Context, string, Event) error
	ListInbox(context.Context, string) ([]Event, error)
	AckInbox(context.Context, string, []string) error
}

type MemoryStore struct {
	mu    sync.Mutex
	exec  map[string]*Execution
	inbox map[string]map[string]Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{exec: map[string]*Execution{}, inbox: map[string]map[string]Event{}}
}

func (s *MemoryStore) Create(ctx context.Context, e *Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.exec[e.ID]; ok {
		return ErrExecutionExists
	}
	c, err := cloneExecution(e)
	if err != nil {
		return err
	}
	s.exec[e.ID] = c
	return nil
}
func (s *MemoryStore) Load(ctx context.Context, id string) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.exec[id]
	if !ok {
		return nil, ErrExecutionNotFound
	}
	return cloneExecution(e)
}
func (s *MemoryStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Execution) error) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.exec[id]
	if !ok {
		return nil, ErrExecutionNotFound
	}
	if cur.Version != expected {
		return nil, ErrConflict
	}
	next, err := cloneExecution(cur)
	if err != nil {
		return nil, err
	}
	if err = mutate(next); err != nil {
		return nil, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	res, err := cloneExecution(next)
	if err != nil {
		return nil, err
	}
	s.exec[id] = next
	return res, nil
}
func (s *MemoryStore) PutInbox(ctx context.Context, id string, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.exec[id]; !ok {
		return ErrExecutionNotFound
	}
	if e.ID == "" {
		return fmt.Errorf("adgo: event id is required")
	}
	if s.inbox[id] == nil {
		s.inbox[id] = map[string]Event{}
	}
	if _, ok := s.inbox[id][e.ID]; !ok {
		s.inbox[id][e.ID] = e
	}
	return nil
}
func (s *MemoryStore) ListInbox(ctx context.Context, id string) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.exec[id]; !ok {
		return nil, ErrExecutionNotFound
	}
	out := make([]Event, 0, len(s.inbox[id]))
	for _, e := range s.inbox[id] {
		out = append(out, e)
	}
	sortEvents(out)
	return out, nil
}
func (s *MemoryStore) AckInbox(ctx context.Context, id string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, eid := range ids {
		delete(s.inbox[id], eid)
	}
	return nil
}

// FileStore is a crash-resilient local durable store. Each committed execution
// version is written as a complete immutable commit file using temp+fsync+rename.
// Inbox events are also immutable files and are acknowledged only after a state
// commit, so a crash can at worst cause safe event redelivery.
type FileStore struct {
	root           string
	mu             sync.Mutex
	lockStaleAfter time.Duration
}

func NewFileStore(root string) (*FileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: file store root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "executions"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		return nil, err
	}
	return &FileStore{root: root, lockStaleAfter: 30 * time.Second}, nil
}

func (s *FileStore) executionDir(id string) string {
	return filepath.Join(s.root, "executions", EncodeDurableName(id))
}
func (s *FileStore) commitsDir(id string) string { return filepath.Join(s.executionDir(id), "commits") }
func (s *FileStore) inboxDir(id string) string   { return filepath.Join(s.executionDir(id), "inbox") }

func (s *FileStore) Create(ctx context.Context, e *Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withExecutionLock(ctx, e.ID, func() error {
		dir := s.executionDir(e.ID)
		if _, err := os.Stat(dir); err == nil {
			return ErrExecutionExists
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(s.commitsDir(e.ID), 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(s.inboxDir(e.ID), 0o755); err != nil {
			return err
		}
		return s.writeCommit(e)
	})
}

func (s *FileStore) Load(ctx context.Context, id string) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(id)
}
func (s *FileStore) loadUnlocked(id string) (*Execution, error) {
	entries, err := os.ReadDir(s.commitsDir(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".json") {
			names = append(names, ent.Name())
		}
	}
	if len(names) == 0 {
		return nil, ErrExecutionNotFound
	}
	sort.Strings(names)
	data, err := os.ReadFile(filepath.Join(s.commitsDir(id), names[len(names)-1]))
	if err != nil {
		return nil, err
	}
	var e Execution
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("decode latest execution commit: %w", err)
	}
	ensureExecution(&e)
	return &e, nil
}
func (s *FileStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Execution) error) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result *Execution
	err := s.withExecutionLock(ctx, id, func() error {
		cur, err := s.loadUnlocked(id)
		if err != nil {
			return err
		}
		if cur.Version != expected {
			return ErrConflict
		}
		next, err := cloneExecution(cur)
		if err != nil {
			return err
		}
		if err := mutate(next); err != nil {
			return err
		}
		next.Version++
		next.UpdatedAt = time.Now().UTC()
		if err := s.writeCommit(next); err != nil {
			return err
		}
		result, err = cloneExecution(next)
		return err
	})
	return result, err
}

func (s *FileStore) writeCommit(e *Execution) error {
	dir := s.commitsDir(e.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%020d.json", e.Version)
	return atomicWrite(filepath.Join(dir, name), data)
}
func (s *FileStore) PutInbox(ctx context.Context, id string, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withExecutionLock(ctx, id, func() error {
		if _, err := os.Stat(s.executionDir(id)); errors.Is(err, fs.ErrNotExist) {
			return ErrExecutionNotFound
		} else if err != nil {
			return err
		}
		if e.ID == "" {
			return fmt.Errorf("adgo: event id is required")
		}
		if e.At.IsZero() {
			e.At = time.Now().UTC()
		}
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(s.inboxDir(id), 0o755); err != nil {
			return err
		}
		path := filepath.Join(s.inboxDir(id), EncodeDurableName(e.ID)+".json")
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return atomicWrite(path, data)
	})
}

func (s *FileStore) ListInbox(ctx context.Context, id string) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.inboxDir(id))
	if errors.Is(err, fs.ErrNotExist) {
		if _, x := os.Stat(s.executionDir(id)); errors.Is(x, fs.ErrNotExist) {
			return nil, ErrExecutionNotFound
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Event{}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.inboxDir(id), ent.Name()))
		if err != nil {
			return nil, err
		}
		var e Event
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("decode inbox event %s: %w", ent.Name(), err)
		}
		out = append(out, e)
	}
	sortEvents(out)
	return out, nil
}
func (s *FileStore) AckInbox(ctx context.Context, id string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withExecutionLock(ctx, id, func() error {
		for _, eid := range ids {
			err := os.Remove(filepath.Join(s.inboxDir(id), EncodeDurableName(eid)+".json"))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return syncDir(s.inboxDir(id))
	})
}

func (s *FileStore) withExecutionLock(ctx context.Context, id string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	locksDir := filepath.Join(s.root, "locks")
	path := filepath.Join(locksDir, EncodeDurableName(id)+".lock")
	owner, err := newFileLockOwner()
	if err != nil {
		return err
	}
	for {
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)
		if openErr == nil {
			record := fileLockRecord{Owner: owner, AcquiredAt: time.Now().UTC()}
			if err := writeFileLockRecord(f, record); err != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return err
			}
			if err := f.Sync(); err != nil {
				_ = f.Close()
				_ = releaseFileLock(path, owner)
				return err
			}
			if err := syncDir(locksDir); err != nil {
				_ = f.Close()
				_ = releaseFileLock(path, owner)
				return err
			}

			heartbeat := startFileLockHeartbeat(f, path, owner, s.lockStaleAfter)
			cleaned := false
			cleanup := func() error {
				heartbeatErr := heartbeat.Stop()
				closeErr := f.Close()
				releaseErr := releaseFileLock(path, owner)
				syncErr := syncDir(locksDir)
				if heartbeatErr != nil {
					return heartbeatErr
				}
				if closeErr != nil {
					return closeErr
				}
				if releaseErr != nil {
					return releaseErr
				}
				return syncErr
			}
			defer func() {
				if !cleaned {
					_ = cleanup()
				}
			}()

			fnErr := fn()
			cleanupErr := cleanup()
			cleaned = true
			if fnErr != nil {
				return fnErr
			}
			return cleanupErr
		}
		if !errors.Is(openErr, fs.ErrExist) {
			return openErr
		}
		removed, staleErr := removeStaleFileLock(path, s.lockStaleAfter)
		if staleErr != nil {
			return staleErr
		}
		if removed {
			_ = syncDir(locksDir)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return syncDir(dir)
}
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func safeName(value string) string {
	if value == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func sortEvents(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].At.Equal(events[j].At) {
			return events[i].ID < events[j].ID
		}
		return events[i].At.Before(events[j].At)
	})
}
func cloneExecution(e *Execution) (*Execution, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var out Execution
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	ensureExecution(&out)
	return &out, nil
}

func ensureExecution(e *Execution) {
	if e.Nodes == nil {
		e.Nodes = map[string]*NodeRuntime{}
	}
	if e.Data == nil {
		e.Data = map[string]json.RawMessage{}
	}
	if e.Artifacts == nil {
		e.Artifacts = map[string]ArtifactRef{}
	}
	if e.Quality == nil {
		e.Quality = QualityVector{}
	}
	if e.ActiveTasks == nil {
		e.ActiveTasks = map[string]TaskRuntime{}
	}
	if e.SeenEvents == nil {
		e.SeenEvents = map[string]bool{}
	}
	if e.RevisionCounters == nil {
		e.RevisionCounters = map[string]int{}
	}
	if e.StrategyBans == nil {
		e.StrategyBans = map[string]bool{}
	}
	if e.WaitingFor == nil {
		e.WaitingFor = map[string]string{}
	}
	if e.ThrottleUntil == nil {
		e.ThrottleUntil = map[string]time.Time{}
	}
}

package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pebbledb "github.com/cockroachdb/pebble"
)

type ExecutionDeletionStore interface {
	DeleteExecution(context.Context, string) error
}

type VersionPruner interface {
	PruneVersions(context.Context, string, int) (int, error)
}

type ArchiveFunc func(context.Context, *Execution) error

type RetentionPolicy struct {
	TerminalFor time.Duration
	Statuses    []ExecutionStatus
	Limit       int
	Archive     ArchiveFunc
}

type RetentionResult struct {
	Examined int      `json:"examined"`
	Deleted  []string `json:"deleted,omitempty"`
	Skipped  []string `json:"skipped,omitempty"`
}

// CollectExecutions removes only terminal executions that satisfy an explicit
// age/status policy. An Archive hook, when present, must succeed before delete.
func CollectExecutions(ctx context.Context, store Store, policy RetentionPolicy) (RetentionResult, error) {
	catalog, ok := store.(ExecutionCatalog)
	if !ok {
		return RetentionResult{}, fmt.Errorf("adgo: retention requires ExecutionCatalog")
	}
	deletion, ok := store.(ExecutionDeletionStore)
	if !ok {
		return RetentionResult{}, fmt.Errorf("adgo: store does not support execution deletion")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return RetentionResult{}, err
	}
	allowed := map[ExecutionStatus]struct{}{}
	if len(policy.Statuses) == 0 {
		allowed[StatusCompleted] = struct{}{}
		allowed[StatusFailed] = struct{}{}
		allowed[StatusCanceled] = struct{}{}
		allowed[StatusDeadlocked] = struct{}{}
	} else {
		for _, status := range policy.Statuses {
			if !terminal(status) {
				return RetentionResult{}, fmt.Errorf("adgo: retention status %s is not terminal", status)
			}
			allowed[status] = struct{}{}
		}
	}
	now := time.Now().UTC()
	result := RetentionResult{}
	for _, id := range ids {
		if policy.Limit > 0 && len(result.Deleted) >= policy.Limit {
			break
		}
		execution, err := store.Load(ctx, id)
		if err != nil {
			return result, err
		}
		result.Examined++
		if _, ok := allowed[execution.Status]; !ok || !terminal(execution.Status) {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if policy.TerminalFor > 0 && now.Sub(execution.UpdatedAt) < policy.TerminalFor {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if policy.Archive != nil {
			if err := policy.Archive(ctx, execution); err != nil {
				return result, fmt.Errorf("archive execution %s: %w", id, err)
			}
		}
		if err := deletion.DeleteExecution(ctx, id); err != nil {
			return result, err
		}
		result.Deleted = append(result.Deleted, id)
	}
	return result, nil
}

func (s *MemoryStore) DeleteExecution(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.exec[id]; !ok {
		return ErrExecutionNotFound
	}
	delete(s.exec, id)
	delete(s.inbox, id)
	return nil
}

func (s *FileStore) DeleteExecution(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withExecutionLock(ctx, id, func() error {
		root := s.executionDir(id)
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			return ErrExecutionNotFound
		} else if err != nil {
			return err
		}
		return os.RemoveAll(root)
	})
}

func (s *FileStore) PruneVersions(ctx context.Context, id string, keepLast int) (int, error) {
	if keepLast < 1 {
		return 0, fmt.Errorf("adgo: keepLast must be >= 1")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	err := s.withExecutionLock(ctx, id, func() error {
		commits := filepath.Join(s.executionDir(id), "commits")
		entries, err := os.ReadDir(commits)
		if errors.Is(err, fs.ErrNotExist) {
			return ErrExecutionNotFound
		}
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		if len(names) <= keepLast {
			return nil
		}
		for _, name := range names[:len(names)-keepLast] {
			if err := os.Remove(filepath.Join(commits, name)); err != nil {
				return err
			}
			removed++
		}
		return syncDir(commits)
	})
	return removed, err
}

func (s *PebbleStore) DeleteExecution(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.loadUnlocked(id); err != nil {
		return err
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	prefix := pebbleExecutionPrefix(id)
	if err := batch.DeleteRange(prefix, prefixEnd(prefix), nil); err != nil {
		return err
	}
	if err := batch.Delete(pebbleCatalogKey(id), nil); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *PebbleStore) PruneVersions(_ context.Context, id string, keepLast int) (int, error) {
	if keepLast < 1 {
		return 0, fmt.Errorf("adgo: keepLast must be >= 1")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.loadUnlocked(id); err != nil {
		return 0, err
	}
	prefix := pebbleVersionPrefix(id)
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: prefix, UpperBound: prefixEnd(prefix)})
	if err != nil {
		return 0, err
	}
	keys := [][]byte{}
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, append([]byte(nil), iter.Key()...))
	}
	if err := iter.Error(); err != nil {
		iter.Close()
		return 0, err
	}
	iter.Close()
	if len(keys) <= keepLast {
		return 0, nil
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	remove := keys[:len(keys)-keepLast]
	for _, key := range remove {
		if err := batch.Delete(key, nil); err != nil {
			return 0, err
		}
	}
	if err := batch.Commit(s.writeOptions()); err != nil {
		return 0, err
	}
	return len(remove), nil
}

// JSONArchive writes one final execution snapshot to the supplied callback. It
// is a small helper for object-storage adapters that want canonical JSON bytes.
func JSONArchive(write func(context.Context, string, []byte) error) ArchiveFunc {
	return func(ctx context.Context, execution *Execution) error {
		if write == nil {
			return fmt.Errorf("adgo: archive writer is nil")
		}
		data, err := json.MarshalIndent(execution, "", "  ")
		if err != nil {
			return err
		}
		return write(ctx, execution.ID, data)
	}
}

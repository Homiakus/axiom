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
	"sync"
	"time"
)

var (
	ErrScheduleNotFound = errors.New("adgo: schedule not found")
	ErrScheduleExists   = errors.New("adgo: schedule already exists")
)

// Schedule describes a durable fixed-interval trigger. Deterministic execution
// ids make each firing idempotent even if a scheduler crashes between starting a
// workflow and advancing the schedule cursor.
type Schedule struct {
	ID          string         `json:"id"`
	PlanDigest  string         `json:"planDigest"`
	Every       time.Duration  `json:"every"`
	StartAt     time.Time      `json:"startAt"`
	NextAt      time.Time      `json:"nextAt"`
	LastFiredAt time.Time      `json:"lastFiredAt,omitempty"`
	Enabled     bool           `json:"enabled"`
	CatchUp     bool           `json:"catchUp"`
	MaxCatchUp  int            `json:"maxCatchUp,omitempty"`
	Initial     map[string]any `json:"initial,omitempty"`
	Budget      BudgetLimit    `json:"budget"`
	Version     uint64         `json:"version"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type ScheduleStore interface {
	Create(context.Context, *Schedule) error
	Load(context.Context, string) (*Schedule, error)
	List(context.Context) ([]*Schedule, error)
	Commit(context.Context, string, uint64, func(*Schedule) error) (*Schedule, error)
}

type MemoryScheduleStore struct {
	mu        sync.Mutex
	schedules map[string]*Schedule
}

func NewMemoryScheduleStore() *MemoryScheduleStore {
	return &MemoryScheduleStore{schedules: map[string]*Schedule{}}
}

func (s *MemoryScheduleStore) Create(_ context.Context, schedule *Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.schedules[schedule.ID]; exists {
		return ErrScheduleExists
	}
	copy, err := cloneSchedule(schedule)
	if err != nil {
		return err
	}
	s.schedules[schedule.ID] = copy
	return nil
}

func (s *MemoryScheduleStore) Load(_ context.Context, id string) (*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	schedule := s.schedules[id]
	if schedule == nil {
		return nil, ErrScheduleNotFound
	}
	return cloneSchedule(schedule)
}

func (s *MemoryScheduleStore) List(_ context.Context) ([]*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.schedules))
	for id := range s.schedules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Schedule, 0, len(ids))
	for _, id := range ids {
		copy, err := cloneSchedule(s.schedules[id])
		if err != nil {
			return nil, err
		}
		out = append(out, copy)
	}
	return out, nil
}

func (s *MemoryScheduleStore) Commit(_ context.Context, id string, expected uint64, mutate func(*Schedule) error) (*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.schedules[id]
	if current == nil {
		return nil, ErrScheduleNotFound
	}
	if current.Version != expected {
		return nil, ErrConflict
	}
	next, err := cloneSchedule(current)
	if err != nil {
		return nil, err
	}
	if err := mutate(next); err != nil {
		return nil, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	res, err := cloneSchedule(next)
	if err != nil {
		return nil, err
	}
	s.schedules[id] = next
	return res, nil
}

type FileScheduleStore struct {
	root           string
	mu             sync.Mutex
	lockStaleAfter time.Duration
}

func NewFileScheduleStore(root string) (*FileScheduleStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: schedule store root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "schedules"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		return nil, err
	}
	return &FileScheduleStore{root: root, lockStaleAfter: 30 * time.Second}, nil
}

func (s *FileScheduleStore) path(id string) string {
	return filepath.Join(s.root, "schedules", safeName(id)+".json")
}

func (s *FileScheduleStore) Create(ctx context.Context, schedule *Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withLock(ctx, schedule.ID, func() error {
		path := s.path(schedule.ID)
		if _, err := os.Stat(path); err == nil {
			return ErrScheduleExists
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		data, err := json.MarshalIndent(schedule, "", "  ")
		if err != nil {
			return err
		}
		return atomicWrite(path, data)
	})
}

func (s *FileScheduleStore) Load(_ context.Context, id string) (*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(id)
}

func (s *FileScheduleStore) loadUnlocked(id string) (*Schedule, error) {
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrScheduleNotFound
	}
	if err != nil {
		return nil, err
	}
	var schedule Schedule
	if err := json.Unmarshal(data, &schedule); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (s *FileScheduleStore) List(_ context.Context) ([]*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "schedules"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]*Schedule, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "schedules", entry.Name()))
		if err != nil {
			return nil, err
		}
		var schedule Schedule
		if err := json.Unmarshal(data, &schedule); err != nil {
			return nil, err
		}
		out = append(out, &schedule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *FileScheduleStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Schedule) error) (*Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result *Schedule
	err := s.withLock(ctx, id, func() error {
		current, err := s.loadUnlocked(id)
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrConflict
		}
		next, err := cloneSchedule(current)
		if err != nil {
			return err
		}
		if err := mutate(next); err != nil {
			return err
		}
		next.Version++
		next.UpdatedAt = time.Now().UTC()
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicWrite(s.path(id), data); err != nil {
			return err
		}
		result, err = cloneSchedule(next)
		return err
	})
	return result, err
}

func (s *FileScheduleStore) withLock(ctx context.Context, id string, fn func() error) error {
	lockDir := filepath.Join(s.root, "locks")
	path := filepath.Join(lockDir, "schedule-"+safeName(id)+".lock")
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", time.Now().UTC().UnixNano())
			_ = file.Sync()
			_ = file.Close()
			defer func() { _ = os.Remove(path); _ = syncDir(lockDir) }()
			return fn()
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > s.lockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type ScheduleRunner struct {
	engine   *Engine
	store    ScheduleStore
	interval time.Duration
}

func NewScheduleRunner(engine *Engine, store ScheduleStore) (*ScheduleRunner, error) {
	if engine == nil || store == nil {
		return nil, fmt.Errorf("adgo: engine and schedule store are required")
	}
	return &ScheduleRunner{engine: engine, store: store, interval: time.Second}, nil
}

func (r *ScheduleRunner) Register(ctx context.Context, schedule Schedule) (*Schedule, error) {
	if strings.TrimSpace(schedule.ID) == "" {
		return nil, fmt.Errorf("adgo: schedule id is required")
	}
	if schedule.Every <= 0 {
		return nil, fmt.Errorf("adgo: schedule interval must be positive")
	}
	now := time.Now().UTC()
	if schedule.StartAt.IsZero() {
		schedule.StartAt = now
	}
	if schedule.NextAt.IsZero() {
		schedule.NextAt = schedule.StartAt
	}
	if schedule.MaxCatchUp <= 0 {
		schedule.MaxCatchUp = 100
	}
	schedule.PlanDigest = r.engine.plan.Digest
	schedule.Enabled = true
	schedule.Version = 1
	schedule.CreatedAt = now
	schedule.UpdatedAt = now
	if err := r.store.Create(ctx, &schedule); err != nil {
		return nil, err
	}
	return r.store.Load(ctx, schedule.ID)
}

func (r *ScheduleRunner) Tick(ctx context.Context, now time.Time) ([]string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	schedules, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	started := []string{}
	for _, schedule := range schedules {
		if !schedule.Enabled || schedule.PlanDigest != r.engine.plan.Digest || schedule.NextAt.After(now) {
			continue
		}
		count := 0
		for !schedule.NextAt.After(now) {
			fireAt := schedule.NextAt
			executionID := scheduledExecutionID(schedule.ID, fireAt)
			if _, err := r.engine.StartOrLoad(ctx, executionID, schedule.Initial, schedule.Budget); err != nil {
				return started, err
			}
			started = append(started, executionID)
			if _, err := r.engine.Advance(ctx, executionID); err != nil && !errors.Is(err, ErrDeadlock) {
				return started, err
			}
			updated, err := r.commitSchedule(ctx, schedule.ID, func(current *Schedule) error {
				if current.NextAt.After(fireAt) {
					// Already advanced by another runner; idempotent success
					return nil
				}
				if !current.NextAt.Equal(fireAt) {
					return ErrConflict
				}
				if current.Every <= 0 {
					return fmt.Errorf("adgo: invalid non-positive schedule interval")
				}
				current.LastFiredAt = fireAt
				current.NextAt = fireAt.Add(current.Every)
				if !current.CatchUp && !current.NextAt.After(now) {
					diff := now.Sub(current.NextAt)
					if diff > 0 {
						missed := int64(diff/current.Every) + 1
						if missed > 1000000 {
							missed = 1000000
						}
						current.NextAt = current.NextAt.Add(time.Duration(missed) * current.Every)
					}
				}
				return nil
			})
			if err != nil {
				return started, err
			}
			schedule = updated
			count++
			if !schedule.CatchUp || count >= schedule.MaxCatchUp {
				break
			}
		}
	}
	return started, nil
}

func (r *ScheduleRunner) Run(ctx context.Context) error {
	for {
		if _, err := r.Tick(ctx, time.Time{}); err != nil {
			return err
		}
		if err := sleepContext(ctx, r.interval); err != nil {
			return err
		}
	}
}

func (r *ScheduleRunner) commitSchedule(ctx context.Context, id string, mutate func(*Schedule) error) (*Schedule, error) {
	for attempt := 0; attempt < 8; attempt++ {
		current, err := r.store.Load(ctx, id)
		if err != nil {
			return nil, err
		}
		next, err := r.store.Commit(ctx, id, current.Version, mutate)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return next, err
	}
	return nil, ErrConflict
}

func scheduledExecutionID(scheduleID string, at time.Time) string {
	return fmt.Sprintf("schedule-%s-%d", safeName(scheduleID), at.UTC().UnixNano())
}

func cloneSchedule(schedule *Schedule) (*Schedule, error) {
	data, err := json.Marshal(schedule)
	if err != nil {
		return nil, err
	}
	var copy Schedule
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

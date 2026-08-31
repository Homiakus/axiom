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

var ErrProviderHealthNotFound = errors.New("adgo: provider health not found")

// ProviderHealthStore makes adaptive routing state durable and shareable across
// coordinator processes. Update must be atomic for one capability/provider key.
type ProviderHealthStore interface {
	LoadProviderHealth(context.Context, string, string) (ProviderHealth, error)
	UpdateProviderHealth(context.Context, string, string, func(*ProviderHealth)) (ProviderHealth, error)
	ListProviderHealth(context.Context) ([]ProviderHealth, error)
	DeleteProviderHealth(context.Context, string, string) error
}

type MemoryProviderHealthStore struct {
	mu     sync.Mutex
	health map[string]ProviderHealth
}

func NewMemoryProviderHealthStore() *MemoryProviderHealthStore {
	return &MemoryProviderHealthStore{health: map[string]ProviderHealth{}}
}

func (s *MemoryProviderHealthStore) LoadProviderHealth(_ context.Context, capability, provider string) (ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health, ok := s.health[providerHealthKey(capability, provider)]
	if !ok {
		return ProviderHealth{}, ErrProviderHealthNotFound
	}
	return health, nil
}

func (s *MemoryProviderHealthStore) UpdateProviderHealth(_ context.Context, capability, provider string, mutate func(*ProviderHealth)) (ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := providerHealthKey(capability, provider)
	health := s.health[key]
	if health.Capability == "" {
		health.Capability = capability
		health.Provider = provider
	}
	mutate(&health)
	s.health[key] = health
	return health, nil
}

func (s *MemoryProviderHealthStore) ListProviderHealth(_ context.Context) ([]ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProviderHealth, 0, len(s.health))
	for _, health := range s.health {
		out = append(out, health)
	}
	sortProviderHealth(out)
	return out, nil
}

func (s *MemoryProviderHealthStore) DeleteProviderHealth(_ context.Context, capability, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.health, providerHealthKey(capability, provider))
	return nil
}

type FileProviderHealthStore struct {
	root           string
	mu             sync.Mutex
	lockStaleAfter time.Duration
}

func NewFileProviderHealthStore(root string) (*FileProviderHealthStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: provider health store root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "provider-health"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		return nil, err
	}
	return &FileProviderHealthStore{root: root, lockStaleAfter: 30 * time.Second}, nil
}

func (s *FileProviderHealthStore) path(capability, provider string) string {
	return filepath.Join(s.root, "provider-health", safeName(capability)+"--"+safeName(provider)+".json")
}

func (s *FileProviderHealthStore) LoadProviderHealth(_ context.Context, capability, provider string) (ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(capability, provider)
}

func (s *FileProviderHealthStore) loadUnlocked(capability, provider string) (ProviderHealth, error) {
	data, err := os.ReadFile(s.path(capability, provider))
	if errors.Is(err, fs.ErrNotExist) {
		return ProviderHealth{}, ErrProviderHealthNotFound
	}
	if err != nil {
		return ProviderHealth{}, err
	}
	var health ProviderHealth
	if err := json.Unmarshal(data, &health); err != nil {
		return ProviderHealth{}, err
	}
	return health, nil
}

func (s *FileProviderHealthStore) UpdateProviderHealth(ctx context.Context, capability, provider string, mutate func(*ProviderHealth)) (ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result ProviderHealth
	err := s.withLock(ctx, capability, provider, func() error {
		health, err := s.loadUnlocked(capability, provider)
		if errors.Is(err, ErrProviderHealthNotFound) {
			health = ProviderHealth{Capability: capability, Provider: provider}
		} else if err != nil {
			return err
		}
		mutate(&health)
		data, err := json.MarshalIndent(health, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicWrite(s.path(capability, provider), data); err != nil {
			return err
		}
		result = health
		return nil
	})
	return result, err
}

func (s *FileProviderHealthStore) ListProviderHealth(_ context.Context) ([]ProviderHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "provider-health"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ProviderHealth, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "provider-health", entry.Name()))
		if err != nil {
			return nil, err
		}
		var health ProviderHealth
		if err := json.Unmarshal(data, &health); err != nil {
			return nil, err
		}
		out = append(out, health)
	}
	sortProviderHealth(out)
	return out, nil
}

func (s *FileProviderHealthStore) DeleteProviderHealth(ctx context.Context, capability, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withLock(ctx, capability, provider, func() error {
		err := os.Remove(s.path(capability, provider))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return syncDir(filepath.Join(s.root, "provider-health"))
	})
}

func (s *FileProviderHealthStore) withLock(ctx context.Context, capability, provider string, fn func() error) error {
	locks := filepath.Join(s.root, "locks")
	path := filepath.Join(locks, "provider-"+safeName(capability)+"--"+safeName(provider)+".lock")
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", time.Now().UTC().UnixNano())
			_ = file.Sync()
			_ = file.Close()
			defer func() { _ = os.Remove(path); _ = syncDir(locks) }()
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

func sortProviderHealth(values []ProviderHealth) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Capability == values[j].Capability {
			return values[i].Provider < values[j].Provider
		}
		return values[i].Capability < values[j].Capability
	})
}

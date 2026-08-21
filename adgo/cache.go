package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

var ErrCacheBusy = errors.New("adgo: activity cache computation in progress")

type CachedActivityResult struct {
	Key       string         `json:"key"`
	Result    ActivityResult `json:"result"`
	CreatedAt time.Time      `json:"createdAt"`
	ExpiresAt time.Time      `json:"expiresAt,omitempty"`
}

type CacheLease struct {
	Key       string    `json:"key"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ActivityCache interface {
	Get(context.Context, string) (*CachedActivityResult, error)
	Claim(context.Context, string, time.Duration) (CacheLease, error)
	Put(context.Context, CacheLease, ActivityResult, time.Duration) error
	Abort(context.Context, CacheLease) error
}

type CachePolicy struct {
	Namespace string
	TTL       time.Duration
	LeaseTTL  time.Duration
	Key       func(ActivityRequest) (string, error)
}

type memoryCacheSlot struct {
	Result *CachedActivityResult
	Lease  CacheLease
}

type MemoryActivityCache struct {
	mu    sync.Mutex
	slots map[string]*memoryCacheSlot
}

func NewMemoryActivityCache() *MemoryActivityCache {
	return &MemoryActivityCache{slots: map[string]*memoryCacheSlot{}}
}

func (c *MemoryActivityCache) Get(_ context.Context, key string) (*CachedActivityResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	slot := c.slots[key]
	if slot == nil || slot.Result == nil {
		return nil, nil
	}
	if !slot.Result.ExpiresAt.IsZero() && !slot.Result.ExpiresAt.After(time.Now().UTC()) {
		slot.Result = nil
		return nil, nil
	}
	return cloneCachedResult(slot.Result)
}

func (c *MemoryActivityCache) Claim(_ context.Context, key string, ttl time.Duration) (CacheLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	slot := c.slots[key]
	if slot == nil {
		slot = &memoryCacheSlot{}
		c.slots[key] = slot
	}
	if slot.Result != nil && (slot.Result.ExpiresAt.IsZero() || slot.Result.ExpiresAt.After(now)) {
		return CacheLease{}, ErrCacheBusy
	}
	if slot.Lease.Token != "" && slot.Lease.ExpiresAt.After(now) {
		return CacheLease{}, &CacheBusyError{RetryAfter: slot.Lease.ExpiresAt.Sub(now)}
	}
	lease := newCacheLease(key, ttl, now)
	slot.Lease = lease
	return lease, nil
}

func (c *MemoryActivityCache) Put(_ context.Context, lease CacheLease, result ActivityResult, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	slot := c.slots[lease.Key]
	if slot == nil || slot.Lease.Token != lease.Token || !slot.Lease.ExpiresAt.After(time.Now().UTC()) {
		return ErrStaleTask
	}
	now := time.Now().UTC()
	entry := &CachedActivityResult{Key: lease.Key, Result: result, CreatedAt: now}
	if ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
	}
	cloned, err := cloneCachedResult(entry)
	if err != nil {
		return err
	}
	slot.Result = cloned
	slot.Lease = CacheLease{}
	return nil
}

func (c *MemoryActivityCache) Abort(_ context.Context, lease CacheLease) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot := c.slots[lease.Key]; slot != nil && slot.Lease.Token == lease.Token {
		slot.Lease = CacheLease{}
	}
	return nil
}

type cacheFileRecord struct {
	Result *CachedActivityResult `json:"result,omitempty"`
	Lease  CacheLease            `json:"lease,omitempty"`
}

type FileActivityCache struct {
	root           string
	mu             sync.Mutex
	lockStaleAfter time.Duration
}

func NewFileActivityCache(root string) (*FileActivityCache, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: activity cache root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "activity-cache"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		return nil, err
	}
	return &FileActivityCache{root: root, lockStaleAfter: 30 * time.Second}, nil
}

func (c *FileActivityCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.root, "activity-cache", hex.EncodeToString(sum[:])+".json")
}

func (c *FileActivityCache) load(key string) (*cacheFileRecord, error) {
	data, err := os.ReadFile(c.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return &cacheFileRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var record cacheFileRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (c *FileActivityCache) write(key string, record *cacheFileRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(c.path(key), data)
}

func (c *FileActivityCache) Get(ctx context.Context, key string) (*CachedActivityResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result *CachedActivityResult
	err := c.withLock(ctx, key, func() error {
		record, err := c.load(key)
		if err != nil {
			return err
		}
		if record.Result == nil {
			return nil
		}
		if !record.Result.ExpiresAt.IsZero() && !record.Result.ExpiresAt.After(time.Now().UTC()) {
			record.Result = nil
			return c.write(key, record)
		}
		result, err = cloneCachedResult(record.Result)
		return err
	})
	return result, err
}

func (c *FileActivityCache) Claim(ctx context.Context, key string, ttl time.Duration) (CacheLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lease CacheLease
	err := c.withLock(ctx, key, func() error {
		record, err := c.load(key)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if record.Result != nil && (record.Result.ExpiresAt.IsZero() || record.Result.ExpiresAt.After(now)) {
			return ErrCacheBusy
		}
		if record.Lease.Token != "" && record.Lease.ExpiresAt.After(now) {
			return &CacheBusyError{RetryAfter: record.Lease.ExpiresAt.Sub(now)}
		}
		lease = newCacheLease(key, ttl, now)
		record.Lease = lease
		return c.write(key, record)
	})
	return lease, err
}

func (c *FileActivityCache) Put(ctx context.Context, lease CacheLease, result ActivityResult, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withLock(ctx, lease.Key, func() error {
		record, err := c.load(lease.Key)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if record.Lease.Token != lease.Token || !record.Lease.ExpiresAt.After(now) {
			return ErrStaleTask
		}
		entry := &CachedActivityResult{Key: lease.Key, Result: result, CreatedAt: now}
		if ttl > 0 {
			entry.ExpiresAt = now.Add(ttl)
		}
		record.Result = entry
		record.Lease = CacheLease{}
		return c.write(lease.Key, record)
	})
}

func (c *FileActivityCache) Abort(ctx context.Context, lease CacheLease) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withLock(ctx, lease.Key, func() error {
		record, err := c.load(lease.Key)
		if err != nil {
			return err
		}
		if record.Lease.Token == lease.Token {
			record.Lease = CacheLease{}
			return c.write(lease.Key, record)
		}
		return nil
	})
}

func (c *FileActivityCache) withLock(ctx context.Context, key string, fn func() error) error {
	locks := filepath.Join(c.root, "locks")
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(locks, "cache-"+hex.EncodeToString(sum[:12])+".lock")
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
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
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > c.lockStaleAfter {
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

type CacheBusyError struct{ RetryAfter time.Duration }
func (e *CacheBusyError) Error() string { return ErrCacheBusy.Error() }
func (e *CacheBusyError) Unwrap() error { return ErrCacheBusy }

func newCacheLease(key string, ttl time.Duration, now time.Time) CacheLease {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", key, now.UnixNano())))
	return CacheLease{Key: key, Token: "cache-" + hex.EncodeToString(sum[:12]), ExpiresAt: now.Add(ttl)}
}

func cloneCachedResult(result *CachedActivityResult) (*CachedActivityResult, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var copy CachedActivityResult
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

func DefaultActivityCacheKey(namespace, activity string, request ActivityRequest) (string, error) {
	hash := sha256.New()
	_, _ = hash.Write([]byte(namespace))
	_, _ = hash.Write([]byte("\x00" + activity + "\x00"))
	keys := make([]string, 0, len(request.Data))
	for key := range request.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(request.Data[key])
		_, _ = hash.Write([]byte{0})
	}
	artifactKeys := make([]string, 0, len(request.Artifacts))
	for key := range request.Artifacts {
		artifactKeys = append(artifactKeys, key)
	}
	sort.Strings(artifactKeys)
	for _, key := range artifactKeys {
		artifact := request.Artifacts[key]
		encoded, err := json.Marshal(artifact)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write(encoded)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// WithResultCache wraps a deterministic/pure activity. Do not apply it to an
// external side-effect node unless the cache key itself is part of the external
// idempotency contract and replaying a cached result is semantically valid.
func WithResultCache(cache ActivityCache, activity string, policy CachePolicy, handler ActivityHandler) ActivityHandler {
	return func(ctx context.Context, request ActivityRequest) (ActivityResult, error) {
		if cache == nil || handler == nil {
			return ActivityResult{}, fmt.Errorf("adgo: cache and handler are required")
		}
		keyFunc := policy.Key
		if keyFunc == nil {
			keyFunc = func(request ActivityRequest) (string, error) {
				return DefaultActivityCacheKey(policy.Namespace, activity, request)
			}
		}
		key, err := keyFunc(request)
		if err != nil {
			return ActivityResult{}, err
		}
		if cached, err := cache.Get(ctx, key); err != nil {
			return ActivityResult{}, err
		} else if cached != nil {
			result := cached.Result
			if result.Metrics == nil {
				result.Metrics = map[string]float64{}
			}
			result.Metrics["cache_hit"] = 1
			return result, nil
		}
		lease, err := cache.Claim(ctx, key, policy.LeaseTTL)
		if err != nil {
			var busy *CacheBusyError
			if errors.As(err, &busy) {
				return ActivityResult{}, RateLimited(busy.RetryAfter, err)
			}
			if errors.Is(err, ErrCacheBusy) {
				return ActivityResult{}, RateLimited(100*time.Millisecond, err)
			}
			return ActivityResult{}, err
		}
		result, callErr := handler(ctx, request)
		if callErr != nil {
			_ = cache.Abort(context.Background(), lease)
			return ActivityResult{}, callErr
		}
		if err := cache.Put(ctx, lease, result, policy.TTL); err != nil {
			return ActivityResult{}, err
		}
		if result.Metrics == nil {
			result.Metrics = map[string]float64{}
		}
		result.Metrics["cache_miss"] = 1
		return result, nil
	}
}

package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrAdmissionDenied = errors.New("adgo: admission denied")

type AdmissionPolicy struct {
	MaxConcurrent int           `json:"maxConcurrent,omitempty"`
	Rate          int           `json:"rate,omitempty"`
	Period        time.Duration `json:"period,omitempty"`
	Burst         int           `json:"burst,omitempty"`
}

type AdmissionLease struct {
	Key       string    `json:"key"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type AdmissionDeniedError struct {
	Key        string
	RetryAfter time.Duration
	Reason     string
}

func (e *AdmissionDeniedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s", ErrAdmissionDenied, e.Reason)
	}
	return ErrAdmissionDenied.Error()
}
func (e *AdmissionDeniedError) Unwrap() error { return ErrAdmissionDenied }

type AdmissionController interface {
	Acquire(context.Context, string, AdmissionPolicy, time.Duration) (AdmissionLease, error)
	Heartbeat(context.Context, AdmissionLease, time.Duration) (AdmissionLease, error)
	Release(context.Context, AdmissionLease) error
	Snapshot(context.Context, string) (AdmissionSnapshot, error)
}

type AdmissionSnapshot struct {
	Key        string    `json:"key"`
	InFlight   int       `json:"inFlight"`
	Tokens     float64   `json:"tokens"`
	LastRefill time.Time `json:"lastRefill"`
}

type admissionState struct {
	Key        string               `json:"key"`
	Tokens     float64              `json:"tokens"`
	LastRefill time.Time            `json:"lastRefill"`
	InFlight   map[string]time.Time `json:"inFlight,omitempty"`
}

type MemoryAdmissionController struct {
	mu     sync.Mutex
	states map[string]*admissionState
}

func NewMemoryAdmissionController() *MemoryAdmissionController {
	return &MemoryAdmissionController{states: map[string]*admissionState{}}
}

func (c *MemoryAdmissionController) Acquire(_ context.Context, key string, policy AdmissionPolicy, ttl time.Duration) (AdmissionLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[key]
	if state == nil {
		state = &admissionState{Key: key, InFlight: map[string]time.Time{}}
		c.states[key] = state
	}
	return acquireAdmission(state, key, policy, ttl, time.Now().UTC())
}

func (c *MemoryAdmissionController) Heartbeat(_ context.Context, lease AdmissionLease, ttl time.Duration) (AdmissionLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[lease.Key]
	if state == nil {
		return AdmissionLease{}, ErrStaleTask
	}
	return heartbeatAdmission(state, lease, ttl, time.Now().UTC())
}

func (c *MemoryAdmissionController) Release(_ context.Context, lease AdmissionLease) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[lease.Key]
	if state != nil {
		delete(state.InFlight, lease.Token)
	}
	return nil
}

func (c *MemoryAdmissionController) Snapshot(_ context.Context, key string) (AdmissionSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[key]
	if state == nil {
		return AdmissionSnapshot{Key: key}, nil
	}
	purgeAdmission(state, time.Now().UTC())
	return AdmissionSnapshot{Key: key, InFlight: len(state.InFlight), Tokens: state.Tokens, LastRefill: state.LastRefill}, nil
}

type FileAdmissionController struct {
	root           string
	mu             sync.Mutex
	lockStaleAfter time.Duration
}

func NewFileAdmissionController(root string) (*FileAdmissionController, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: admission store root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "admission"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "locks"), 0o755); err != nil {
		return nil, err
	}
	return &FileAdmissionController{root: root, lockStaleAfter: 30 * time.Second}, nil
}

func (c *FileAdmissionController) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.root, "admission", hex.EncodeToString(sum[:16])+".json")
}

func (c *FileAdmissionController) Acquire(ctx context.Context, key string, policy AdmissionPolicy, ttl time.Duration) (AdmissionLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lease AdmissionLease
	err := c.withLock(ctx, key, func() error {
		state, err := c.load(key)
		if err != nil {
			return err
		}
		lease, err = acquireAdmission(state, key, policy, ttl, time.Now().UTC())
		if err != nil {
			if persistErr := c.write(state); persistErr != nil {
				return persistErr
			}
			return err
		}
		return c.write(state)
	})
	return lease, err
}

func (c *FileAdmissionController) Heartbeat(ctx context.Context, lease AdmissionLease, ttl time.Duration) (AdmissionLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var next AdmissionLease
	err := c.withLock(ctx, lease.Key, func() error {
		state, err := c.load(lease.Key)
		if err != nil {
			return err
		}
		next, err = heartbeatAdmission(state, lease, ttl, time.Now().UTC())
		if err != nil {
			return err
		}
		return c.write(state)
	})
	return next, err
}

func (c *FileAdmissionController) Release(ctx context.Context, lease AdmissionLease) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withLock(ctx, lease.Key, func() error {
		state, err := c.load(lease.Key)
		if err != nil {
			return err
		}
		delete(state.InFlight, lease.Token)
		return c.write(state)
	})
}

func (c *FileAdmissionController) Snapshot(ctx context.Context, key string) (AdmissionSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var snapshot AdmissionSnapshot
	err := c.withLock(ctx, key, func() error {
		state, err := c.load(key)
		if err != nil {
			return err
		}
		purgeAdmission(state, time.Now().UTC())
		snapshot = AdmissionSnapshot{Key: key, InFlight: len(state.InFlight), Tokens: state.Tokens, LastRefill: state.LastRefill}
		return c.write(state)
	})
	return snapshot, err
}

func (c *FileAdmissionController) load(key string) (*admissionState, error) {
	data, err := os.ReadFile(c.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return &admissionState{Key: key, InFlight: map[string]time.Time{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var state admissionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.InFlight == nil {
		state.InFlight = map[string]time.Time{}
	}
	if state.Key == "" {
		state.Key = key
	}
	return &state, nil
}

func (c *FileAdmissionController) write(state *admissionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(c.path(state.Key), data)
}

func (c *FileAdmissionController) withLock(ctx context.Context, key string, fn func() error) error {
	locks := filepath.Join(c.root, "locks")
	sum := sha256.Sum256([]byte(key))
	name := "admission-" + hex.EncodeToString(sum[:12]) + ".lock"
	return withOwnedFileLock(ctx, locks, name, c.lockStaleAfter, fn)
}

func acquireAdmission(state *admissionState, key string, policy AdmissionPolicy, ttl time.Duration, now time.Time) (AdmissionLease, error) {
	if strings.TrimSpace(key) == "" {
		return AdmissionLease{}, fmt.Errorf("adgo: admission key is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if policy.Burst <= 0 && policy.Rate > 0 {
		policy.Burst = policy.Rate
	}
	if policy.Period <= 0 && policy.Rate > 0 {
		policy.Period = time.Second
	}
	if state.InFlight == nil {
		state.InFlight = map[string]time.Time{}
	}
	purgeAdmission(state, now)
	refillAdmission(state, policy, now)

	if policy.MaxConcurrent > 0 && len(state.InFlight) >= policy.MaxConcurrent {
		retry := soonestAdmissionExpiry(state, now)
		return AdmissionLease{}, &AdmissionDeniedError{Key: key, RetryAfter: retry, Reason: "concurrency limit reached"}
	}
	if policy.Rate > 0 && state.Tokens < 1 {
		perToken := float64(policy.Period) / float64(policy.Rate)
		retry := time.Duration(math.Ceil((1-state.Tokens)*perToken))
		if retry < time.Millisecond {
			retry = time.Millisecond
		}
		return AdmissionLease{}, &AdmissionDeniedError{Key: key, RetryAfter: retry, Reason: "rate limit reached"}
	}
	if policy.Rate > 0 {
		state.Tokens--
	}
	tokenHash := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", key, now.UnixNano(), len(state.InFlight))))
	lease := AdmissionLease{Key: key, Token: "permit-" + hex.EncodeToString(tokenHash[:12]), ExpiresAt: now.Add(ttl)}
	state.InFlight[lease.Token] = lease.ExpiresAt
	return lease, nil
}

func heartbeatAdmission(state *admissionState, lease AdmissionLease, ttl time.Duration, now time.Time) (AdmissionLease, error) {
	purgeAdmission(state, now)
	expires, ok := state.InFlight[lease.Token]
	if !ok || !expires.After(now) {
		return AdmissionLease{}, ErrStaleTask
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	lease.ExpiresAt = now.Add(ttl)
	state.InFlight[lease.Token] = lease.ExpiresAt
	return lease, nil
}

func purgeAdmission(state *admissionState, now time.Time) {
	for token, expires := range state.InFlight {
		if !expires.After(now) {
			delete(state.InFlight, token)
		}
	}
}

func refillAdmission(state *admissionState, policy AdmissionPolicy, now time.Time) {
	if policy.Rate <= 0 {
		return
	}
	burst := policy.Burst
	if burst <= 0 {
		burst = policy.Rate
	}
	if state.LastRefill.IsZero() {
		state.LastRefill = now
		state.Tokens = float64(burst)
		return
	}
	elapsed := now.Sub(state.LastRefill)
	if elapsed <= 0 {
		return
	}
	state.Tokens += float64(elapsed) * float64(policy.Rate) / float64(policy.Period)
	if state.Tokens > float64(burst) {
		state.Tokens = float64(burst)
	}
	state.LastRefill = now
}

func soonestAdmissionExpiry(state *admissionState, now time.Time) time.Duration {
	values := make([]time.Time, 0, len(state.InFlight))
	for _, expires := range state.InFlight {
		values = append(values, expires)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Before(values[j]) })
	if len(values) == 0 || !values[0].After(now) {
		return time.Millisecond
	}
	return values[0].Sub(now)
}

// WithAdmission wraps an activity with distributed concurrency/rate admission.
// Denial becomes FailureRateLimit so the normal durable retry path handles it.
func WithAdmission(controller AdmissionController, key string, policy AdmissionPolicy, ttl time.Duration, handler ActivityHandler) ActivityHandler {
	return func(ctx context.Context, request ActivityRequest) (ActivityResult, error) {
		if controller == nil || handler == nil {
			return ActivityResult{}, fmt.Errorf("adgo: admission controller and activity handler are required")
		}
		lease, err := controller.Acquire(ctx, key, policy, ttl)
		if err != nil {
			var denied *AdmissionDeniedError
			if errors.As(err, &denied) {
				return ActivityResult{}, RateLimited(denied.RetryAfter, err)
			}
			return ActivityResult{}, err
		}
		defer controller.Release(context.Background(), lease)
		return handler(ctx, request)
	}
}

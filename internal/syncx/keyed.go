// Package syncx contains small synchronization primitives shared by Axiom frontends.
package syncx

import "sync"

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// KeyedLocker serializes work for one key while allowing independent keys to run concurrently.
// Lock returns an idempotent-by-contract unlock function that must be called exactly once.
type KeyedLocker struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

func NewKeyedLocker() *KeyedLocker {
	return &KeyedLocker{locks: map[string]*keyedLock{}}
}

func (k *KeyedLocker) Lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*keyedLock{}
	}
	lock := k.locks[key]
	if lock == nil {
		lock = &keyedLock{}
		k.locks[key] = lock
	}
	lock.refs++
	k.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		k.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

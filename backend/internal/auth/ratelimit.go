package auth

import (
	"sync"
	"time"
)

// Limiter is an in-memory failure counter with exponential backoff, used to slow down password
// guessing. After threshold consecutive failures for a key, further attempts are refused for
// base·2^(failures−threshold), capped at max. A failure streak is forgotten after resetAfter
// without failures. State is per process, which is enough for the single-instance deployment.
type Limiter struct {
	mu         sync.Mutex
	entries    map[string]*limitEntry
	threshold  int
	base, max  time.Duration
	resetAfter time.Duration
	now        func() time.Time
}

type limitEntry struct {
	fails       int
	lastFailure time.Time
	lockedUntil time.Time
}

// maxLimiterEntries bounds memory: stale entries are pruned when the map grows beyond it.
const maxLimiterEntries = 10000

// NewLimiter creates a limiter.
func NewLimiter(threshold int, base, maxBackoff, resetAfter time.Duration) *Limiter {
	return &Limiter{
		entries:    make(map[string]*limitEntry),
		threshold:  threshold,
		base:       base,
		max:        maxBackoff,
		resetAfter: resetAfter,
		now:        time.Now,
	}
}

// RetryAfter reports how long key is still locked; zero means an attempt is allowed.
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0
	}
	now := l.now()
	if now.Sub(e.lastFailure) > l.resetAfter {
		delete(l.entries, key)
		return 0
	}
	if d := e.lockedUntil.Sub(now); d > 0 {
		return d
	}
	return 0
}

// Fail records a failed attempt for key.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.lastFailure) > l.resetAfter {
		if len(l.entries) >= maxLimiterEntries {
			l.pruneLocked(now)
		}
		e = &limitEntry{}
		l.entries[key] = e
	}
	e.fails++
	e.lastFailure = now
	if over := e.fails - l.threshold; over >= 0 {
		d := l.max
		if over < 30 {
			if b := l.base << over; b < l.max {
				d = b
			}
		}
		e.lockedUntil = now.Add(d)
	}
}

// Reset forgets the failures of key (after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) pruneLocked(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.lastFailure) > l.resetAfter || now.After(e.lockedUntil) && e.fails < l.threshold {
			delete(l.entries, k)
		}
	}
}

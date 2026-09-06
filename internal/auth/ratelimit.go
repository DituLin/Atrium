package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a bounded map of token-bucket limiters keyed by a string, usually
// a client IP. Idle keys are dropped so the map cannot grow without bound.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    rate.Limit
	burst   int
	maxKeys int
	ttl     time.Duration
	now     func() time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// LimiterOptions configures a Limiter.
type LimiterOptions struct {
	// PerMinute is the sustained allowance per key.
	PerMinute int
	// Burst defaults to PerMinute when zero.
	Burst int
	// MaxKeys bounds the map; the default is 4096.
	MaxKeys int
	// TTL evicts keys that have not been seen; the default is 10 minutes.
	TTL time.Duration
	// Now allows tests to control time.
	Now func() time.Time
}

// NewLimiter builds a rate limiter with the given allowance.
func NewLimiter(opts LimiterOptions) *Limiter {
	if opts.Burst <= 0 {
		opts.Burst = opts.PerMinute
	}
	if opts.MaxKeys <= 0 {
		opts.MaxKeys = 4096
	}
	if opts.TTL <= 0 {
		opts.TTL = 10 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate.Limit(float64(opts.PerMinute) / 60.0),
		burst:   opts.Burst,
		maxKeys: opts.MaxKeys,
		ttl:     opts.TTL,
		now:     opts.Now,
	}
}

// Allow consumes one token for key and reports whether the request may proceed.
func (l *Limiter) Allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		l.evictLocked(now)
		b = &bucket{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.limiter.AllowN(now, 1)
}

// evictLocked drops stale keys, and the oldest key when the map is full.
func (l *Limiter) evictLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.seen) > l.ttl {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < l.maxKeys {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, b := range l.buckets {
		if oldestKey == "" || b.seen.Before(oldest) {
			oldestKey, oldest = k, b.seen
		}
	}
	delete(l.buckets, oldestKey)
}

// Size reports how many keys are tracked; tests use it.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// FailureLimiter blocks a key for a cooldown once it exceeds an allowance of
// authentication failures (design §6.8: 10 failures/min per IP → 429 for 60 s).
type FailureLimiter struct {
	limiter  *Limiter
	mu       sync.Mutex
	blocked  map[string]time.Time
	cooldown time.Duration
	now      func() time.Time
}

// NewFailureLimiter builds the auth-failure limiter.
func NewFailureLimiter(perMinute int, cooldown time.Duration, now func() time.Time) *FailureLimiter {
	if now == nil {
		now = time.Now
	}
	return &FailureLimiter{
		limiter:  NewLimiter(LimiterOptions{PerMinute: perMinute, Burst: perMinute, Now: now}),
		blocked:  make(map[string]time.Time),
		cooldown: cooldown,
		now:      now,
	}
}

// Blocked reports whether the key is currently in its cooldown window.
func (f *FailureLimiter) Blocked(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	until, ok := f.blocked[key]
	if !ok {
		return false
	}
	if f.now().Before(until) {
		return true
	}
	delete(f.blocked, key)
	return false
}

// RecordFailure counts one failed authentication and reports whether the key is
// now blocked.
func (f *FailureLimiter) RecordFailure(key string) bool {
	if f.limiter.Allow(key) {
		return f.Blocked(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked[key] = f.now().Add(f.cooldown)
	return true
}

// RetryAfter returns the remaining cooldown for a blocked key.
func (f *FailureLimiter) RetryAfter(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	until, ok := f.blocked[key]
	if !ok {
		return 0
	}
	d := until.Sub(f.now())
	if d < 0 {
		return 0
	}
	return d
}

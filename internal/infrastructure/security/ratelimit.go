// Rate limiting, brute-force lockout, IP connection caps, and anti-cheat strike
// tracking for direct-VPS hardened WebSocket and HTTP auth endpoints.
package security

import (
	"sync"
	"time"
)

type LoginLimiter struct {
	mu           sync.Mutex
	attempts     map[string]*loginBucket
	softLimit    int
	hardLimit    int
	softLockout  time.Duration
	hardLockout  time.Duration
	windowPeriod time.Duration
}

type loginBucket struct {
	failures    int
	firstFailed time.Time
	lockedUntil time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		attempts:     make(map[string]*loginBucket),
		softLimit:    5,
		hardLimit:    15,
		softLockout:  1 * time.Minute,
		hardLockout:  15 * time.Minute,
		windowPeriod: 15 * time.Minute,
	}
}

func (l *LoginLimiter) Check(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.attempts[key]
	if !ok {
		return true, 0
	}
	now := time.Now()
	if now.Before(b.lockedUntil) {
		return false, b.lockedUntil.Sub(now)
	}
	if now.Sub(b.firstFailed) > l.windowPeriod {
		delete(l.attempts, key)
	}
	return true, 0
}

func (l *LoginLimiter) RecordFailure(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.attempts[key]
	if !ok || now.Sub(b.firstFailed) > l.windowPeriod {
		b = &loginBucket{firstFailed: now}
		l.attempts[key] = b
	}
	b.failures++
	switch {
	case b.failures >= l.hardLimit:
		b.lockedUntil = now.Add(l.hardLockout)
		return l.hardLockout
	case b.failures >= l.softLimit:
		b.lockedUntil = now.Add(l.softLockout)
		return l.softLockout
	default:
		return 0
	}
}

func (l *LoginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

type IPConnTracker struct {
	mu            sync.Mutex
	perIP         map[string]int
	total         int
	maxTotal      int
	maxPerIP      int
}

func NewIPConnTracker(maxTotal int, maxPerIP int) *IPConnTracker {
	if maxTotal <= 0 {
		maxTotal = 2000
	}
	if maxPerIP <= 0 {
		maxPerIP = 5
	}
	return &IPConnTracker{
		perIP:    make(map[string]int),
		maxTotal: maxTotal,
		maxPerIP: maxPerIP,
	}
}

func (t *IPConnTracker) Acquire(ip string) (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.total >= t.maxTotal {
		return false, "server connection limit reached"
	}
	if ip != "" && t.perIP[ip] >= t.maxPerIP {
		return false, "too many connections from this IP"
	}
	t.total++
	if ip != "" {
		t.perIP[ip]++
	}
	return true, ""
}

func (t *IPConnTracker) Release(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.total > 0 {
		t.total--
	}
	if ip != "" {
		if n := t.perIP[ip]; n <= 1 {
			delete(t.perIP, ip)
		} else {
			t.perIP[ip] = n - 1
		}
	}
}

func (t *IPConnTracker) Total() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

type TokenBucket struct {
	mu         sync.Mutex
	ratePerSec float64
	burst      float64
	tokens     float64
	lastRefill time.Time
}

func NewTokenBucket(ratePerSec float64, burst int) *TokenBucket {
	return &TokenBucket{
		ratePerSec: ratePerSec,
		burst:      float64(burst),
		tokens:     float64(burst),
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	if elapsed > 0 {
		tb.tokens += elapsed * tb.ratePerSec
		if tb.tokens > tb.burst {
			tb.tokens = tb.burst
		}
		tb.lastRefill = now
	}
	if tb.tokens < 1.0 {
		return false
	}
	tb.tokens -= 1.0
	return true
}

type StrikeTracker struct {
	mu        sync.Mutex
	strikes   int
	threshold int
	decay     time.Duration
	lastAt    time.Time
}

func NewStrikeTracker(threshold int, decay time.Duration) *StrikeTracker {
	if threshold <= 0 {
		threshold = 10
	}
	if decay <= 0 {
		decay = 2 * time.Minute
	}
	return &StrikeTracker{
		threshold: threshold,
		decay:     decay,
	}
}

func (s *StrikeTracker) Add(weight int) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if !s.lastAt.IsZero() && now.Sub(s.lastAt) > s.decay {
		s.strikes = 0
	}
	s.lastAt = now
	s.strikes += weight
	return s.strikes, s.strikes >= s.threshold
}

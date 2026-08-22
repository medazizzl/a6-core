package auth
import (
	"sync"
	"time"
)

const (
	rateLimitWindow = time.Minute
	rateLimitMax    = 5
)

type RateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{attempts: make(map[string][]time.Time)}
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	kept := r.attempts[key][:0]
	for _, t := range r.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rateLimitMax {
		r.attempts[key] = kept
		return false
	}
	r.attempts[key] = append(kept, now)
	return true
}

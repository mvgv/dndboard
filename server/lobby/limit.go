package lobby

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a token bucket per key: a burst of requests, refilled at one
// per interval.
type RateLimiter struct {
	burst    float64
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows burst requests at once per key, refilling one every
// interval. now may be nil for the real clock.
func NewRateLimiter(burst int, interval time.Duration, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{burst: float64(burst), interval: interval, now: now, buckets: map[string]*bucket{}}
}

// maxBuckets bounds memory against many distinct clients.
const maxBuckets = 10_000

// Allow reports whether key may make a request now.
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	b, ok := r.buckets[key]
	if !ok {
		if len(r.buckets) >= maxBuckets {
			for k, o := range r.buckets { // drop buckets that have refilled
				if o.tokens+float64(now.Sub(o.last))/float64(r.interval) >= r.burst {
					delete(r.buckets, k)
				}
			}
		}
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	b.tokens = min(r.burst, b.tokens+float64(now.Sub(b.last))/float64(r.interval))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware answers 429 to a client address that is over its limit.
func (r *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		host, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			host = req.RemoteAddr
		}
		if !r.Allow(host) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, req)
	})
}

package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter tracks and enforces rate limits over a sliding window.
type RateLimiter interface {
	Allow(key string) (allowed bool, remaining int, resetAt time.Time)
}

type MemoryRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	count     int
	resetTime time.Time
}

func NewMemoryRateLimiter(limit int, window time.Duration) *MemoryRateLimiter {
	limiter := &MemoryRateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*bucket),
	}
	go limiter.cleanupLoop()
	return limiter
}

func (l *MemoryRateLimiter) Allow(key string) (bool, int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	b, exists := l.buckets[key]

	if !exists || now.After(b.resetTime) {
		resetTime := now.Add(l.window)
		l.buckets[key] = &bucket{
			count:     1,
			resetTime: resetTime,
		}
		return true, l.limit - 1, resetTime
	}

	if b.count >= l.limit {
		return false, 0, b.resetTime
	}

	b.count++
	remaining := l.limit - b.count
	return true, remaining, b.resetTime
}

func (l *MemoryRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.window * 2)
	for now := range ticker.C {
		l.mu.Lock()
		for k, b := range l.buckets {
			if now.After(b.resetTime) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

// RateLimitMiddleware returns a Chi/net/http middleware that enforces rate limiting.
func RateLimitMiddleware(limiter RateLimiter, limit int, window time.Duration, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = defaultRateLimitKey
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			allowed, remaining, resetAt := limiter.Allow(key)

			resetUnix := resetAt.Unix()
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetUnix, 10))

			if !allowed {
				retryAfter := int64(time.Until(resetAt).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func defaultRateLimitKey(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "global"
}

func FormatRateLimitKey(prefix, identifier string) string {
	return fmt.Sprintf("%s:%s", prefix, identifier)
}

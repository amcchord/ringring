package webapp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	windowStart time.Time
	count       int
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
	now     func() time.Time
}

func newRateLimiter(now func() time.Time) *rateLimiter {
	return &rateLimiter{buckets: make(map[string]rateBucket), now: now}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	bucket := l.buckets[key]
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= window {
		bucket = rateBucket{windowStart: now}
	}
	bucket.count++
	l.buckets[key] = bucket
	if len(l.buckets) > 10_000 {
		for candidate, value := range l.buckets {
			if now.Sub(value.windowStart) > 2*window {
				delete(l.buckets, candidate)
			}
		}
	}
	return bucket.count <= limit
}

func (a *App) rateLimit(next http.Handler) http.Handler {
	limiter := newRateLimiter(func() time.Time { return a.now() })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category, limit, window := rateCategory(r)
		if category != "" {
			key := category + ":" + a.clientIP(r)
			if !limiter.allow(key, limit, window) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many rings; please wait a moment", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func rateCategory(r *http.Request) (string, int, time.Duration) {
	switch {
	case r.URL.Path == "/signup" || r.URL.Path == "/login" || r.URL.Path == "/recover":
		return "native-auth", 20, 5 * time.Minute
	case strings.HasPrefix(r.URL.Path, "/auth/"):
		return "auth", 30, 5 * time.Minute
	case strings.HasPrefix(r.URL.Path, "/join/"):
		return "join", 60, 5 * time.Minute
	case r.Method == http.MethodPost && (r.URL.Path == "/parties" || strings.HasSuffix(r.URL.Path, "/invites") || strings.HasSuffix(r.URL.Path, "/services") || strings.Contains(r.URL.Path, "/devices/")):
		return "party-write", 30, 5 * time.Minute
	default:
		return "", 0, 0
	}
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "172.31.88.30" {
		forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if net.ParseIP(forwarded) != nil {
			return forwarded
		}
	}
	if net.ParseIP(host) == nil {
		return "unknown"
	}
	return host
}

package webapp

import (
	"testing"
	"time"
)

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(func() time.Time { return now })
	if !limiter.allow("join:192.0.2.1", 2, time.Minute) || !limiter.allow("join:192.0.2.1", 2, time.Minute) {
		t.Fatal("requests inside the limit should pass")
	}
	if limiter.allow("join:192.0.2.1", 2, time.Minute) {
		t.Fatal("request above the limit should fail")
	}
	now = now.Add(time.Minute)
	if !limiter.allow("join:192.0.2.1", 2, time.Minute) {
		t.Fatal("new window should reset the bucket")
	}
}

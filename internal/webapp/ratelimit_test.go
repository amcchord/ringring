package webapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/config"
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

func TestClientIPTrustsOnlyTheCaddyPeer(t *testing.T) {
	app := &App{cfg: config.Config{Environment: "production"}}
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{
			name:       "direct attacker cannot spoof forwarding header",
			remoteAddr: "198.51.100.20:4567",
			forwarded:  "203.0.113.9",
			want:       "198.51.100.20",
		},
		{
			name:       "fixed Caddy peer supplies client address",
			remoteAddr: "172.31.88.30:4567",
			forwarded:  "203.0.113.9",
			want:       "203.0.113.9",
		},
		{
			name:       "first Caddy forwarding address is the client",
			remoteAddr: "172.31.88.30:4567",
			forwarded:  "203.0.113.9, 172.31.88.1",
			want:       "203.0.113.9",
		},
		{
			name:       "malformed Caddy forwarding header falls back to peer",
			remoteAddr: "172.31.88.30:4567",
			forwarded:  "not-an-address",
			want:       "172.31.88.30",
		},
		{
			name:       "IPv6 client is retained",
			remoteAddr: "172.31.88.30:4567",
			forwarded:  "2001:db8::25",
			want:       "2001:db8::25",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://ringring.live/login", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.forwarded)
			if got := app.clientIP(request); got != test.want {
				t.Fatalf("client IP = %q, want %q", got, test.want)
			}
		})
	}
}

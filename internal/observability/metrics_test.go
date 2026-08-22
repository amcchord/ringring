package observability

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestErrorClassNeverReturnsErrorText(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{nil, "none"},
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "timeout"},
		{errors.New("private-party-value"), "internal"},
		{&net.DNSError{IsTimeout: true, Name: "private-host.invalid"}, "timeout"},
	} {
		got := ErrorClass(test.err)
		if got != test.want || strings.Contains(got, "private") {
			t.Fatalf("ErrorClass(%v)=%q want=%q", test.err, got, test.want)
		}
	}
}

func TestMetricsHandlerRendersBoundedAggregates(t *testing.T) {
	registry := New()
	registry.HTTPStarted()
	registry.HTTPFinished("host", http.MethodPost, http.StatusSeeOther, 75*time.Millisecond)
	registry.HTTPStarted()
	registry.HTTPFinished("private-party-id", "PURGE", 999, -time.Second)
	registry.ObserveReconciliation(true)
	registry.ObserveReconciliation(false)
	registry.ObserveVoice("ai_bridge", "completed")
	registry.ObserveVoice("caller-supplied-value", "caller-result")
	registry.SetAIActive(2)

	handler := registry.Handler(func(context.Context) HealthSnapshot {
		return HealthSnapshot{
			DatabaseUp: true, AsteriskAMIUp: true, ReachableContacts: 2,
			UnreachableContacts: 1, NonQualifiedContacts: 3, UnknownContacts: -1,
		}
	})
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("unexpected metrics response: status=%d headers=%v", response.Code, response.Header())
	}
	for _, expected := range []string{
		"ringring_database_up 1",
		"ringring_asterisk_ami_up 1",
		"ringring_sip_contacts{state=\"reachable\"} 2",
		"ringring_sip_contacts{state=\"unknown\"} 0",
		"ringring_http_requests_total{surface=\"host\",method=\"POST\",status_class=\"3xx\"} 1",
		"ringring_http_request_duration_seconds_bucket{surface=\"host\",method=\"POST\",status_class=\"3xx\",le=\"0.1\"} 1",
		"ringring_http_requests_total{surface=\"other\",method=\"OTHER\",status_class=\"other\"} 1",
		"ringring_telephony_reconciliations_total{result=\"success\"} 1",
		"ringring_telephony_reconciliations_total{result=\"error\"} 1",
		"ringring_voice_service_requests_total{service=\"ai_bridge\",result=\"completed\"} 1",
		"ringring_voice_service_requests_total{service=\"other\",result=\"error\"} 1",
		"ringring_ai_calls_active 2",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics omitted %q\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"private-party-id", "caller-supplied-value", "caller-result"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics exposed caller-controlled label %q", forbidden)
		}
	}
}

func TestMetricsHandlerIsNarrowAndConcurrentSafe(t *testing.T) {
	registry := New()
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for request := 0; request < 100; request++ {
				registry.HTTPStarted()
				registry.HTTPFinished("public", http.MethodGet, http.StatusOK, time.Millisecond)
				registry.ObserveVoice("weather", "ready")
			}
		}()
	}
	workers.Wait()
	handler := registry.Handler(nil)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(response.Body.String(), "ringring_http_requests_total{surface=\"public\",method=\"GET\",status_class=\"2xx\"} 2000") {
		t.Fatal("concurrent HTTP observations were lost")
	}
	if !strings.Contains(response.Body.String(), "ringring_voice_service_requests_total{service=\"weather\",result=\"ready\"} 2000") {
		t.Fatal("concurrent voice observations were lost")
	}

	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/other", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.path, response.Code, test.status)
		}
	}
}

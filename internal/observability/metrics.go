package observability

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var requestDurationBuckets = [...]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type httpKey struct {
	surface     string
	method      string
	statusClass string
}

type httpValue struct {
	count   uint64
	sum     float64
	buckets [len(requestDurationBuckets)]uint64
}

type voiceKey struct {
	service string
	result  string
}

// Registry keeps process-lifetime aggregate counters only. Its label values
// are reduced through fixed allowlists so caller-controlled values and family
// record identifiers cannot create time series or appear in a scrape.
type Registry struct {
	mu              sync.Mutex
	startedAt       time.Time
	httpInFlight    int64
	http            map[httpKey]httpValue
	reconciliations map[string]uint64
	voice           map[voiceKey]uint64
}

type HealthSnapshot struct {
	DatabaseUp           bool
	AsteriskAMIUp        bool
	ReachableContacts    int
	UnreachableContacts  int
	NonQualifiedContacts int
	UnknownContacts      int
}

type HealthSnapshotFunc func(context.Context) HealthSnapshot

func New() *Registry {
	return &Registry{
		startedAt:       time.Now(),
		http:            make(map[httpKey]httpValue),
		reconciliations: make(map[string]uint64),
		voice:           make(map[voiceKey]uint64),
	}
}

// ErrorClass reduces an operational error to a fixed, non-sensitive category.
// Logs use the surrounding event message for operation context instead of
// serializing error strings that may contain a path or provider response.
func ErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, fs.ErrPermission):
		return "permission"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "internal"
}

func (r *Registry) HTTPStarted() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.httpInFlight++
	r.mu.Unlock()
}

func (r *Registry) HTTPFinished(surface, method string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	key := httpKey{
		surface:     normalizeSurface(surface),
		method:      normalizeMethod(method),
		statusClass: normalizeStatus(status),
	}
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	r.mu.Lock()
	if r.httpInFlight > 0 {
		r.httpInFlight--
	}
	value := r.http[key]
	value.count++
	value.sum += seconds
	for index, upperBound := range requestDurationBuckets {
		if seconds <= upperBound {
			value.buckets[index]++
		}
	}
	r.http[key] = value
	r.mu.Unlock()
}

func (r *Registry) ObserveReconciliation(success bool) {
	if r == nil {
		return
	}
	result := "error"
	if success {
		result = "success"
	}
	r.mu.Lock()
	r.reconciliations[result]++
	r.mu.Unlock()
}

func (r *Registry) ObserveVoice(service, result string) {
	if r == nil {
		return
	}
	key := voiceKey{service: normalizeVoiceService(service), result: normalizeVoiceResult(result)}
	r.mu.Lock()
	r.voice[key]++
	r.mu.Unlock()
}

func (r *Registry) Handler(snapshot HealthSnapshotFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/metrics" {
			http.NotFound(w, request)
			return
		}
		if request.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		current := HealthSnapshot{}
		if snapshot != nil {
			ctx, cancel := context.WithTimeout(request.Context(), 4*time.Second)
			current = snapshot(ctx)
			cancel()
		}
		body := r.render(current)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = fmt.Fprint(w, body)
	})
}

type registrySnapshot struct {
	startedAt       time.Time
	httpInFlight    int64
	http            map[httpKey]httpValue
	reconciliations map[string]uint64
	voice           map[voiceKey]uint64
}

func (r *Registry) snapshot() registrySnapshot {
	if r == nil {
		return registrySnapshot{startedAt: time.Now()}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := registrySnapshot{
		startedAt:       r.startedAt,
		httpInFlight:    r.httpInFlight,
		http:            make(map[httpKey]httpValue, len(r.http)),
		reconciliations: make(map[string]uint64, len(r.reconciliations)),
		voice:           make(map[voiceKey]uint64, len(r.voice)),
	}
	for key, value := range r.http {
		copy.http[key] = value
	}
	for key, value := range r.reconciliations {
		copy.reconciliations[key] = value
	}
	for key, value := range r.voice {
		copy.voice[key] = value
	}
	return copy
}

func (r *Registry) render(health HealthSnapshot) string {
	snapshot := r.snapshot()
	var output strings.Builder
	writeMetricHelp(&output, "ringring_process_start_time_seconds", "Unix time when this RingRing process started.", "gauge")
	fmt.Fprintf(&output, "ringring_process_start_time_seconds %d\n", snapshot.startedAt.Unix())
	writeMetricHelp(&output, "ringring_database_up", "Whether the private SQLite readiness query succeeds.", "gauge")
	fmt.Fprintf(&output, "ringring_database_up %d\n", boolNumber(health.DatabaseUp))
	writeMetricHelp(&output, "ringring_asterisk_ami_up", "Whether the private aggregate Asterisk AMI query succeeds.", "gauge")
	fmt.Fprintf(&output, "ringring_asterisk_ami_up %d\n", boolNumber(health.AsteriskAMIUp))
	writeMetricHelp(&output, "ringring_sip_contacts", "Current aggregate SIP contacts by normalized reachability state.", "gauge")
	fmt.Fprintf(&output, "ringring_sip_contacts{state=\"reachable\"} %d\n", nonnegative(health.ReachableContacts))
	fmt.Fprintf(&output, "ringring_sip_contacts{state=\"unreachable\"} %d\n", nonnegative(health.UnreachableContacts))
	fmt.Fprintf(&output, "ringring_sip_contacts{state=\"nonqualified\"} %d\n", nonnegative(health.NonQualifiedContacts))
	fmt.Fprintf(&output, "ringring_sip_contacts{state=\"unknown\"} %d\n", nonnegative(health.UnknownContacts))
	writeMetricHelp(&output, "ringring_http_requests_in_flight", "Current requests on the public web listener.", "gauge")
	fmt.Fprintf(&output, "ringring_http_requests_in_flight %d\n", snapshot.httpInFlight)

	httpKeys := make([]httpKey, 0, len(snapshot.http))
	for key := range snapshot.http {
		httpKeys = append(httpKeys, key)
	}
	sort.Slice(httpKeys, func(i, j int) bool {
		if httpKeys[i].surface != httpKeys[j].surface {
			return httpKeys[i].surface < httpKeys[j].surface
		}
		if httpKeys[i].method != httpKeys[j].method {
			return httpKeys[i].method < httpKeys[j].method
		}
		return httpKeys[i].statusClass < httpKeys[j].statusClass
	})
	writeMetricHelp(&output, "ringring_http_requests_total", "Completed web requests grouped only by coarse surface, method, and status class.", "counter")
	writeMetricHelp(&output, "ringring_http_request_duration_seconds", "Web request duration grouped only by coarse surface, method, and status class.", "histogram")
	for _, key := range httpKeys {
		value := snapshot.http[key]
		labels := httpLabels(key)
		fmt.Fprintf(&output, "ringring_http_requests_total%s %d\n", labels, value.count)
		for index, upperBound := range requestDurationBuckets {
			fmt.Fprintf(&output, "ringring_http_request_duration_seconds_bucket%s %d\n", withLabel(labels, "le", strconv.FormatFloat(upperBound, 'g', -1, 64)), value.buckets[index])
		}
		fmt.Fprintf(&output, "ringring_http_request_duration_seconds_bucket%s %d\n", withLabel(labels, "le", "+Inf"), value.count)
		fmt.Fprintf(&output, "ringring_http_request_duration_seconds_sum%s %s\n", labels, strconv.FormatFloat(value.sum, 'g', -1, 64))
		fmt.Fprintf(&output, "ringring_http_request_duration_seconds_count%s %d\n", labels, value.count)
	}

	writeMetricHelp(&output, "ringring_telephony_reconciliations_total", "Generated-configuration reconciliation attempts by aggregate result.", "counter")
	for _, result := range []string{"success", "error"} {
		fmt.Fprintf(&output, "ringring_telephony_reconciliations_total{result=\"%s\"} %d\n", result, snapshot.reconciliations[result])
	}
	writeMetricHelp(&output, "ringring_voice_service_requests_total", "Private voice-service operations by bounded service and result labels.", "counter")
	voiceKeys := make([]voiceKey, 0, len(snapshot.voice))
	for key := range snapshot.voice {
		voiceKeys = append(voiceKeys, key)
	}
	sort.Slice(voiceKeys, func(i, j int) bool {
		if voiceKeys[i].service != voiceKeys[j].service {
			return voiceKeys[i].service < voiceKeys[j].service
		}
		return voiceKeys[i].result < voiceKeys[j].result
	})
	for _, key := range voiceKeys {
		fmt.Fprintf(&output, "ringring_voice_service_requests_total{service=\"%s\",result=\"%s\"} %d\n", key.service, key.result, snapshot.voice[key])
	}
	return output.String()
}

func writeMetricHelp(output *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(output, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func httpLabels(key httpKey) string {
	return fmt.Sprintf("{surface=\"%s\",method=\"%s\",status_class=\"%s\"}", key.surface, key.method, key.statusClass)
}

func withLabel(labels, name, value string) string {
	return strings.TrimSuffix(labels, "}") + fmt.Sprintf(",%s=\"%s\"}", name, value)
}

func normalizeSurface(value string) string {
	switch value {
	case "health", "static", "authentication", "host", "invitation", "provisioning", "public":
		return value
	default:
		return "other"
	}
}

func normalizeMethod(value string) string {
	switch value {
	case http.MethodGet, http.MethodPost, http.MethodHead:
		return value
	default:
		return "OTHER"
	}
}

func normalizeStatus(status int) string {
	if status < 100 || status > 599 {
		return "other"
	}
	return strconv.Itoa(status/100) + "xx"
}

func normalizeVoiceService(value string) string {
	switch value {
	case "weather", "operator", "extension", "conference_join":
		return value
	default:
		return "other"
	}
}

func normalizeVoiceResult(value string) string {
	switch value {
	case "ready", "changed", "completed", "error", "abandoned":
		return value
	default:
		return "error"
	}
}

func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nonnegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

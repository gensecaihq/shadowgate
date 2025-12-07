package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks gateway metrics
type Metrics struct {
	startTime time.Time

	// Request counters
	totalRequests   int64
	allowedRequests int64
	deniedRequests  int64
	droppedRequests int64

	// Per-profile counters
	profileRequests map[string]*int64
	profileMu       sync.RWMutex

	// Decision counters
	decisions  map[string]*int64
	decisionMu sync.RWMutex

	// Rule hit counters
	ruleHits   map[string]*int64
	ruleHitsMu sync.RWMutex

	// Unique IPs seen
	uniqueIPs   map[string]struct{}
	uniqueIPsMu sync.RWMutex

	// Response time tracking
	totalResponseTime int64
	responseCount     int64

	// Per-backend metrics
	backendStats   map[string]*BackendStats
	backendStatsMu sync.RWMutex
}

// BackendStats tracks per-backend statistics
type BackendStats struct {
	Requests      int64
	Errors        int64
	TotalLatency  int64 // microseconds
	MinLatency    int64 // microseconds
	MaxLatency    int64 // microseconds
}

// New creates a new metrics instance
func New() *Metrics {
	return &Metrics{
		startTime:       time.Now(),
		profileRequests: make(map[string]*int64),
		decisions:       make(map[string]*int64),
		ruleHits:        make(map[string]*int64),
		uniqueIPs:       make(map[string]struct{}),
		backendStats:    make(map[string]*BackendStats),
	}
}

// RecordRequest records a request
func (m *Metrics) RecordRequest(profileID, clientIP, action string, durationMs float64) {
	atomic.AddInt64(&m.totalRequests, 1)

	switch action {
	case "allow_forward":
		atomic.AddInt64(&m.allowedRequests, 1)
	case "deny_decoy":
		atomic.AddInt64(&m.deniedRequests, 1)
	case "drop":
		atomic.AddInt64(&m.droppedRequests, 1)
	}

	// Profile counter
	m.profileMu.Lock()
	if m.profileRequests[profileID] == nil {
		var zero int64
		m.profileRequests[profileID] = &zero
	}
	atomic.AddInt64(m.profileRequests[profileID], 1)
	m.profileMu.Unlock()

	// Decision counter
	m.decisionMu.Lock()
	if m.decisions[action] == nil {
		var zero int64
		m.decisions[action] = &zero
	}
	atomic.AddInt64(m.decisions[action], 1)
	m.decisionMu.Unlock()

	// Unique IPs (cap at 100k to prevent unbounded growth)
	m.uniqueIPsMu.Lock()
	if len(m.uniqueIPs) >= 100000 {
		// Reset to prevent memory leak
		m.uniqueIPs = make(map[string]struct{})
	}
	m.uniqueIPs[clientIP] = struct{}{}
	m.uniqueIPsMu.Unlock()

	// Response time
	atomic.AddInt64(&m.totalResponseTime, int64(durationMs*1000))
	atomic.AddInt64(&m.responseCount, 1)
}

// RecordRuleHit records a rule hit
func (m *Metrics) RecordRuleHit(ruleType string) {
	m.ruleHitsMu.Lock()
	if m.ruleHits[ruleType] == nil {
		var zero int64
		m.ruleHits[ruleType] = &zero
	}
	atomic.AddInt64(m.ruleHits[ruleType], 1)
	m.ruleHitsMu.Unlock()
}

// RecordBackendRequest records a backend request with latency
func (m *Metrics) RecordBackendRequest(backendName string, latencyUs int64, isError bool) {
	m.backendStatsMu.Lock()
	stats := m.backendStats[backendName]
	if stats == nil {
		stats = &BackendStats{
			MinLatency: latencyUs,
			MaxLatency: latencyUs,
		}
		m.backendStats[backendName] = stats
	}
	m.backendStatsMu.Unlock()

	atomic.AddInt64(&stats.Requests, 1)
	atomic.AddInt64(&stats.TotalLatency, latencyUs)

	if isError {
		atomic.AddInt64(&stats.Errors, 1)
	}

	// Update min/max latency (these need locking for correctness)
	m.backendStatsMu.Lock()
	if latencyUs < stats.MinLatency || stats.MinLatency == 0 {
		stats.MinLatency = latencyUs
	}
	if latencyUs > stats.MaxLatency {
		stats.MaxLatency = latencyUs
	}
	m.backendStatsMu.Unlock()
}

// BackendStatsSnapshot represents per-backend statistics snapshot
type BackendStatsSnapshot struct {
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	ErrorRate    float64 `json:"error_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
}

// Snapshot represents a point-in-time metrics snapshot
type Snapshot struct {
	Uptime           string                          `json:"uptime"`
	TotalRequests    int64                           `json:"total_requests"`
	AllowedRequests  int64                           `json:"allowed_requests"`
	DeniedRequests   int64                           `json:"denied_requests"`
	DroppedRequests  int64                           `json:"dropped_requests"`
	UniqueIPs        int                             `json:"unique_ips"`
	AvgResponseMs    float64                         `json:"avg_response_ms"`
	RequestsPerSec   float64                         `json:"requests_per_sec"`
	ProfileRequests  map[string]int64                `json:"profile_requests"`
	Decisions        map[string]int64                `json:"decisions"`
	RuleHits         map[string]int64                `json:"rule_hits"`
	BackendStats     map[string]BackendStatsSnapshot `json:"backend_stats"`
}

// GetSnapshot returns a snapshot of current metrics
func (m *Metrics) GetSnapshot() *Snapshot {
	uptime := time.Since(m.startTime)
	total := atomic.LoadInt64(&m.totalRequests)
	respCount := atomic.LoadInt64(&m.responseCount)
	respTime := atomic.LoadInt64(&m.totalResponseTime)

	var avgResp float64
	if respCount > 0 {
		avgResp = float64(respTime) / float64(respCount) / 1000.0
	}

	var rps float64
	if uptime.Seconds() > 0 {
		rps = float64(total) / uptime.Seconds()
	}

	// Copy profile requests
	m.profileMu.RLock()
	profileReqs := make(map[string]int64)
	for k, v := range m.profileRequests {
		profileReqs[k] = atomic.LoadInt64(v)
	}
	m.profileMu.RUnlock()

	// Copy decisions
	m.decisionMu.RLock()
	decisions := make(map[string]int64)
	for k, v := range m.decisions {
		decisions[k] = atomic.LoadInt64(v)
	}
	m.decisionMu.RUnlock()

	// Copy rule hits
	m.ruleHitsMu.RLock()
	ruleHits := make(map[string]int64)
	for k, v := range m.ruleHits {
		ruleHits[k] = atomic.LoadInt64(v)
	}
	m.ruleHitsMu.RUnlock()

	// Count unique IPs
	m.uniqueIPsMu.RLock()
	uniqueCount := len(m.uniqueIPs)
	m.uniqueIPsMu.RUnlock()

	// Copy backend stats
	m.backendStatsMu.RLock()
	backendStats := make(map[string]BackendStatsSnapshot)
	for name, stats := range m.backendStats {
		requests := atomic.LoadInt64(&stats.Requests)
		errors := atomic.LoadInt64(&stats.Errors)
		totalLatency := atomic.LoadInt64(&stats.TotalLatency)

		var errorRate float64
		if requests > 0 {
			errorRate = float64(errors) / float64(requests) * 100
		}

		var avgLatency float64
		if requests > 0 {
			avgLatency = float64(totalLatency) / float64(requests) / 1000.0 // us to ms
		}

		backendStats[name] = BackendStatsSnapshot{
			Requests:     requests,
			Errors:       errors,
			ErrorRate:    errorRate,
			AvgLatencyMs: avgLatency,
			MinLatencyMs: float64(stats.MinLatency) / 1000.0,
			MaxLatencyMs: float64(stats.MaxLatency) / 1000.0,
		}
	}
	m.backendStatsMu.RUnlock()

	return &Snapshot{
		Uptime:          uptime.Round(time.Second).String(),
		TotalRequests:   total,
		AllowedRequests: atomic.LoadInt64(&m.allowedRequests),
		DeniedRequests:  atomic.LoadInt64(&m.deniedRequests),
		DroppedRequests: atomic.LoadInt64(&m.droppedRequests),
		UniqueIPs:       uniqueCount,
		AvgResponseMs:   avgResp,
		RequestsPerSec:  rps,
		ProfileRequests: profileReqs,
		Decisions:       decisions,
		RuleHits:        ruleHits,
		BackendStats:    backendStats,
	}
}

// Handler returns an HTTP handler for the metrics endpoint
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := m.GetSnapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshot)
	}
}

// PrometheusHandler returns an HTTP handler for Prometheus-format metrics
func (m *Metrics) PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := m.GetSnapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// Total requests
		fmt.Fprintf(w, "# HELP shadowgate_requests_total Total number of requests processed\n")
		fmt.Fprintf(w, "# TYPE shadowgate_requests_total counter\n")
		fmt.Fprintf(w, "shadowgate_requests_total %d\n\n", snapshot.TotalRequests)

		// Allowed/Denied/Dropped requests
		fmt.Fprintf(w, "# HELP shadowgate_requests_allowed_total Total number of allowed requests\n")
		fmt.Fprintf(w, "# TYPE shadowgate_requests_allowed_total counter\n")
		fmt.Fprintf(w, "shadowgate_requests_allowed_total %d\n\n", snapshot.AllowedRequests)

		fmt.Fprintf(w, "# HELP shadowgate_requests_denied_total Total number of denied requests\n")
		fmt.Fprintf(w, "# TYPE shadowgate_requests_denied_total counter\n")
		fmt.Fprintf(w, "shadowgate_requests_denied_total %d\n\n", snapshot.DeniedRequests)

		fmt.Fprintf(w, "# HELP shadowgate_requests_dropped_total Total number of dropped requests\n")
		fmt.Fprintf(w, "# TYPE shadowgate_requests_dropped_total counter\n")
		fmt.Fprintf(w, "shadowgate_requests_dropped_total %d\n\n", snapshot.DroppedRequests)

		// Unique IPs
		fmt.Fprintf(w, "# HELP shadowgate_unique_ips Number of unique client IPs seen\n")
		fmt.Fprintf(w, "# TYPE shadowgate_unique_ips gauge\n")
		fmt.Fprintf(w, "shadowgate_unique_ips %d\n\n", snapshot.UniqueIPs)

		// Average response time
		fmt.Fprintf(w, "# HELP shadowgate_response_time_ms_avg Average response time in milliseconds\n")
		fmt.Fprintf(w, "# TYPE shadowgate_response_time_ms_avg gauge\n")
		fmt.Fprintf(w, "shadowgate_response_time_ms_avg %.3f\n\n", snapshot.AvgResponseMs)

		// Requests per second
		fmt.Fprintf(w, "# HELP shadowgate_requests_per_second Current request rate\n")
		fmt.Fprintf(w, "# TYPE shadowgate_requests_per_second gauge\n")
		fmt.Fprintf(w, "shadowgate_requests_per_second %.3f\n\n", snapshot.RequestsPerSec)

		// Per-profile requests
		fmt.Fprintf(w, "# HELP shadowgate_profile_requests_total Requests per profile\n")
		fmt.Fprintf(w, "# TYPE shadowgate_profile_requests_total counter\n")
		for profile, count := range snapshot.ProfileRequests {
			fmt.Fprintf(w, "shadowgate_profile_requests_total{profile=%q} %d\n", profile, count)
		}
		fmt.Fprintf(w, "\n")

		// Per-decision counts
		fmt.Fprintf(w, "# HELP shadowgate_decisions_total Counts by decision type\n")
		fmt.Fprintf(w, "# TYPE shadowgate_decisions_total counter\n")
		for decision, count := range snapshot.Decisions {
			fmt.Fprintf(w, "shadowgate_decisions_total{decision=%q} %d\n", decision, count)
		}
		fmt.Fprintf(w, "\n")

		// Per-rule hits
		fmt.Fprintf(w, "# HELP shadowgate_rule_hits_total Counts by rule type\n")
		fmt.Fprintf(w, "# TYPE shadowgate_rule_hits_total counter\n")
		for rule, count := range snapshot.RuleHits {
			fmt.Fprintf(w, "shadowgate_rule_hits_total{rule=%q} %d\n", rule, count)
		}
		fmt.Fprintf(w, "\n")

		// Backend metrics
		fmt.Fprintf(w, "# HELP shadowgate_backend_requests_total Total requests per backend\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_requests_total counter\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_requests_total{backend=%q} %d\n", backend, stats.Requests)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP shadowgate_backend_errors_total Total errors per backend\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_errors_total counter\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_errors_total{backend=%q} %d\n", backend, stats.Errors)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP shadowgate_backend_latency_ms_avg Average latency per backend in milliseconds\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_latency_ms_avg gauge\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_latency_ms_avg{backend=%q} %.3f\n", backend, stats.AvgLatencyMs)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP shadowgate_backend_latency_ms_min Minimum latency per backend in milliseconds\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_latency_ms_min gauge\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_latency_ms_min{backend=%q} %.3f\n", backend, stats.MinLatencyMs)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP shadowgate_backend_latency_ms_max Maximum latency per backend in milliseconds\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_latency_ms_max gauge\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_latency_ms_max{backend=%q} %.3f\n", backend, stats.MaxLatencyMs)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP shadowgate_backend_error_rate Error rate per backend (percentage)\n")
		fmt.Fprintf(w, "# TYPE shadowgate_backend_error_rate gauge\n")
		for backend, stats := range snapshot.BackendStats {
			fmt.Fprintf(w, "shadowgate_backend_error_rate{backend=%q} %.2f\n", backend, stats.ErrorRate)
		}
	}
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	atomic.StoreInt64(&m.totalRequests, 0)
	atomic.StoreInt64(&m.allowedRequests, 0)
	atomic.StoreInt64(&m.deniedRequests, 0)
	atomic.StoreInt64(&m.droppedRequests, 0)
	atomic.StoreInt64(&m.totalResponseTime, 0)
	atomic.StoreInt64(&m.responseCount, 0)

	m.profileMu.Lock()
	m.profileRequests = make(map[string]*int64)
	m.profileMu.Unlock()

	m.decisionMu.Lock()
	m.decisions = make(map[string]*int64)
	m.decisionMu.Unlock()

	m.ruleHitsMu.Lock()
	m.ruleHits = make(map[string]*int64)
	m.ruleHitsMu.Unlock()

	m.uniqueIPsMu.Lock()
	m.uniqueIPs = make(map[string]struct{})
	m.uniqueIPsMu.Unlock()

	m.backendStatsMu.Lock()
	m.backendStats = make(map[string]*BackendStats)
	m.backendStatsMu.Unlock()

	m.startTime = time.Now()
}

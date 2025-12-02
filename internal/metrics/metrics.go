package metrics

import (
	"encoding/json"
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
	decisions map[string]*int64
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
}

// New creates a new metrics instance
func New() *Metrics {
	return &Metrics{
		startTime:       time.Now(),
		profileRequests: make(map[string]*int64),
		decisions:       make(map[string]*int64),
		ruleHits:        make(map[string]*int64),
		uniqueIPs:       make(map[string]struct{}),
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

// Snapshot represents a point-in-time metrics snapshot
type Snapshot struct {
	Uptime           string             `json:"uptime"`
	TotalRequests    int64              `json:"total_requests"`
	AllowedRequests  int64              `json:"allowed_requests"`
	DeniedRequests   int64              `json:"denied_requests"`
	DroppedRequests  int64              `json:"dropped_requests"`
	UniqueIPs        int                `json:"unique_ips"`
	AvgResponseMs    float64            `json:"avg_response_ms"`
	RequestsPerSec   float64            `json:"requests_per_sec"`
	ProfileRequests  map[string]int64   `json:"profile_requests"`
	Decisions        map[string]int64   `json:"decisions"`
	RuleHits         map[string]int64   `json:"rule_hits"`
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

	m.startTime = time.Now()
}

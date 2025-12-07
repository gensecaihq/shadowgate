package metrics

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsRecordRequest(t *testing.T) {
	m := New()

	m.RecordRequest("profile1", "10.0.0.1", "allow_forward", 15.5)
	m.RecordRequest("profile1", "10.0.0.2", "deny_decoy", 10.0)
	m.RecordRequest("profile2", "10.0.0.1", "allow_forward", 20.0)

	snapshot := m.GetSnapshot()

	if snapshot.TotalRequests != 3 {
		t.Errorf("expected 3 total requests, got %d", snapshot.TotalRequests)
	}

	if snapshot.AllowedRequests != 2 {
		t.Errorf("expected 2 allowed requests, got %d", snapshot.AllowedRequests)
	}

	if snapshot.DeniedRequests != 1 {
		t.Errorf("expected 1 denied request, got %d", snapshot.DeniedRequests)
	}

	if snapshot.UniqueIPs != 2 {
		t.Errorf("expected 2 unique IPs, got %d", snapshot.UniqueIPs)
	}

	if snapshot.ProfileRequests["profile1"] != 2 {
		t.Errorf("expected 2 requests for profile1, got %d", snapshot.ProfileRequests["profile1"])
	}

	if snapshot.ProfileRequests["profile2"] != 1 {
		t.Errorf("expected 1 request for profile2, got %d", snapshot.ProfileRequests["profile2"])
	}
}

func TestMetricsRuleHits(t *testing.T) {
	m := New()

	m.RecordRuleHit("ip_allow")
	m.RecordRuleHit("ip_allow")
	m.RecordRuleHit("ua_whitelist")

	snapshot := m.GetSnapshot()

	if snapshot.RuleHits["ip_allow"] != 2 {
		t.Errorf("expected 2 ip_allow hits, got %d", snapshot.RuleHits["ip_allow"])
	}

	if snapshot.RuleHits["ua_whitelist"] != 1 {
		t.Errorf("expected 1 ua_whitelist hit, got %d", snapshot.RuleHits["ua_whitelist"])
	}
}

func TestMetricsHandler(t *testing.T) {
	m := New()
	m.RecordRequest("test", "10.0.0.1", "allow_forward", 10.0)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	m.Handler()(rr, req)

	if rr.Code != 200 {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var snapshot Snapshot
	if err := json.NewDecoder(rr.Body).Decode(&snapshot); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if snapshot.TotalRequests != 1 {
		t.Errorf("expected 1 total request in response, got %d", snapshot.TotalRequests)
	}
}

func TestMetricsReset(t *testing.T) {
	m := New()

	m.RecordRequest("test", "10.0.0.1", "allow_forward", 10.0)
	m.Reset()

	snapshot := m.GetSnapshot()

	if snapshot.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", snapshot.TotalRequests)
	}

	if snapshot.UniqueIPs != 0 {
		t.Errorf("expected 0 unique IPs after reset, got %d", snapshot.UniqueIPs)
	}
}

func TestBackendMetrics(t *testing.T) {
	m := New()

	// Record some backend requests
	m.RecordBackendRequest("backend1", 5000, false)  // 5ms success
	m.RecordBackendRequest("backend1", 10000, false) // 10ms success
	m.RecordBackendRequest("backend1", 15000, true)  // 15ms error
	m.RecordBackendRequest("backend2", 3000, false)  // 3ms success

	snapshot := m.GetSnapshot()

	// Check backend1 stats
	b1Stats, ok := snapshot.BackendStats["backend1"]
	if !ok {
		t.Fatal("expected backend1 stats")
	}

	if b1Stats.Requests != 3 {
		t.Errorf("expected 3 requests for backend1, got %d", b1Stats.Requests)
	}

	if b1Stats.Errors != 1 {
		t.Errorf("expected 1 error for backend1, got %d", b1Stats.Errors)
	}

	// Error rate should be ~33.33%
	if b1Stats.ErrorRate < 33 || b1Stats.ErrorRate > 34 {
		t.Errorf("expected ~33%% error rate, got %.2f%%", b1Stats.ErrorRate)
	}

	// Average latency should be (5+10+15)/3 = 10ms
	if b1Stats.AvgLatencyMs < 9.9 || b1Stats.AvgLatencyMs > 10.1 {
		t.Errorf("expected ~10ms avg latency, got %.2fms", b1Stats.AvgLatencyMs)
	}

	// Min latency should be 5ms
	if b1Stats.MinLatencyMs < 4.9 || b1Stats.MinLatencyMs > 5.1 {
		t.Errorf("expected 5ms min latency, got %.2fms", b1Stats.MinLatencyMs)
	}

	// Max latency should be 15ms
	if b1Stats.MaxLatencyMs < 14.9 || b1Stats.MaxLatencyMs > 15.1 {
		t.Errorf("expected 15ms max latency, got %.2fms", b1Stats.MaxLatencyMs)
	}

	// Check backend2 stats
	b2Stats, ok := snapshot.BackendStats["backend2"]
	if !ok {
		t.Fatal("expected backend2 stats")
	}

	if b2Stats.Requests != 1 {
		t.Errorf("expected 1 request for backend2, got %d", b2Stats.Requests)
	}

	if b2Stats.Errors != 0 {
		t.Errorf("expected 0 errors for backend2, got %d", b2Stats.Errors)
	}
}

func TestBackendMetricsReset(t *testing.T) {
	m := New()

	m.RecordBackendRequest("backend1", 5000, false)
	m.Reset()

	snapshot := m.GetSnapshot()

	if len(snapshot.BackendStats) != 0 {
		t.Errorf("expected 0 backend stats after reset, got %d", len(snapshot.BackendStats))
	}
}

func TestPrometheusBackendMetrics(t *testing.T) {
	m := New()
	m.RecordBackendRequest("test-backend", 5000, false)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	m.PrometheusHandler()(rr, req)

	body := rr.Body.String()

	// Check that backend metrics are present
	if !strings.Contains(body, "shadowgate_backend_requests_total{backend=\"test-backend\"}") {
		t.Error("expected shadowgate_backend_requests_total metric")
	}

	if !strings.Contains(body, "shadowgate_backend_errors_total{backend=\"test-backend\"}") {
		t.Error("expected shadowgate_backend_errors_total metric")
	}

	if !strings.Contains(body, "shadowgate_backend_latency_ms_avg{backend=\"test-backend\"}") {
		t.Error("expected shadowgate_backend_latency_ms_avg metric")
	}
}

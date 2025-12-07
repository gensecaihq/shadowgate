package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shadowgate/internal/metrics"
	"shadowgate/internal/proxy"
)

func TestHealthEndpoint(t *testing.T) {
	api := New(Config{
		Addr:    ":0",
		Version: "test",
	})

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	api.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	api := New(Config{
		Addr:    ":0",
		Version: "1.0.0",
	})

	req := httptest.NewRequest("GET", "/status", nil)
	rr := httptest.NewRecorder()

	api.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp StatusResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Status != "running" {
		t.Errorf("expected status 'running', got %q", resp.Status)
	}

	if resp.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", resp.Version)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	m := metrics.New()
	m.RecordRequest("test", "10.0.0.1", "allow_forward", 10.0)

	api := New(Config{
		Addr:    ":0",
		Metrics: m,
	})

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	api.handleMetrics(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestBackendsEndpoint(t *testing.T) {
	api := New(Config{
		Addr: ":0",
	})

	pool := proxy.NewPool()
	b1, _ := proxy.NewBackend("backend1", "http://127.0.0.1:8001", 10)
	b2, _ := proxy.NewBackend("backend2", "http://127.0.0.1:8002", 5)
	pool.Add(b1)
	pool.Add(b2)

	b1.SetHealthy(false)

	api.RegisterPool("test-profile", pool)

	req := httptest.NewRequest("GET", "/backends", nil)
	rr := httptest.NewRecorder()

	api.handleBackends(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp BackendsResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	profile, ok := resp.Profiles["test-profile"]
	if !ok {
		t.Fatal("expected test-profile in response")
	}

	if profile.Total != 2 {
		t.Errorf("expected 2 total backends, got %d", profile.Total)
	}

	if profile.Healthy != 1 {
		t.Errorf("expected 1 healthy backend, got %d", profile.Healthy)
	}
}

func TestReloadEndpoint(t *testing.T) {
	reloadCalled := false
	api := New(Config{
		Addr: ":0",
		ReloadFunc: func() error {
			reloadCalled = true
			return nil
		},
	})

	req := httptest.NewRequest("POST", "/reload", nil)
	rr := httptest.NewRecorder()

	api.handleReload(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if !reloadCalled {
		t.Error("expected reload function to be called")
	}

	var resp ReloadResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestReloadEndpointWrongMethod(t *testing.T) {
	api := New(Config{
		Addr: ":0",
	})

	req := httptest.NewRequest("GET", "/reload", nil)
	rr := httptest.NewRecorder()

	api.handleReload(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestAuthTokenRequired(t *testing.T) {
	api := New(Config{
		Addr:      ":0",
		AuthToken: "secret-token",
		Version:   "test",
	})

	tests := []struct {
		name       string
		path       string
		auth       string
		wantStatus int
	}{
		{"health no auth", "/health", "", http.StatusOK},
		{"status no auth", "/status", "", http.StatusUnauthorized},
		{"status wrong token", "/status", "Bearer wrong-token", http.StatusUnauthorized},
		{"status valid token", "/status", "Bearer secret-token", http.StatusOK},
		{"status basic auth", "/status", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rr := httptest.NewRecorder()

			api.server.Handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestIPAllowlist(t *testing.T) {
	api := New(Config{
		Addr:       ":0",
		AllowedIPs: []string{"10.0.0.0/8", "192.168.1.100"},
		Version:    "test",
	})

	tests := []struct {
		name       string
		remoteAddr string
		wantStatus int
	}{
		{"allowed subnet", "10.1.2.3:12345", http.StatusOK},
		{"allowed single IP", "192.168.1.100:12345", http.StatusOK},
		{"denied IP", "172.16.0.1:12345", http.StatusForbidden},
		{"denied public IP", "8.8.8.8:12345", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/status", nil)
			req.RemoteAddr = tt.remoteAddr
			rr := httptest.NewRecorder()

			api.server.Handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestCombinedAuth(t *testing.T) {
	api := New(Config{
		Addr:       ":0",
		AuthToken:  "secret-token",
		AllowedIPs: []string{"10.0.0.0/8"},
		Version:    "test",
	})

	tests := []struct {
		name       string
		remoteAddr string
		auth       string
		wantStatus int
	}{
		{"allowed IP, valid token", "10.1.2.3:12345", "Bearer secret-token", http.StatusOK},
		{"allowed IP, no token", "10.1.2.3:12345", "", http.StatusUnauthorized},
		{"denied IP, valid token", "172.16.0.1:12345", "Bearer secret-token", http.StatusForbidden},
		{"denied IP, no token", "172.16.0.1:12345", "", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/status", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rr := httptest.NewRecorder()

			api.server.Handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestNoAuthConfigured(t *testing.T) {
	// When no auth is configured, all endpoints should be accessible
	api := New(Config{
		Addr:    ":0",
		Version: "test",
	})

	req := httptest.NewRequest("GET", "/status", nil)
	req.RemoteAddr = "8.8.8.8:12345" // Any IP
	rr := httptest.NewRecorder()

	api.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 when no auth configured, got %d", rr.Code)
	}
}

func TestPrometheusMetricsWithCircuitBreaker(t *testing.T) {
	m := metrics.New()
	m.RecordRequest("test", "10.0.0.1", "allow_forward", 10.0)

	api := New(Config{
		Addr:    ":0",
		Metrics: m,
	})

	// Register a pool with backends
	pool := proxy.NewPool()
	b1, _ := proxy.NewBackend("backend1", "http://127.0.0.1:8001", 10)
	b2, _ := proxy.NewBackend("backend2", "http://127.0.0.1:8002", 5)
	pool.Add(b1)
	pool.Add(b2)

	// Mark one backend as unhealthy
	b1.SetHealthy(false)

	api.RegisterPool("test-profile", pool)

	req := httptest.NewRequest("GET", "/metrics/prometheus", nil)
	rr := httptest.NewRecorder()

	api.handlePrometheusMetrics(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Check that circuit breaker metrics are present
	if !strings.Contains(body, "shadowgate_circuit_breaker_state") {
		t.Error("expected shadowgate_circuit_breaker_state metric")
	}

	if !strings.Contains(body, "shadowgate_circuit_breaker_failures") {
		t.Error("expected shadowgate_circuit_breaker_failures metric")
	}

	if !strings.Contains(body, "shadowgate_circuit_breaker_successes") {
		t.Error("expected shadowgate_circuit_breaker_successes metric")
	}

	if !strings.Contains(body, "shadowgate_backend_healthy") {
		t.Error("expected shadowgate_backend_healthy metric")
	}

	// Check that profile and backend labels are present
	if !strings.Contains(body, "profile=\"test-profile\"") {
		t.Error("expected profile label in metrics")
	}

	if !strings.Contains(body, "backend=\"backend1\"") {
		t.Error("expected backend1 label in metrics")
	}

	if !strings.Contains(body, "backend=\"backend2\"") {
		t.Error("expected backend2 label in metrics")
	}
}

func TestCircuitBreakerMetricsWithOpenCircuit(t *testing.T) {
	m := metrics.New()
	api := New(Config{
		Addr:    ":0",
		Metrics: m,
	})

	pool := proxy.NewPool()
	b1, _ := proxy.NewBackend("failing-backend", "http://127.0.0.1:8001", 10)
	pool.Add(b1)

	// Simulate failures to open the circuit breaker
	for i := 0; i < 5; i++ {
		b1.CircuitBreakerStats() // Access to trigger
	}

	api.RegisterPool("prod", pool)

	req := httptest.NewRequest("GET", "/metrics/prometheus", nil)
	rr := httptest.NewRecorder()

	api.handlePrometheusMetrics(rr, req)

	body := rr.Body.String()

	// Should have the failing backend in metrics
	if !strings.Contains(body, "failing-backend") {
		t.Error("expected failing-backend in metrics")
	}

	// Should have prod profile
	if !strings.Contains(body, "prod") {
		t.Error("expected prod profile in metrics")
	}
}

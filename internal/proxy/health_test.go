package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBackendHealth(t *testing.T) {
	b, err := NewBackend("test", "http://127.0.0.1:8080", 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Should be healthy by default
	if !b.IsHealthy() {
		t.Error("expected backend to be healthy by default")
	}

	// Set unhealthy
	b.SetHealthy(false)
	if b.IsHealthy() {
		t.Error("expected backend to be unhealthy")
	}

	// Set healthy again
	b.SetHealthy(true)
	if !b.IsHealthy() {
		t.Error("expected backend to be healthy")
	}

	// Check status
	status := b.GetHealthStatus()
	if status.CheckCount != 2 {
		t.Errorf("expected 2 checks, got %d", status.CheckCount)
	}
	if status.FailCount != 1 {
		t.Errorf("expected 1 fail, got %d", status.FailCount)
	}
}

func TestPoolNextHealthy(t *testing.T) {
	pool := NewPool()

	b1, _ := NewBackend("b1", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("b2", "http://127.0.0.1:8002", 10)
	b3, _ := NewBackend("b3", "http://127.0.0.1:8003", 10)

	pool.Add(b1)
	pool.Add(b2)
	pool.Add(b3)

	// All healthy - should round robin
	first := pool.NextHealthy()
	if first == nil {
		t.Fatal("expected a backend")
	}

	// Mark b1 unhealthy
	b1.SetHealthy(false)

	// Should skip b1
	for i := 0; i < 10; i++ {
		b := pool.NextHealthy()
		if b.Name == "b1" {
			t.Error("should not return unhealthy b1")
		}
	}

	// Mark all unhealthy - should still return something (fallback)
	b2.SetHealthy(false)
	b3.SetHealthy(false)

	b := pool.NextHealthy()
	if b == nil {
		t.Error("should return fallback backend when all unhealthy")
	}
}

func TestPoolHealthyCount(t *testing.T) {
	pool := NewPool()

	b1, _ := NewBackend("b1", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("b2", "http://127.0.0.1:8002", 10)

	pool.Add(b1)
	pool.Add(b2)

	if pool.HealthyCount() != 2 {
		t.Errorf("expected 2 healthy, got %d", pool.HealthyCount())
	}

	b1.SetHealthy(false)

	if pool.HealthyCount() != 1 {
		t.Errorf("expected 1 healthy, got %d", pool.HealthyCount())
	}
}

func TestHealthChecker(t *testing.T) {
	// Create a test server
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	pool := NewPool()
	b, _ := NewBackend("test", server.URL, 10)
	pool.Add(b)

	config := HealthConfig{
		Enabled:  true,
		Interval: 50 * time.Millisecond,
		Timeout:  1 * time.Second,
		Path:     "/",
	}

	hc := NewHealthChecker(pool, config)
	hc.Start()
	defer hc.Stop()

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	if !b.IsHealthy() {
		t.Error("expected backend to be healthy")
	}

	// Make server unhealthy
	healthy = false

	// Wait for check
	time.Sleep(100 * time.Millisecond)

	if b.IsHealthy() {
		t.Error("expected backend to be unhealthy")
	}
}

func TestGetHealthStatuses(t *testing.T) {
	pool := NewPool()

	b1, _ := NewBackend("b1", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("b2", "http://127.0.0.1:8002", 10)

	pool.Add(b1)
	pool.Add(b2)

	b1.SetHealthy(false)

	statuses := pool.GetHealthStatuses()

	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}

	if statuses["b1"].Healthy {
		t.Error("expected b1 to be unhealthy")
	}

	if !statuses["b2"].Healthy {
		t.Error("expected b2 to be healthy")
	}
}

func TestPoolNextWeighted(t *testing.T) {
	pool := NewPool()

	// b1 has weight 10, b2 has weight 1
	b1, _ := NewBackend("b1", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("b2", "http://127.0.0.1:8002", 1)

	pool.Add(b1)
	pool.Add(b2)

	// Count selections over many iterations
	counts := map[string]int{"b1": 0, "b2": 0}
	for i := 0; i < 110; i++ {
		b := pool.NextWeighted()
		counts[b.Name]++
	}

	// b1 should be selected roughly 10x more than b2
	// With 110 iterations and weights 10:1, expect ~100:10
	if counts["b1"] < 80 {
		t.Errorf("expected b1 to be selected more often, got %d", counts["b1"])
	}

	// Mark b1 unhealthy - should only return b2
	b1.SetHealthy(false)

	for i := 0; i < 10; i++ {
		b := pool.NextWeighted()
		if b.Name != "b2" {
			t.Errorf("expected only b2 when b1 unhealthy, got %s", b.Name)
		}
	}
}

func TestServeHTTPWithRetry(t *testing.T) {
	// Create backend servers - first one fails, second succeeds
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success from backend2"))
	}))
	defer backend2.Close()

	pool := NewPool()
	b1, _ := NewBackend("failing", backend1.URL, 10)
	b2, _ := NewBackend("working", backend2.URL, 10)
	pool.Add(b1)
	pool.Add(b2)

	// Mark first backend as unhealthy so it's skipped
	b1.SetHealthy(false)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// With retry, should skip failing backend and succeed with second
	result := pool.ServeHTTPWithRetry(rr, req, 2)

	if result == nil {
		t.Error("expected a backend to handle request")
	}

	// The working backend should have been used
	if result.Name != "working" {
		t.Errorf("expected 'working' backend, got %q", result.Name)
	}

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestServeHTTPWithRetryEmptyPool(t *testing.T) {
	pool := NewPool()

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	result := pool.ServeHTTPWithRetry(rr, req, 2)

	if result != nil {
		t.Error("expected nil from empty pool")
	}
}

func TestServeHTTPWithRetrySingleBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	pool := NewPool()
	b, _ := NewBackend("single", backend.URL, 10)
	pool.Add(b)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	result := pool.ServeHTTPWithRetry(rr, req, 3)

	if result == nil {
		t.Error("expected a backend to handle request")
	}

	if result.Name != "single" {
		t.Errorf("expected 'single' backend, got %q", result.Name)
	}
}

func TestBackendHealthCheckPath(t *testing.T) {
	// Server that only responds healthy on /custom/health
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/custom/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Backend with custom health path
	b, err := NewBackendWithHealthPath("custom", server.URL, 10, "/custom/health")
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if b.HealthCheckPath != "/custom/health" {
		t.Errorf("expected health path '/custom/health', got %q", b.HealthCheckPath)
	}

	pool := NewPool()
	pool.Add(b)

	config := HealthConfig{
		Enabled:  true,
		Interval: 50 * time.Millisecond,
		Timeout:  1 * time.Second,
		Path:     "/", // Default path, but backend should use its own
	}

	hc := NewHealthChecker(pool, config)
	hc.Start()
	defer hc.Stop()

	// Wait for health check
	time.Sleep(100 * time.Millisecond)

	// Backend should be healthy because it uses its custom health path
	if !b.IsHealthy() {
		t.Error("expected backend to be healthy using custom health path")
	}
}

func TestBackendDefaultHealthPath(t *testing.T) {
	// Test that default health path is set
	b, err := NewBackend("default", "http://127.0.0.1:8080", 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if b.HealthCheckPath != "/" {
		t.Errorf("expected default health path '/', got %q", b.HealthCheckPath)
	}
}

package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewBackend(t *testing.T) {
	b, err := NewBackend("test", "http://127.0.0.1:8080", 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if b.Name != "test" {
		t.Errorf("expected name 'test', got %q", b.Name)
	}

	if b.Weight != 10 {
		t.Errorf("expected weight 10, got %d", b.Weight)
	}
}

func TestNewBackendInvalidURL(t *testing.T) {
	_, err := NewBackend("test", "://invalid", 10)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestBackendServeHTTP(t *testing.T) {
	// Create a test backend server
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer backendServer.Close()

	b, err := NewBackend("test", backendServer.URL, 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Create a test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	b.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "backend response" {
		t.Errorf("expected 'backend response', got %q", string(body))
	}
}

func TestPoolRoundRobin(t *testing.T) {
	pool := NewPool()

	b1, _ := NewBackend("b1", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("b2", "http://127.0.0.1:8002", 10)
	b3, _ := NewBackend("b3", "http://127.0.0.1:8003", 10)

	pool.Add(b1)
	pool.Add(b2)
	pool.Add(b3)

	if pool.Len() != 3 {
		t.Errorf("expected 3 backends, got %d", pool.Len())
	}

	// Test round-robin
	names := make([]string, 6)
	for i := 0; i < 6; i++ {
		names[i] = pool.Next().Name
	}

	expected := []string{"b1", "b2", "b3", "b1", "b2", "b3"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("position %d: expected %s, got %s", i, expected[i], name)
		}
	}
}

func TestPoolGet(t *testing.T) {
	pool := NewPool()

	b1, _ := NewBackend("primary", "http://127.0.0.1:8001", 10)
	b2, _ := NewBackend("secondary", "http://127.0.0.1:8002", 5)

	pool.Add(b1)
	pool.Add(b2)

	found := pool.Get("primary")
	if found == nil || found.Name != "primary" {
		t.Error("expected to find 'primary' backend")
	}

	notFound := pool.Get("nonexistent")
	if notFound != nil {
		t.Error("expected nil for nonexistent backend")
	}
}

func TestPoolEmpty(t *testing.T) {
	pool := NewPool()

	if pool.Next() != nil {
		t.Error("expected nil from empty pool")
	}
}

func TestBackendStripsServerHeaders(t *testing.T) {
	// Create a test backend server that returns sensitive headers
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "Apache/2.4.41")
		w.Header().Set("X-Powered-By", "PHP/7.4.3")
		w.Header().Set("X-AspNet-Version", "4.0.30319")
		w.Header().Set("X-Custom-Header", "should-remain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backendServer.Close()

	b, err := NewBackend("test", backendServer.URL, 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	b.ServeHTTP(rr, req)

	// These headers should be stripped
	if rr.Header().Get("Server") != "" {
		t.Error("Server header should be stripped")
	}
	if rr.Header().Get("X-Powered-By") != "" {
		t.Error("X-Powered-By header should be stripped")
	}
	if rr.Header().Get("X-AspNet-Version") != "" {
		t.Error("X-AspNet-Version header should be stripped")
	}

	// Custom headers should remain
	if rr.Header().Get("X-Custom-Header") != "should-remain" {
		t.Error("X-Custom-Header should remain")
	}
}

func TestBackendCircuitBreaker(t *testing.T) {
	failCount := 0
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount++
		if failCount <= 5 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backendServer.Close()

	b, err := NewBackend("test", backendServer.URL, 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Initial state should be closed
	if b.CircuitBreakerState() != CircuitClosed {
		t.Errorf("expected closed state, got %v", b.CircuitBreakerState())
	}

	// Make requests that will fail and eventually open the circuit
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		b.ServeHTTP(rr, req)
	}

	// Circuit should be open now
	if b.CircuitBreakerState() != CircuitOpen {
		t.Errorf("expected open state after failures, got %v", b.CircuitBreakerState())
	}

	// Requests should be blocked with 503
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	b.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when circuit is open, got %d", rr.Code)
	}
}

func TestBackendCircuitBreakerReset(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backendServer.Close()

	b, err := NewBackend("test", backendServer.URL, 10)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Open the circuit
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		b.ServeHTTP(rr, req)
	}

	if b.CircuitBreakerState() != CircuitOpen {
		t.Fatalf("expected open state, got %v", b.CircuitBreakerState())
	}

	// Reset the circuit breaker
	b.ResetCircuitBreaker()

	if b.CircuitBreakerState() != CircuitClosed {
		t.Errorf("expected closed state after reset, got %v", b.CircuitBreakerState())
	}
}

func TestBackendWithOptions(t *testing.T) {
	opts := BackendOptions{
		HealthCheckPath: "/custom/health",
		Timeout:         60 * time.Second,
	}

	b, err := NewBackendWithOptions("test", "http://127.0.0.1:8080", 5, opts)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if b.Name != "test" {
		t.Errorf("expected name 'test', got %q", b.Name)
	}

	if b.Weight != 5 {
		t.Errorf("expected weight 5, got %d", b.Weight)
	}

	if b.HealthCheckPath != "/custom/health" {
		t.Errorf("expected health path '/custom/health', got %q", b.HealthCheckPath)
	}
}

func TestBackendOptionsDefaults(t *testing.T) {
	opts := DefaultBackendOptions()

	if opts.HealthCheckPath != "/" {
		t.Errorf("expected default health path '/', got %q", opts.HealthCheckPath)
	}

	if opts.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", opts.Timeout)
	}
}

func TestBackendWithOptionsEmptyValues(t *testing.T) {
	// Empty options should use defaults
	opts := BackendOptions{}

	b, err := NewBackendWithOptions("test", "http://127.0.0.1:8080", 10, opts)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if b.HealthCheckPath != "/" {
		t.Errorf("expected default health path '/', got %q", b.HealthCheckPath)
	}
}

func TestBackendWithOptionsZeroTimeout(t *testing.T) {
	// Zero timeout should use default
	opts := BackendOptions{
		Timeout: 0,
	}

	b, err := NewBackendWithOptions("test", "http://127.0.0.1:8080", 10, opts)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Backend should be created successfully with default timeout
	if b == nil {
		t.Error("expected backend to be created")
	}
}

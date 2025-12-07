package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Backend represents an upstream backend server
type Backend struct {
	Name            string
	URL             *url.URL
	Weight          int
	HealthCheckPath string
	proxy           *httputil.ReverseProxy
	health          HealthStatus
	healthMu        sync.RWMutex
	circuitBreaker  *CircuitBreaker
}

// BackendOptions contains optional backend configuration
type BackendOptions struct {
	HealthCheckPath string
	Timeout         time.Duration
}

// DefaultBackendOptions returns default backend options
func DefaultBackendOptions() BackendOptions {
	return BackendOptions{
		HealthCheckPath: "/",
		Timeout:         30 * time.Second,
	}
}

// NewBackend creates a new backend with default options
func NewBackend(name, rawURL string, weight int) (*Backend, error) {
	return NewBackendWithOptions(name, rawURL, weight, DefaultBackendOptions())
}

// NewBackendWithHealthPath creates a new backend with a custom health check path
func NewBackendWithHealthPath(name, rawURL string, weight int, healthCheckPath string) (*Backend, error) {
	opts := DefaultBackendOptions()
	opts.HealthCheckPath = healthCheckPath
	return NewBackendWithOptions(name, rawURL, weight, opts)
}

// NewBackendWithOptions creates a new backend with custom options
func NewBackendWithOptions(name, rawURL string, weight int, opts BackendOptions) (*Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL: %w", err)
	}

	if opts.HealthCheckPath == "" {
		opts.HealthCheckPath = "/"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	b := &Backend{
		Name:            name,
		URL:             u,
		Weight:          weight,
		HealthCheckPath: opts.HealthCheckPath,
		health:          HealthStatus{Healthy: true}, // Assume healthy until checked
		circuitBreaker:  NewCircuitBreaker(DefaultCircuitBreakerConfig()),
	}

	// Create reverse proxy with connection pooling and timeouts
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: opts.Timeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true, // Preserve original encoding
	}

	b.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			req.Host = u.Host

			// Remove hop-by-hop headers
			req.Header.Del("Connection")
			req.Header.Del("Proxy-Connection")
			req.Header.Del("Keep-Alive")
			req.Header.Del("Proxy-Authenticate")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Te")
			req.Header.Del("Trailers")
			req.Header.Del("Transfer-Encoding")
			req.Header.Del("Upgrade")
		},
		ModifyResponse: func(resp *http.Response) error {
			// Strip sensitive backend headers that could leak information
			resp.Header.Del("Server")
			resp.Header.Del("X-Powered-By")
			resp.Header.Del("X-AspNet-Version")
			resp.Header.Del("X-AspNetMvc-Version")
			resp.Header.Del("X-Runtime")
			resp.Header.Del("X-Version")
			return nil
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Return 502 Bad Gateway on backend error
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	return b, nil
}

// ServeHTTP proxies the request to the backend
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check circuit breaker
	if !b.circuitBreaker.Allow() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Use a custom response writer to capture the status
	wrapper := &responseWrapper{ResponseWriter: w, statusCode: http.StatusOK}
	b.proxy.ServeHTTP(wrapper, r)

	// Record success/failure based on status code
	if wrapper.statusCode >= 500 || wrapper.statusCode == http.StatusBadGateway {
		b.circuitBreaker.RecordFailure()
	} else {
		b.circuitBreaker.RecordSuccess()
	}
}

// responseWrapper wraps ResponseWriter to capture status code
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWrapper) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// CircuitBreakerState returns the current circuit breaker state
func (b *Backend) CircuitBreakerState() CircuitState {
	return b.circuitBreaker.State()
}

// CircuitBreakerStats returns circuit breaker statistics
func (b *Backend) CircuitBreakerStats() CircuitBreakerStats {
	return b.circuitBreaker.Stats()
}

// ResetCircuitBreaker resets the circuit breaker
func (b *Backend) ResetCircuitBreaker() {
	b.circuitBreaker.Reset()
}

// Pool manages multiple backends with load balancing
type Pool struct {
	backends   []*Backend
	currentIdx uint64
	mu         sync.RWMutex
}

// NewPool creates a new backend pool
func NewPool() *Pool {
	return &Pool{
		backends: make([]*Backend, 0),
	}
}

// Add adds a backend to the pool
func (p *Pool) Add(b *Backend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = append(p.backends, b)
}

// Next returns the next backend using round-robin (ignores health)
func (p *Pool) Next() *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.backends) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&p.currentIdx, 1) - 1
	return p.backends[idx%uint64(len(p.backends))]
}

// Get returns a backend by name
func (p *Pool) Get(name string) *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, b := range p.backends {
		if b.Name == name {
			return b
		}
	}
	return nil
}

// Len returns the number of backends
func (p *Pool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.backends)
}

// GetCircuitBreakerStats returns circuit breaker statistics for all backends
func (p *Pool) GetCircuitBreakerStats() map[string]CircuitBreakerStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats)
	for _, b := range p.backends {
		stats[b.Name] = b.CircuitBreakerStats()
	}
	return stats
}

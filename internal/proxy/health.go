package proxy

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// HealthConfig configures health checking
type HealthConfig struct {
	Enabled  bool
	Interval time.Duration
	Timeout  time.Duration
	Path     string // Health check endpoint path (e.g., "/health")
}

// DefaultHealthConfig returns default health check settings
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		Enabled:  true,
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Path:     "/",
	}
}

// HealthChecker performs health checks on backends
type HealthChecker struct {
	pool     *Pool
	config   HealthConfig
	client   *http.Client
	stop     chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(pool *Pool, config HealthConfig) *HealthChecker {
	return &HealthChecker{
		pool:   pool,
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
		stop: make(chan struct{}),
	}
}

// Start begins periodic health checking
func (hc *HealthChecker) Start() {
	hc.mu.Lock()
	if hc.running {
		hc.mu.Unlock()
		return
	}
	hc.running = true
	hc.mu.Unlock()

	// Initial health check
	hc.checkAll()

	go func() {
		ticker := time.NewTicker(hc.config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				hc.checkAll()
			case <-hc.stop:
				return
			}
		}
	}()
}

// Stop stops health checking
func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return
	}
	hc.running = false
	close(hc.stop)
}

func (hc *HealthChecker) checkAll() {
	hc.pool.mu.RLock()
	backends := hc.pool.backends
	hc.pool.mu.RUnlock()

	for _, b := range backends {
		healthy := hc.check(b)
		b.SetHealthy(healthy)
	}
}

func (hc *HealthChecker) check(b *Backend) bool {
	// Use backend's health check path if set, otherwise fall back to global config
	path := b.HealthCheckPath
	if path == "" {
		path = hc.config.Path
	}
	url := b.URL.Scheme + "://" + b.URL.Host + path

	ctx, cancel := context.WithTimeout(context.Background(), hc.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx as healthy
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// HealthStatus represents backend health status
type HealthStatus struct {
	Healthy     bool
	LastCheck   time.Time
	LastHealthy time.Time
	CheckCount  int64
	FailCount   int64
}

// health-related methods for Backend

// SetHealthy updates the backend's health status
func (b *Backend) SetHealthy(healthy bool) {
	now := time.Now()
	b.healthMu.Lock()
	defer b.healthMu.Unlock()

	b.health.LastCheck = now
	b.health.CheckCount++

	if healthy {
		b.health.Healthy = true
		b.health.LastHealthy = now
	} else {
		b.health.FailCount++
		b.health.Healthy = false
	}
}

// IsHealthy returns whether the backend is healthy
func (b *Backend) IsHealthy() bool {
	b.healthMu.RLock()
	defer b.healthMu.RUnlock()
	return b.health.Healthy
}

// GetHealthStatus returns the full health status
func (b *Backend) GetHealthStatus() HealthStatus {
	b.healthMu.RLock()
	defer b.healthMu.RUnlock()
	return b.health
}

// Pool methods for health-aware selection

// NextHealthy returns the next healthy backend using round-robin
func (p *Pool) NextHealthy() *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.backends) == 0 {
		return nil
	}

	// Try to find a healthy backend
	start := int(atomic.AddUint64(&p.currentIdx, 1)) - 1
	for i := 0; i < len(p.backends); i++ {
		idx := (start + i) % len(p.backends)
		b := p.backends[idx]
		if b.IsHealthy() {
			return b
		}
	}

	// If no healthy backends, return any backend (fallback)
	return p.backends[start%len(p.backends)]
}

// HealthyCount returns the number of healthy backends
func (p *Pool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, b := range p.backends {
		if b.IsHealthy() {
			count++
		}
	}
	return count
}

// GetHealthStatuses returns health status for all backends
func (p *Pool) GetHealthStatuses() map[string]HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	statuses := make(map[string]HealthStatus)
	for _, b := range p.backends {
		statuses[b.Name] = b.GetHealthStatus()
	}
	return statuses
}

// NextWeighted returns a backend using weighted selection (healthy only)
func (p *Pool) NextWeighted() *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.backends) == 0 {
		return nil
	}

	// Calculate total weight of healthy backends
	totalWeight := 0
	for _, b := range p.backends {
		if b.IsHealthy() {
			totalWeight += b.Weight
		}
	}

	// If no healthy backends, fall back to round-robin
	if totalWeight == 0 {
		idx := int(atomic.AddUint64(&p.currentIdx, 1) - 1)
		return p.backends[idx%len(p.backends)]
	}

	// Weighted selection
	counter := atomic.AddUint64(&p.currentIdx, 1)
	target := int(counter % uint64(totalWeight))

	cumulative := 0
	for _, b := range p.backends {
		if !b.IsHealthy() {
			continue
		}
		cumulative += b.Weight
		if target < cumulative {
			return b
		}
	}

	// Fallback (shouldn't reach here)
	return p.backends[0]
}

// ServeHTTPWithRetry attempts to serve a request, retrying with different backends on failure
// Returns the backend that successfully handled the request, or nil if all attempts failed
func (p *Pool) ServeHTTPWithRetry(w http.ResponseWriter, r *http.Request, maxRetries int) *Backend {
	p.mu.RLock()
	backends := p.backends
	p.mu.RUnlock()

	if len(backends) == 0 {
		return nil
	}

	if maxRetries <= 0 {
		maxRetries = 1
	}
	if maxRetries > len(backends) {
		maxRetries = len(backends)
	}

	tried := make(map[string]bool)
	start := int(atomic.AddUint64(&p.currentIdx, 1)) - 1

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Find a healthy backend we haven't tried yet
		var backend *Backend
		for i := 0; i < len(backends); i++ {
			idx := (start + attempt + i) % len(backends)
			b := backends[idx]
			if !tried[b.Name] && b.IsHealthy() && b.circuitBreaker.Allow() {
				backend = b
				break
			}
		}

		// If no healthy untried backend, try any untried backend
		if backend == nil {
			for i := 0; i < len(backends); i++ {
				idx := (start + attempt + i) % len(backends)
				b := backends[idx]
				if !tried[b.Name] {
					backend = b
					break
				}
			}
		}

		if backend == nil {
			break // All backends tried
		}

		tried[backend.Name] = true

		// Create a response recorder to check if the backend succeeded
		recorder := &retryResponseRecorder{
			ResponseWriter: w,
			statusCode:     0,
			headerWritten:  false,
		}

		backend.ServeHTTP(recorder, r)

		// Check if the request succeeded
		if recorder.statusCode > 0 && recorder.statusCode < 500 && recorder.statusCode != http.StatusBadGateway && recorder.statusCode != http.StatusServiceUnavailable {
			return backend
		}

		// If this is the last attempt or headers were already written, return
		if attempt == maxRetries-1 || recorder.headerWritten {
			return backend
		}
	}

	return nil
}

// retryResponseRecorder wraps ResponseWriter to track if we can still retry
type retryResponseRecorder struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
}

func (r *retryResponseRecorder) WriteHeader(code int) {
	if !r.headerWritten {
		r.statusCode = code
		r.headerWritten = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *retryResponseRecorder) Write(b []byte) (int, error) {
	if !r.headerWritten {
		r.headerWritten = true
		r.statusCode = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

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
	Name     string
	URL      *url.URL
	Weight   int
	proxy    *httputil.ReverseProxy
	health   HealthStatus
	healthMu sync.RWMutex
}

// NewBackend creates a new backend
func NewBackend(name, rawURL string, weight int) (*Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL: %w", err)
	}

	b := &Backend{
		Name:   name,
		URL:    u,
		Weight: weight,
		health: HealthStatus{Healthy: true}, // Assume healthy until checked
	}

	// Create reverse proxy with connection pooling
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // Preserve original encoding
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
	b.proxy.ServeHTTP(w, r)
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

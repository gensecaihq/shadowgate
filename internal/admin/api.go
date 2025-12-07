package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"shadowgate/internal/metrics"
	"shadowgate/internal/proxy"
)

// API provides administrative endpoints
type API struct {
	addr        string
	server      *http.Server
	metrics     *metrics.Metrics
	pools       map[string]*proxy.Pool
	poolsMu     sync.RWMutex
	reloadFunc  func() error
	startTime   time.Time
	version     string
	authToken   string
	allowedNets []*net.IPNet
}

// Config configures the Admin API
type Config struct {
	Addr       string
	Metrics    *metrics.Metrics
	ReloadFunc func() error
	Version    string
	AuthToken  string   // Bearer token for authentication
	AllowedIPs []string // CIDRs allowed to access admin API
}

// New creates a new Admin API
func New(cfg Config) *API {
	api := &API{
		addr:       cfg.Addr,
		metrics:    cfg.Metrics,
		pools:      make(map[string]*proxy.Pool),
		reloadFunc: cfg.ReloadFunc,
		startTime:  time.Now(),
		version:    cfg.Version,
		authToken:  cfg.AuthToken,
	}

	// Parse allowed IP networks
	for _, cidr := range cfg.AllowedIPs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try as single IP
			ip := net.ParseIP(cidr)
			if ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				network = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
			}
		}
		if network != nil {
			api.allowedNets = append(api.allowedNets, network)
		}
	}

	mux := http.NewServeMux()
	// Health endpoint - no auth required (for load balancer checks)
	mux.HandleFunc("/health", api.handleHealth)
	// All other endpoints require authentication
	mux.HandleFunc("/status", api.requireAuth(api.handleStatus))
	mux.HandleFunc("/metrics", api.requireAuth(api.handleMetrics))
	mux.HandleFunc("/metrics/prometheus", api.requireAuth(api.handlePrometheusMetrics))
	mux.HandleFunc("/backends", api.requireAuth(api.handleBackends))
	mux.HandleFunc("/reload", api.requireAuth(api.handleReload))

	api.server = &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return api
}

// requireAuth wraps a handler with authentication and IP-based access control
func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check IP allowlist if configured
		if len(a.allowedNets) > 0 {
			clientIP := extractIP(r.RemoteAddr)
			allowed := false
			if clientIP != nil {
				for _, network := range a.allowedNets {
					if network.Contains(clientIP) {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		// Check bearer token if configured
		if a.authToken != "" {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			if token != a.authToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		next(w, r)
	}
}

// extractIP extracts the IP address from a remote address string
func extractIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(host)
}

// RegisterPool registers a backend pool for status reporting
func (a *API) RegisterPool(profileID string, pool *proxy.Pool) {
	a.poolsMu.Lock()
	defer a.poolsMu.Unlock()
	a.pools[profileID] = pool
}

// Start starts the Admin API server
func (a *API) Start() error {
	go func() {
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash
		}
	}()
	return nil
}

// Stop stops the Admin API server
func (a *API) Stop(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

// StatusResponse represents the status endpoint response
type StatusResponse struct {
	Status    string        `json:"status"`
	Version   string        `json:"version"`
	Uptime    string        `json:"uptime"`
	GoVersion string        `json:"go_version"`
	NumCPU    int           `json:"num_cpu"`
	Goroutines int          `json:"goroutines"`
	Memory    MemoryStats   `json:"memory"`
}

// MemoryStats contains memory statistics
type MemoryStats struct {
	Alloc      uint64 `json:"alloc_bytes"`
	TotalAlloc uint64 `json:"total_alloc_bytes"`
	Sys        uint64 `json:"sys_bytes"`
	NumGC      uint32 `json:"num_gc"`
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	resp := StatusResponse{
		Status:     "running",
		Version:    a.version,
		Uptime:     time.Since(a.startTime).Round(time.Second).String(),
		GoVersion:  runtime.Version(),
		NumCPU:     runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(),
		Memory: MemoryStats{
			Alloc:      mem.Alloc,
			TotalAlloc: mem.TotalAlloc,
			Sys:        mem.Sys,
			NumGC:      mem.NumGC,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.metrics == nil {
		http.Error(w, "Metrics not available", http.StatusServiceUnavailable)
		return
	}

	a.metrics.Handler()(w, r)
}

func (a *API) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.metrics == nil {
		http.Error(w, "Metrics not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Write the main metrics
	a.metrics.PrometheusHandler()(w, r)

	// Append circuit breaker and health metrics
	a.writeCircuitBreakerMetrics(w)
}

func (a *API) writeCircuitBreakerMetrics(w http.ResponseWriter) {
	a.poolsMu.RLock()
	defer a.poolsMu.RUnlock()

	if len(a.pools) == 0 {
		return
	}

	// Circuit breaker state (0=closed, 1=open, 2=half-open)
	w.Write([]byte("\n# HELP shadowgate_circuit_breaker_state Circuit breaker state (0=closed, 1=open, 2=half-open)\n"))
	w.Write([]byte("# TYPE shadowgate_circuit_breaker_state gauge\n"))
	for profileID, pool := range a.pools {
		stats := pool.GetCircuitBreakerStats()
		for backendName, cbStats := range stats {
			line := "shadowgate_circuit_breaker_state{profile=\"" + profileID + "\",backend=\"" + backendName + "\"} " + itoa(int(cbStats.State)) + "\n"
			w.Write([]byte(line))
		}
	}

	// Circuit breaker failures
	w.Write([]byte("\n# HELP shadowgate_circuit_breaker_failures Current consecutive failure count\n"))
	w.Write([]byte("# TYPE shadowgate_circuit_breaker_failures gauge\n"))
	for profileID, pool := range a.pools {
		stats := pool.GetCircuitBreakerStats()
		for backendName, cbStats := range stats {
			line := "shadowgate_circuit_breaker_failures{profile=\"" + profileID + "\",backend=\"" + backendName + "\"} " + itoa(cbStats.Failures) + "\n"
			w.Write([]byte(line))
		}
	}

	// Circuit breaker successes (in half-open state)
	w.Write([]byte("\n# HELP shadowgate_circuit_breaker_successes Current consecutive success count in half-open state\n"))
	w.Write([]byte("# TYPE shadowgate_circuit_breaker_successes gauge\n"))
	for profileID, pool := range a.pools {
		stats := pool.GetCircuitBreakerStats()
		for backendName, cbStats := range stats {
			line := "shadowgate_circuit_breaker_successes{profile=\"" + profileID + "\",backend=\"" + backendName + "\"} " + itoa(cbStats.Successes) + "\n"
			w.Write([]byte(line))
		}
	}

	// Backend health status
	w.Write([]byte("\n# HELP shadowgate_backend_healthy Backend health status (1=healthy, 0=unhealthy)\n"))
	w.Write([]byte("# TYPE shadowgate_backend_healthy gauge\n"))
	for profileID, pool := range a.pools {
		statuses := pool.GetHealthStatuses()
		for backendName, status := range statuses {
			healthy := 0
			if status.Healthy {
				healthy = 1
			}
			line := "shadowgate_backend_healthy{profile=\"" + profileID + "\",backend=\"" + backendName + "\"} " + itoa(healthy) + "\n"
			w.Write([]byte(line))
		}
	}
}

// itoa converts int to string without importing strconv
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	var b [20]byte
	n := len(b) - 1
	for i > 0 {
		b[n] = byte('0' + i%10)
		i /= 10
		n--
	}
	return string(b[n+1:])
}

// BackendsResponse represents the backends endpoint response
type BackendsResponse struct {
	Profiles map[string]ProfileBackends `json:"profiles"`
}

// ProfileBackends represents backends for a profile
type ProfileBackends struct {
	Total   int                          `json:"total"`
	Healthy int                          `json:"healthy"`
	Backends []BackendStatus             `json:"backends"`
}

// BackendStatus represents a backend's status
type BackendStatus struct {
	Name           string             `json:"name"`
	URL            string             `json:"url"`
	Weight         int                `json:"weight"`
	Healthy        bool               `json:"healthy"`
	LastCheck      time.Time          `json:"last_check,omitempty"`
	LastHealthy    time.Time          `json:"last_healthy,omitempty"`
	CheckCount     int64              `json:"check_count"`
	FailCount      int64              `json:"fail_count"`
	CircuitBreaker CircuitBreakerInfo `json:"circuit_breaker"`
}

// CircuitBreakerInfo represents circuit breaker status
type CircuitBreakerInfo struct {
	State           string    `json:"state"`
	Failures        int       `json:"failures"`
	Successes       int       `json:"successes"`
	LastStateChange time.Time `json:"last_state_change"`
}

func (a *API) handleBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	a.poolsMu.RLock()
	defer a.poolsMu.RUnlock()

	resp := BackendsResponse{
		Profiles: make(map[string]ProfileBackends),
	}

	for profileID, pool := range a.pools {
		statuses := pool.GetHealthStatuses()
		backends := make([]BackendStatus, 0, len(statuses))

		for name, status := range statuses {
			b := pool.Get(name)
			if b == nil {
				continue
			}
			cbStats := b.CircuitBreakerStats()
			backends = append(backends, BackendStatus{
				Name:        name,
				URL:         b.URL.String(),
				Weight:      b.Weight,
				Healthy:     status.Healthy,
				LastCheck:   status.LastCheck,
				LastHealthy: status.LastHealthy,
				CheckCount:  status.CheckCount,
				FailCount:   status.FailCount,
				CircuitBreaker: CircuitBreakerInfo{
					State:           cbStats.State.String(),
					Failures:        cbStats.Failures,
					Successes:       cbStats.Successes,
					LastStateChange: cbStats.LastStateChange,
				},
			})
		}

		resp.Profiles[profileID] = ProfileBackends{
			Total:    pool.Len(),
			Healthy:  pool.HealthyCount(),
			Backends: backends,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ReloadResponse represents the reload endpoint response
type ReloadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (a *API) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.reloadFunc == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ReloadResponse{
			Success: false,
			Message: "Reload not configured",
		})
		return
	}

	err := a.reloadFunc()
	resp := ReloadResponse{Success: err == nil}
	if err != nil {
		resp.Message = err.Error()
	} else {
		resp.Message = "Configuration reloaded successfully"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

package gateway

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"shadowgate/internal/config"
)

func TestHandlerAllowForward(t *testing.T) {
	// Create a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	cfg := Config{
		ProfileID: "test",
		Profile: config.ProfileConfig{
			Rules: config.RulesConfig{
				Allow: &config.RuleGroup{
					And: []config.Rule{
						{Type: "ip_allow", CIDRs: []string{"0.0.0.0/0"}},
					},
				},
			},
			Backends: []config.BackendConfig{
				{Name: "primary", URL: backend.URL, Weight: 10},
			},
			Decoy: config.DecoyConfig{
				Mode:       "static",
				StatusCode: 200,
				Body:       "decoy",
			},
		},
	}

	handler, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "backend response" {
		t.Errorf("expected 'backend response', got %q", string(body))
	}
}

func TestHandlerDenyDecoy(t *testing.T) {
	cfg := Config{
		ProfileID: "test",
		Profile: config.ProfileConfig{
			Rules: config.RulesConfig{
				Allow: &config.RuleGroup{
					And: []config.Rule{
						{Type: "ip_allow", CIDRs: []string{"192.168.0.0/16"}},
					},
				},
			},
			Backends: []config.BackendConfig{
				{Name: "primary", URL: "http://127.0.0.1:9999", Weight: 10},
			},
			Decoy: config.DecoyConfig{
				Mode:       "static",
				StatusCode: 200,
				Body:       "decoy response",
			},
		},
	}

	handler, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	// Request from IP not in allow list
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "decoy response" {
		t.Errorf("expected 'decoy response', got %q", string(body))
	}
}

func TestExtractClientIP(t *testing.T) {
	// Test without trusted proxies (legacy behavior - trust XFF)
	t.Run("without trusted proxies", func(t *testing.T) {
		h := &Handler{} // No trusted proxies

		tests := []struct {
			name       string
			remoteAddr string
			headers    map[string]string
			expected   string
		}{
			{
				name:       "from RemoteAddr",
				remoteAddr: "192.168.1.1:12345",
				expected:   "192.168.1.1",
			},
			{
				name:       "from X-Forwarded-For",
				remoteAddr: "127.0.0.1:12345",
				headers:    map[string]string{"X-Forwarded-For": "10.0.0.1, 192.168.1.1"},
				expected:   "10.0.0.1",
			},
			{
				name:       "from X-Real-IP",
				remoteAddr: "127.0.0.1:12345",
				headers:    map[string]string{"X-Real-IP": "10.0.0.2"},
				expected:   "10.0.0.2",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				req.RemoteAddr = tc.remoteAddr
				for k, v := range tc.headers {
					req.Header.Set(k, v)
				}

				result := h.extractClientIP(req)
				if result != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, result)
				}
			})
		}
	})

	// Test with trusted proxies
	t.Run("with trusted proxies", func(t *testing.T) {
		_, trustedNet, _ := net.ParseCIDR("127.0.0.0/8")
		h := &Handler{
			trustedProxies: []*net.IPNet{trustedNet},
		}

		tests := []struct {
			name       string
			remoteAddr string
			headers    map[string]string
			expected   string
		}{
			{
				name:       "trusted proxy with XFF",
				remoteAddr: "127.0.0.1:12345",
				headers:    map[string]string{"X-Forwarded-For": "10.0.0.1, 192.168.1.1"},
				expected:   "10.0.0.1",
			},
			{
				name:       "untrusted source ignores XFF",
				remoteAddr: "192.168.1.1:12345",
				headers:    map[string]string{"X-Forwarded-For": "10.0.0.1"},
				expected:   "192.168.1.1",
			},
			{
				name:       "trusted proxy with X-Real-IP",
				remoteAddr: "127.0.0.1:12345",
				headers:    map[string]string{"X-Real-IP": "10.0.0.2"},
				expected:   "10.0.0.2",
			},
			{
				name:       "untrusted source ignores X-Real-IP",
				remoteAddr: "192.168.1.1:12345",
				headers:    map[string]string{"X-Real-IP": "10.0.0.2"},
				expected:   "192.168.1.1",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				req.RemoteAddr = tc.remoteAddr
				for k, v := range tc.headers {
					req.Header.Set(k, v)
				}

				result := h.extractClientIP(req)
				if result != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, result)
				}
			})
		}
	})
}

func TestRequestIDGeneration(t *testing.T) {
	// Create a test backend that echoes back the request ID
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		w.Header().Set("X-Backend-Received-ID", reqID)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := Config{
		ProfileID: "test",
		Profile: config.ProfileConfig{
			Rules: config.RulesConfig{
				Allow: &config.RuleGroup{
					And: []config.Rule{
						{Type: "ip_allow", CIDRs: []string{"0.0.0.0/0"}},
					},
				},
			},
			Backends: []config.BackendConfig{
				{Name: "primary", URL: backend.URL, Weight: 10},
			},
			Decoy: config.DecoyConfig{
				Mode:       "static",
				StatusCode: 200,
				Body:       "decoy",
			},
		},
	}

	handler, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("failed to create handler: %v", err)
	}

	t.Run("generates request ID when not provided", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		// Should have X-Request-ID in response
		respID := rr.Header().Get("X-Request-ID")
		if respID == "" {
			t.Error("expected X-Request-ID in response")
		}
		if len(respID) != 32 { // 16 bytes = 32 hex chars
			t.Errorf("expected 32 char request ID, got %d chars", len(respID))
		}
	})

	t.Run("preserves existing request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Request-ID", "existing-request-id-12345")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		// Should preserve the provided request ID
		respID := rr.Header().Get("X-Request-ID")
		if respID != "existing-request-id-12345" {
			t.Errorf("expected preserved request ID, got %q", respID)
		}
	})

	t.Run("forwards request ID to backend", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Request-ID", "test-id-for-backend")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		// Backend should have received the request ID
		backendReceivedID := rr.Header().Get("X-Backend-Received-ID")
		if backendReceivedID != "test-id-for-backend" {
			t.Errorf("expected backend to receive request ID, got %q", backendReceivedID)
		}
	})
}

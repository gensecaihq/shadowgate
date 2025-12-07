package config

import (
	"testing"
)

func TestParseValidConfig(t *testing.T) {
	yaml := `
global:
  log:
    level: info
    format: json
    output: stdout

profiles:
  - id: test
    listeners:
      - addr: "0.0.0.0:8080"
        protocol: http
    backends:
      - name: primary
        url: http://127.0.0.1:9000
        weight: 10
    decoy:
      mode: static
      status_code: 200
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Global.Log.Level != "info" {
		t.Errorf("expected log level 'info', got %q", cfg.Global.Log.Level)
	}

	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(cfg.Profiles))
	}

	if cfg.Profiles[0].ID != "test" {
		t.Errorf("expected profile ID 'test', got %q", cfg.Profiles[0].ID)
	}
}

func TestParseInvalidLogLevel(t *testing.T) {
	yaml := `
global:
  log:
    level: invalid
profiles:
  - id: test
    listeners:
      - addr: "0.0.0.0:8080"
        protocol: http
    backends:
      - name: primary
        url: http://127.0.0.1:9000
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestParseNoProfiles(t *testing.T) {
	yaml := `
global:
  log:
    level: info
profiles: []
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for empty profiles")
	}
}

func TestParseDuplicateProfileID(t *testing.T) {
	yaml := `
global:
  log:
    level: info
profiles:
  - id: same
    listeners:
      - addr: "0.0.0.0:8080"
        protocol: http
    backends:
      - name: primary
        url: http://127.0.0.1:9000
  - id: same
    listeners:
      - addr: "0.0.0.0:8081"
        protocol: http
    backends:
      - name: secondary
        url: http://127.0.0.1:9001
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for duplicate profile ID")
	}
}

func TestParseInvalidListenerAddr(t *testing.T) {
	yaml := `
global:
  log:
    level: info
profiles:
  - id: test
    listeners:
      - addr: "invalid"
        protocol: http
    backends:
      - name: primary
        url: http://127.0.0.1:9000
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid listener address")
	}
}

func TestParseHTTPSWithoutTLS(t *testing.T) {
	yaml := `
global:
  log:
    level: info
profiles:
  - id: test
    listeners:
      - addr: "0.0.0.0:443"
        protocol: https
    backends:
      - name: primary
        url: http://127.0.0.1:9000
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for HTTPS without TLS config")
	}
}

func TestValidateRegexPatterns(t *testing.T) {
	valid := []string{".*Chrome.*", "^Mozilla"}
	if err := ValidateRegexPatterns(valid); err != nil {
		t.Errorf("unexpected error for valid patterns: %v", err)
	}

	invalid := []string{"[invalid"}
	if err := ValidateRegexPatterns(invalid); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestBackendURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://127.0.0.1:9000", false},
		{"valid https", "https://backend.example.com", false},
		{"valid with path", "http://127.0.0.1:9000/api", false},
		{"missing scheme", "127.0.0.1:9000", true},
		{"invalid scheme", "ftp://127.0.0.1:9000", true},
		{"missing host", "http://", true},
		{"empty url", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := BackendConfig{
				Name:   "test",
				URL:    tc.url,
				Weight: 1,
			}
			err := b.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for URL %q", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for URL %q: %v", tc.url, err)
			}
		})
	}
}

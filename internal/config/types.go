package config

import "time"

// Config is the root configuration structure
type Config struct {
	Global   GlobalConfig   `yaml:"global"`
	Profiles []ProfileConfig `yaml:"profiles"`
}

// GlobalConfig contains global settings
type GlobalConfig struct {
	Log              LogConfig   `yaml:"log"`
	GeoIPDBPath      string      `yaml:"geoip_db_path"`       // Path to MaxMind GeoIP database
	MetricsAddr      string      `yaml:"metrics_addr"`        // Address for metrics endpoint (e.g., ":9090")
	AdminAPI         AdminConfig `yaml:"admin_api"`           // Admin API configuration
	TrustedProxies   []string    `yaml:"trusted_proxies"`     // CIDRs of trusted proxies for X-Forwarded-For
	MaxRequestBody   int64       `yaml:"max_request_body"`    // Maximum request body size in bytes (default: 10MB)
	ShutdownTimeout  int         `yaml:"shutdown_timeout"`    // Graceful shutdown timeout in seconds (default: 30)
}

// AdminConfig configures the admin API security
type AdminConfig struct {
	Token       string   `yaml:"token"`         // Bearer token for authentication (required for non-health endpoints)
	AllowedIPs  []string `yaml:"allowed_ips"`   // CIDRs allowed to access admin API
}

// LogConfig configures logging behavior
type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
	Output string `yaml:"output"` // stdout, stderr, or file path
}

// ProfileConfig defines a traffic handling profile
type ProfileConfig struct {
	ID        string           `yaml:"id"`
	Listeners []ListenerConfig `yaml:"listeners"`
	Backends  []BackendConfig  `yaml:"backends"`
	Rules     RulesConfig      `yaml:"rules"`
	Decoy     DecoyConfig      `yaml:"decoy"`
	Shaping   ShapingConfig    `yaml:"shaping"`
}

// ListenerConfig defines a network listener
type ListenerConfig struct {
	Addr     string    `yaml:"addr"`     // e.g., "0.0.0.0:443"
	Protocol string    `yaml:"protocol"` // http, https, tcp
	TLS      TLSConfig `yaml:"tls"`
}

// TLSConfig configures TLS settings
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// BackendConfig defines an upstream backend
type BackendConfig struct {
	Name            string `yaml:"name"`
	URL             string `yaml:"url"`              // e.g., "https://127.0.0.1:8443"
	Weight          int    `yaml:"weight"`           // for load balancing
	Timeout         string `yaml:"timeout"`
	HealthCheckPath string `yaml:"health_check_path"` // Health check endpoint (default: "/")
}

// RulesConfig contains allow and deny rule groups
type RulesConfig struct {
	Allow *RuleGroup `yaml:"allow"`
	Deny  *RuleGroup `yaml:"deny"`
}

// RuleGroup represents a group of rules with boolean logic
type RuleGroup struct {
	And  []Rule `yaml:"and,omitempty"`
	Or   []Rule `yaml:"or,omitempty"`
	Not  *Rule  `yaml:"not,omitempty"`
	Rule *Rule  `yaml:"rule,omitempty"` // single rule without logic
}

// Rule represents a single filtering rule
type Rule struct {
	Type string `yaml:"type"` // ip_allow, ip_deny, ua_match, time_window, etc.

	// IP-based rules
	CIDRs []string `yaml:"cidrs,omitempty"`

	// User-Agent rules
	Patterns []string `yaml:"patterns,omitempty"` // regex patterns

	// Time-based rules
	TimeWindows []TimeWindow `yaml:"time_windows,omitempty"`

	// HTTP rules
	Methods []string `yaml:"methods,omitempty"` // GET, POST, etc.
	Paths   []string `yaml:"paths,omitempty"`   // path patterns (regex)
	Headers []Header `yaml:"headers,omitempty"` // header checks

	// GeoIP rules
	Countries []string `yaml:"countries,omitempty"` // ISO country codes

	// ASN rules
	ASNs []uint `yaml:"asns,omitempty"` // AS numbers

	// TLS rules
	TLSMinVersion string `yaml:"tls_min_version,omitempty"` // 1.2, 1.3
	TLSMaxVersion string `yaml:"tls_max_version,omitempty"`
	SNIPatterns   []string `yaml:"sni_patterns,omitempty"`
	RequireSNI    bool     `yaml:"require_sni,omitempty"`

	// Rate limiting
	MaxRequests int    `yaml:"max_requests,omitempty"`
	Window      string `yaml:"window,omitempty"` // e.g., "1m", "1h"

	// Header rule specifics
	HeaderName    string `yaml:"header_name,omitempty"`
	RequireHeader bool   `yaml:"require_header,omitempty"`
}

// TimeWindow defines an allowed time window
type TimeWindow struct {
	Days  []string `yaml:"days"`  // mon, tue, wed, thu, fri, sat, sun
	Start string   `yaml:"start"` // HH:MM format
	End   string   `yaml:"end"`   // HH:MM format
}

// Header defines a header matching rule
type Header struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"` // regex pattern for value
}

// DecoyConfig configures deception behavior
type DecoyConfig struct {
	Mode       string `yaml:"mode"`        // static, redirect, proxy
	StatusCode int    `yaml:"status_code"` // HTTP status code for static mode
	Body       string `yaml:"body"`        // inline body content
	BodyFile   string `yaml:"body_file"`   // path to body file
	RedirectTo string `yaml:"redirect_to"` // URL for redirect mode
}

// ShapingConfig configures traffic shaping
type ShapingConfig struct {
	DelayMin time.Duration `yaml:"delay_min"`
	DelayMax time.Duration `yaml:"delay_max"`
}

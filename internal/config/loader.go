package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	return Parse(data)
}

// Parse parses configuration from YAML bytes
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if err := c.Global.Validate(); err != nil {
		return fmt.Errorf("global config: %w", err)
	}

	if len(c.Profiles) == 0 {
		return fmt.Errorf("at least one profile is required")
	}

	profileIDs := make(map[string]bool)
	for i, p := range c.Profiles {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("profile[%d]: %w", i, err)
		}
		if profileIDs[p.ID] {
			return fmt.Errorf("duplicate profile ID: %s", p.ID)
		}
		profileIDs[p.ID] = true
	}

	return nil
}

// Validate checks global configuration
func (g *GlobalConfig) Validate() error {
	if err := g.Log.Validate(); err != nil {
		return err
	}

	// Validate trusted proxies CIDRs
	for _, cidr := range g.TrustedProxies {
		_, _, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return fmt.Errorf("invalid trusted proxy CIDR or IP: %s", cidr)
			}
		}
	}

	return nil
}

// Validate checks log configuration
func (l *LogConfig) Validate() error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if l.Level != "" && !validLevels[strings.ToLower(l.Level)] {
		return fmt.Errorf("invalid log level: %s", l.Level)
	}

	validFormats := map[string]bool{"json": true, "text": true, "": true}
	if !validFormats[strings.ToLower(l.Format)] {
		return fmt.Errorf("invalid log format: %s", l.Format)
	}

	return nil
}

// Validate checks profile configuration
func (p *ProfileConfig) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("profile ID is required")
	}

	if len(p.Listeners) == 0 {
		return fmt.Errorf("at least one listener is required")
	}

	for i, l := range p.Listeners {
		if err := l.Validate(); err != nil {
			return fmt.Errorf("listener[%d]: %w", i, err)
		}
	}

	if len(p.Backends) == 0 {
		return fmt.Errorf("at least one backend is required")
	}

	for i, b := range p.Backends {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("backend[%d]: %w", i, err)
		}
	}

	if err := p.Decoy.Validate(); err != nil {
		return fmt.Errorf("decoy: %w", err)
	}

	return nil
}

// Validate checks listener configuration
func (l *ListenerConfig) Validate() error {
	if l.Addr == "" {
		return fmt.Errorf("listener address is required")
	}

	_, _, err := net.SplitHostPort(l.Addr)
	if err != nil {
		return fmt.Errorf("invalid listener address %q: %w", l.Addr, err)
	}

	validProtocols := map[string]bool{"http": true, "https": true, "tcp": true}
	if !validProtocols[strings.ToLower(l.Protocol)] {
		return fmt.Errorf("invalid protocol: %s", l.Protocol)
	}

	if strings.ToLower(l.Protocol) == "https" {
		if l.TLS.CertFile == "" || l.TLS.KeyFile == "" {
			return fmt.Errorf("TLS cert_file and key_file required for HTTPS")
		}
	}

	return nil
}

// Validate checks backend configuration
func (b *BackendConfig) Validate() error {
	if b.Name == "" {
		return fmt.Errorf("backend name is required")
	}

	if b.URL == "" {
		return fmt.Errorf("backend URL is required")
	}

	// Validate URL format
	u, err := url.Parse(b.URL)
	if err != nil {
		return fmt.Errorf("invalid backend URL %q: %w", b.URL, err)
	}

	// Ensure scheme is valid
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("backend URL must use http or https scheme: %s", b.URL)
	}

	// Ensure host is present
	if u.Host == "" {
		return fmt.Errorf("backend URL must include host: %s", b.URL)
	}

	if b.Weight < 0 {
		return fmt.Errorf("backend weight cannot be negative")
	}

	return nil
}

// Validate checks decoy configuration
func (d *DecoyConfig) Validate() error {
	if d.Mode == "" {
		return nil // decoy is optional
	}

	validModes := map[string]bool{"static": true, "redirect": true, "proxy": true}
	if !validModes[strings.ToLower(d.Mode)] {
		return fmt.Errorf("invalid decoy mode: %s", d.Mode)
	}

	if d.Mode == "redirect" && d.RedirectTo == "" {
		return fmt.Errorf("redirect_to is required for redirect mode")
	}

	return nil
}

// ValidateRegexPatterns checks if patterns are valid regex
func ValidateRegexPatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("invalid regex pattern %q: %w", p, err)
		}
	}
	return nil
}

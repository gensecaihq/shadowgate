package gateway

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"shadowgate/internal/config"
	"shadowgate/internal/decision"
	"shadowgate/internal/decoy"
	"shadowgate/internal/logging"
	"shadowgate/internal/metrics"
	"shadowgate/internal/proxy"
	"shadowgate/internal/rules"
)

// Handler is the main HTTP handler for the gateway
type Handler struct {
	profileID      string
	decisionEngine *decision.Engine
	backendPool    *proxy.Pool
	decoyStrategy  decoy.Strategy
	logger         *logging.Logger
	metrics        *metrics.Metrics
}

// Config configures the gateway handler
type Config struct {
	ProfileID   string
	Profile     config.ProfileConfig
	Logger      *logging.Logger
	Metrics     *metrics.Metrics
	BackendPool *proxy.Pool // Optional: if nil, will be created from Profile.Backends
}

// NewHandler creates a new gateway handler
func NewHandler(cfg Config) (*Handler, error) {
	h := &Handler{
		profileID: cfg.ProfileID,
		logger:    cfg.Logger,
		metrics:   cfg.Metrics,
	}

	// Build rule groups from config
	var allowRules, denyRules *rules.Group
	if cfg.Profile.Rules.Allow != nil {
		allowRules = buildRuleGroup(cfg.Profile.Rules.Allow)
	}
	if cfg.Profile.Rules.Deny != nil {
		denyRules = buildRuleGroup(cfg.Profile.Rules.Deny)
	}

	h.decisionEngine = decision.NewEngine(allowRules, denyRules)

	// Use provided backend pool or create one
	if cfg.BackendPool != nil {
		h.backendPool = cfg.BackendPool
	} else {
		h.backendPool = proxy.NewPool()
		for _, bc := range cfg.Profile.Backends {
			weight := bc.Weight
			if weight == 0 {
				weight = 1
			}
			backend, err := proxy.NewBackend(bc.Name, bc.URL, weight)
			if err != nil {
				return nil, err
			}
			h.backendPool.Add(backend)
		}
	}

	// Build decoy strategy
	h.decoyStrategy = buildDecoyStrategy(cfg.Profile.Decoy)

	return h, nil
}

func buildRuleGroup(cfg *config.RuleGroup) *rules.Group {
	if cfg == nil {
		return nil
	}

	group := &rules.Group{}

	// Process AND rules
	for _, rc := range cfg.And {
		if r := buildRule(rc); r != nil {
			group.And = append(group.And, r)
		}
	}

	// Process OR rules
	for _, rc := range cfg.Or {
		if r := buildRule(rc); r != nil {
			group.Or = append(group.Or, r)
		}
	}

	// Process NOT rule
	if cfg.Not != nil {
		group.Not = buildRule(*cfg.Not)
	}

	// Process single rule
	if cfg.Rule != nil {
		group.Single = buildRule(*cfg.Rule)
	}

	return group
}

func buildRule(rc config.Rule) rules.Rule {
	var r rules.Rule
	var err error

	switch rc.Type {
	case "ip_allow":
		r, err = rules.NewIPRule(rc.CIDRs, "allow")
	case "ip_deny":
		r, err = rules.NewIPRule(rc.CIDRs, "deny")
	case "ua_whitelist", "ua_match":
		r, err = rules.NewUARule(rc.Patterns, "whitelist")
	case "ua_blacklist":
		r, err = rules.NewUARule(rc.Patterns, "blacklist")
	case "geo_allow":
		r, err = rules.NewGeoRule(rc.Countries, "allow")
	case "geo_deny":
		r, err = rules.NewGeoRule(rc.Countries, "deny")
	case "asn_allow":
		r, err = rules.NewASNRule(rc.ASNs, "allow")
	case "asn_deny":
		r, err = rules.NewASNRule(rc.ASNs, "deny")
	case "method_allow":
		r, err = rules.NewMethodRule(rc.Methods, "allow")
	case "method_deny":
		r, err = rules.NewMethodRule(rc.Methods, "deny")
	case "path_allow":
		r, err = rules.NewPathRule(rc.Paths, "allow")
	case "path_deny":
		r, err = rules.NewPathRule(rc.Paths, "deny")
	case "header_allow":
		r, err = rules.NewHeaderRule(rc.HeaderName, rc.Patterns, rc.RequireHeader, "allow")
	case "header_deny":
		r, err = rules.NewHeaderRule(rc.HeaderName, rc.Patterns, rc.RequireHeader, "deny")
	case "tls_version":
		r, err = rules.NewTLSVersionRule(rc.TLSMinVersion, rc.TLSMaxVersion)
	case "sni_allow":
		r, err = rules.NewSNIRule(rc.SNIPatterns, rc.RequireSNI, "allow")
	case "sni_deny":
		r, err = rules.NewSNIRule(rc.SNIPatterns, rc.RequireSNI, "deny")
	case "rate_limit":
		window, _ := time.ParseDuration(rc.Window)
		if window == 0 {
			window = time.Minute
		}
		maxReqs := rc.MaxRequests
		if maxReqs == 0 {
			maxReqs = 100
		}
		return rules.NewRateLimitRule(maxReqs, window)
	case "time_window":
		windows := make([]rules.TimeWindow, 0, len(rc.TimeWindows))
		for _, tw := range rc.TimeWindows {
			parsed, parseErr := rules.ParseTimeWindow(tw.Days, tw.Start, tw.End)
			if parseErr != nil {
				log.Printf("Warning: failed to parse time window: %v", parseErr)
				continue
			}
			windows = append(windows, parsed)
		}
		return rules.NewTimeRule(windows, nil)
	default:
		log.Printf("Warning: unknown rule type: %s", rc.Type)
		return nil
	}

	if err != nil {
		log.Printf("Warning: failed to build rule type %s: %v", rc.Type, err)
		return nil
	}
	return r
}

func buildDecoyStrategy(cfg config.DecoyConfig) decoy.Strategy {
	switch cfg.Mode {
	case "static":
		body := cfg.Body
		if cfg.BodyFile != "" {
			d, err := decoy.NewStaticDecoyFromFile(cfg.StatusCode, cfg.BodyFile, "")
			if err == nil {
				return d
			}
		}
		statusCode := cfg.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		return decoy.NewStaticDecoy(statusCode, body, "")

	case "redirect":
		return decoy.NewRedirectDecoy(http.StatusFound, cfg.RedirectTo)

	default:
		// Default: simple 200 OK
		return decoy.NewStaticDecoy(http.StatusOK, "", "")
	}
}

// ServeHTTP handles incoming HTTP requests
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract client IP
	clientIP := extractClientIP(r)

	// Evaluate rules
	d := h.decisionEngine.Evaluate(r, clientIP)

	// Execute action
	var statusCode int
	switch d.Action {
	case decision.AllowForward:
		backend := h.backendPool.NextHealthy()
		if backend != nil {
			backend.ServeHTTP(w, r)
			statusCode = http.StatusOK // approximate
		} else {
			w.WriteHeader(http.StatusBadGateway)
			statusCode = http.StatusBadGateway
		}

	case decision.DenyDecoy:
		h.decoyStrategy.Serve(w, r)
		statusCode = http.StatusOK // approximate

	case decision.Drop:
		drop := &decoy.DropDecoy{}
		drop.Serve(w, r)
		return // don't log for dropped connections

	case decision.Redirect:
		http.Redirect(w, r, d.RedirectURL, http.StatusFound)
		statusCode = http.StatusFound

	case decision.Tarpit:
		tarpit := decoy.NewTarpitDecoy(5*time.Second, 30*time.Second, h.decoyStrategy)
		tarpit.Serve(w, r)
		statusCode = http.StatusOK

	default:
		w.WriteHeader(http.StatusInternalServerError)
		statusCode = http.StatusInternalServerError
	}

	duration := float64(time.Since(start).Microseconds()) / 1000.0

	// Record metrics
	if h.metrics != nil {
		h.metrics.RecordRequest(h.profileID, clientIP, d.Action.String(), duration)
	}

	// Log the request
	if h.logger != nil {
		h.logger.LogRequest(logging.RequestLog{
			Timestamp:  start,
			ProfileID:  h.profileID,
			ClientIP:   clientIP,
			Method:     r.Method,
			Path:       r.URL.Path,
			UserAgent:  r.Header.Get("User-Agent"),
			Action:     d.Action.String(),
			Reason:     d.Reason,
			Labels:     d.Labels,
			StatusCode: statusCode,
			Duration:   duration,
		})
	}
}

func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

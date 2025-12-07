package rules

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRuleAllow(t *testing.T) {
	rule, err := NewIPRule([]string{"10.0.0.0/8", "192.168.1.0/24"}, "allow")
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	tests := []struct {
		ip      string
		matched bool
	}{
		{"10.1.2.3", true},
		{"192.168.1.100", true},
		{"8.8.8.8", false},
		{"192.168.2.1", false},
	}

	for _, tc := range tests {
		ctx := &Context{ClientIP: tc.ip}
		result := rule.Evaluate(ctx)
		if result.Matched != tc.matched {
			t.Errorf("IP %s: expected matched=%v, got %v", tc.ip, tc.matched, result.Matched)
		}
	}
}

func TestIPRuleSingleIP(t *testing.T) {
	rule, err := NewIPRule([]string{"192.168.1.1"}, "allow")
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	ctx := &Context{ClientIP: "192.168.1.1"}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected single IP to match")
	}

	ctx = &Context{ClientIP: "192.168.1.2"}
	result = rule.Evaluate(ctx)
	if result.Matched {
		t.Error("expected different IP not to match")
	}
}

func TestUARuleWhitelist(t *testing.T) {
	rule, err := NewUARule([]string{".*Chrome.*", ".*Firefox.*"}, "whitelist")
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	tests := []struct {
		ua      string
		matched bool
	}{
		{"Mozilla/5.0 Chrome/91.0", true},
		{"Mozilla/5.0 Firefox/89.0", true},
		{"curl/7.68.0", false},
		{"python-requests/2.25.1", false},
	}

	for _, tc := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("User-Agent", tc.ua)
		ctx := &Context{Request: req}
		result := rule.Evaluate(ctx)
		if result.Matched != tc.matched {
			t.Errorf("UA %q: expected matched=%v, got %v", tc.ua, tc.matched, result.Matched)
		}
	}
}

func TestUARuleBlacklist(t *testing.T) {
	rule, err := NewUARule([]string{".*curl.*", ".*python.*"}, "blacklist")
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	ctx := &Context{Request: req}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected curl to match blacklist")
	}
}

func TestEvaluatorAND(t *testing.T) {
	ipRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")
	uaRule, _ := NewUARule([]string{".*Chrome.*"}, "whitelist")

	group := &Group{
		And: []Rule{ipRule, uaRule},
	}

	eval := NewEvaluator()

	// Both match
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Chrome/91.0")
	ctx := &Context{ClientIP: "10.1.2.3", Request: req}
	result := eval.EvaluateGroup(group, ctx)
	if !result.Matched {
		t.Error("expected AND group to match when all rules match")
	}

	// Only IP matches
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	ctx = &Context{ClientIP: "10.1.2.3", Request: req}
	result = eval.EvaluateGroup(group, ctx)
	if result.Matched {
		t.Error("expected AND group not to match when one rule fails")
	}
}

func TestEvaluatorOR(t *testing.T) {
	ipRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")
	uaRule, _ := NewUARule([]string{".*Chrome.*"}, "whitelist")

	group := &Group{
		Or: []Rule{ipRule, uaRule},
	}

	eval := NewEvaluator()

	// Only IP matches
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	ctx := &Context{ClientIP: "10.1.2.3", Request: req}
	result := eval.EvaluateGroup(group, ctx)
	if !result.Matched {
		t.Error("expected OR group to match when one rule matches")
	}

	// Neither matches
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "curl/7.68.0")
	ctx = &Context{ClientIP: "8.8.8.8", Request: req}
	result = eval.EvaluateGroup(group, ctx)
	if result.Matched {
		t.Error("expected OR group not to match when no rules match")
	}
}

func TestParseTimeWindow(t *testing.T) {
	tw, err := ParseTimeWindow([]string{"mon", "tue", "wed"}, "09:00", "17:00")
	if err != nil {
		t.Fatalf("failed to parse time window: %v", err)
	}

	if len(tw.Days) != 3 {
		t.Errorf("expected 3 days, got %d", len(tw.Days))
	}
}

// TLS Version Rule Tests

func TestTLSVersionRule(t *testing.T) {
	rule, err := NewTLSVersionRule("1.2", "1.3")
	if err != nil {
		t.Fatalf("failed to create TLS rule: %v", err)
	}

	tests := []struct {
		name       string
		tlsVersion uint16
		matched    bool
	}{
		{"TLS 1.2 in range", 0x0303, true},  // TLS 1.2
		{"TLS 1.3 in range", 0x0304, true},  // TLS 1.3
		{"TLS 1.1 below range", 0x0302, false}, // TLS 1.1
		{"No TLS", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{TLSVersion: tc.tlsVersion}
			result := rule.Evaluate(ctx)
			if result.Matched != tc.matched {
				t.Errorf("TLS version %x: expected matched=%v, got %v", tc.tlsVersion, tc.matched, result.Matched)
			}
		})
	}
}

func TestTLSVersionRuleType(t *testing.T) {
	rule, _ := NewTLSVersionRule("1.2", "1.3")
	if rule.Type() != "tls_version" {
		t.Errorf("expected type 'tls_version', got %q", rule.Type())
	}
}

func TestTLSVersionRuleInvalidVersion(t *testing.T) {
	_, err := NewTLSVersionRule("invalid", "1.3")
	if err == nil {
		t.Error("expected error for invalid TLS version")
	}
}

func TestTLSVersionRuleEmptyVersion(t *testing.T) {
	rule, err := NewTLSVersionRule("", "")
	if err != nil {
		t.Fatalf("failed to create rule with empty versions: %v", err)
	}

	ctx := &Context{TLSVersion: 0x0303}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected match with no version constraints")
	}
}

// SNI Rule Tests

func TestSNIRuleAllow(t *testing.T) {
	rule, err := NewSNIRule([]string{`.*\.example\.com$`, `^test\..*`}, false, "allow")
	if err != nil {
		t.Fatalf("failed to create SNI rule: %v", err)
	}

	tests := []struct {
		sni     string
		matched bool
	}{
		{"www.example.com", true},
		{"test.domain.com", true},
		{"other.domain.com", false},
		{"", true}, // SNI not required
	}

	for _, tc := range tests {
		ctx := &Context{SNI: tc.sni}
		result := rule.Evaluate(ctx)
		if result.Matched != tc.matched {
			t.Errorf("SNI %q: expected matched=%v, got %v", tc.sni, tc.matched, result.Matched)
		}
	}
}

func TestSNIRuleRequireSNI(t *testing.T) {
	rule, err := NewSNIRule([]string{`.*\.example\.com$`}, true, "allow")
	if err != nil {
		t.Fatalf("failed to create SNI rule: %v", err)
	}

	// Empty SNI should not match when required
	ctx := &Context{SNI: ""}
	result := rule.Evaluate(ctx)
	if result.Matched {
		t.Error("expected no match when SNI is required but not present")
	}
}

func TestSNIRuleType(t *testing.T) {
	rule, _ := NewSNIRule([]string{`.*`}, false, "allow")
	if rule.Type() != "sni_allow" {
		t.Errorf("expected type 'sni_allow', got %q", rule.Type())
	}

	rule2, _ := NewSNIRule([]string{`.*`}, false, "deny")
	if rule2.Type() != "sni_deny" {
		t.Errorf("expected type 'sni_deny', got %q", rule2.Type())
	}
}

func TestSNIRuleInvalidMode(t *testing.T) {
	_, err := NewSNIRule([]string{`.*`}, false, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestSNIRuleInvalidPattern(t *testing.T) {
	_, err := NewSNIRule([]string{`[invalid`}, false, "allow")
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

// GeoIP Rule Tests (without actual database)

func TestGeoRuleCreation(t *testing.T) {
	rule, err := NewGeoRule([]string{"US", "CA", "gb"}, "allow")
	if err != nil {
		t.Fatalf("failed to create GeoIP rule: %v", err)
	}

	if rule.Type() != "geo_allow" {
		t.Errorf("expected type 'geo_allow', got %q", rule.Type())
	}
}

func TestGeoRuleInvalidMode(t *testing.T) {
	_, err := NewGeoRule([]string{"US"}, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestGeoRuleNoDatabase(t *testing.T) {
	rule, _ := NewGeoRule([]string{"US"}, "allow")
	ctx := &Context{ClientIP: "8.8.8.8"}
	result := rule.Evaluate(ctx)
	// Should return not matched when no database is loaded
	if result.Matched {
		t.Error("expected no match when GeoIP database is not loaded")
	}
}

func TestASNRuleCreation(t *testing.T) {
	rule, err := NewASNRule([]uint{15169, 32934}, "deny")
	if err != nil {
		t.Fatalf("failed to create ASN rule: %v", err)
	}

	if rule.Type() != "asn_deny" {
		t.Errorf("expected type 'asn_deny', got %q", rule.Type())
	}
}

func TestASNRuleInvalidMode(t *testing.T) {
	_, err := NewASNRule([]uint{15169}, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestASNRuleNoDatabase(t *testing.T) {
	rule, _ := NewASNRule([]uint{15169}, "allow")
	ctx := &Context{ClientIP: "8.8.8.8"}
	result := rule.Evaluate(ctx)
	// Should return not matched when no database is loaded
	if result.Matched {
		t.Error("expected no match when GeoIP database is not loaded")
	}
}

// Time Rule Tests

func TestTimeRuleEvaluate(t *testing.T) {
	// Create a window that includes all days and all hours to ensure it matches
	windows := []TimeWindow{
		{
			Days:  []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday},
			Start: 0,
			End:   24 * time.Hour,
		},
	}

	rule := NewTimeRule(windows, time.UTC)
	ctx := &Context{}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected match for all-day window")
	}

	if rule.Type() != "time_window" {
		t.Errorf("expected type 'time_window', got %q", rule.Type())
	}
}

func TestTimeRuleNoMatch(t *testing.T) {
	// Create a window with no days
	windows := []TimeWindow{
		{
			Days:  []time.Weekday{},
			Start: 0,
			End:   24 * time.Hour,
		},
	}

	rule := NewTimeRule(windows, nil) // nil location should default to UTC
	ctx := &Context{}
	result := rule.Evaluate(ctx)
	if result.Matched {
		t.Error("expected no match when no days are specified")
	}
}

func TestParseTimeWindowErrors(t *testing.T) {
	// Invalid day
	_, err := ParseTimeWindow([]string{"invalid"}, "09:00", "17:00")
	if err == nil {
		t.Error("expected error for invalid day")
	}

	// Invalid start time
	_, err = ParseTimeWindow([]string{"mon"}, "invalid", "17:00")
	if err == nil {
		t.Error("expected error for invalid start time")
	}

	// Invalid end time
	_, err = ParseTimeWindow([]string{"mon"}, "09:00", "invalid")
	if err == nil {
		t.Error("expected error for invalid end time")
	}
}

func TestParseTimeWindowAllDays(t *testing.T) {
	// Test all day name formats
	days := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	tw, err := ParseTimeWindow(days, "00:00", "23:59")
	if err != nil {
		t.Fatalf("failed to parse all days: %v", err)
	}
	if len(tw.Days) != 7 {
		t.Errorf("expected 7 days, got %d", len(tw.Days))
	}
}

// Rate Limit Tests

func TestRateLimitRuleType(t *testing.T) {
	rule := NewRateLimitRule(10, time.Minute)
	defer rule.Stop()
	if rule.Type() != "rate_limit" {
		t.Errorf("expected type 'rate_limit', got %q", rule.Type())
	}
}

func TestRateLimitGetStats(t *testing.T) {
	rule := NewRateLimitRule(10, time.Minute)
	defer rule.Stop()

	// Make some requests
	ctx := &Context{ClientIP: "10.0.0.1"}
	rule.Evaluate(ctx)
	rule.Evaluate(ctx)

	ctx2 := &Context{ClientIP: "10.0.0.2"}
	rule.Evaluate(ctx2)

	stats := rule.GetStats()
	if stats["10.0.0.1"] != 2 {
		t.Errorf("expected 2 requests for 10.0.0.1, got %d", stats["10.0.0.1"])
	}
	if stats["10.0.0.2"] != 1 {
		t.Errorf("expected 1 request for 10.0.0.2, got %d", stats["10.0.0.2"])
	}
}

func TestRateLimitRuleStop(t *testing.T) {
	rule := NewRateLimitRule(10, time.Minute)

	// Stop should be safe to call multiple times
	rule.Stop()
	rule.Stop() // Should not panic

	// Rule should still work after stop (just no cleanup)
	ctx := &Context{ClientIP: "10.0.0.1"}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected rule to still work after stop")
	}
}

// Evaluator Tests

func TestEvaluatorNOT(t *testing.T) {
	ipRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")

	group := &Group{
		Not: ipRule,
	}

	eval := NewEvaluator()

	// IP matches the rule, so NOT should be false
	ctx := &Context{ClientIP: "10.1.2.3"}
	result := eval.EvaluateGroup(group, ctx)
	if result.Matched {
		t.Error("expected NOT group not to match when inner rule matches")
	}

	// IP doesn't match the rule, so NOT should be true
	ctx = &Context{ClientIP: "8.8.8.8"}
	result = eval.EvaluateGroup(group, ctx)
	if !result.Matched {
		t.Error("expected NOT group to match when inner rule doesn't match")
	}
}

func TestEvaluatorSingle(t *testing.T) {
	ipRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")

	group := &Group{
		Single: ipRule,
	}

	eval := NewEvaluator()

	ctx := &Context{ClientIP: "10.1.2.3"}
	result := eval.EvaluateGroup(group, ctx)
	if !result.Matched {
		t.Error("expected single rule to match")
	}

	ctx = &Context{ClientIP: "8.8.8.8"}
	result = eval.EvaluateGroup(group, ctx)
	if result.Matched {
		t.Error("expected single rule not to match")
	}
}

func TestEvaluatorNilGroup(t *testing.T) {
	eval := NewEvaluator()
	ctx := &Context{ClientIP: "10.1.2.3"}
	result := eval.EvaluateGroup(nil, ctx)
	if result.Matched {
		t.Error("expected nil group not to match")
	}
}

func TestEvaluatorEmptyGroup(t *testing.T) {
	eval := NewEvaluator()
	ctx := &Context{ClientIP: "10.1.2.3"}
	result := eval.EvaluateGroup(&Group{}, ctx)
	if result.Matched {
		t.Error("expected empty group not to match")
	}
}

// IP Rule Edge Cases

func TestIPRuleInvalidCIDR(t *testing.T) {
	_, err := NewIPRule([]string{"invalid"}, "allow")
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestIPRuleInvalidMode(t *testing.T) {
	_, err := NewIPRule([]string{"10.0.0.0/8"}, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestIPRuleInvalidClientIP(t *testing.T) {
	rule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")
	ctx := &Context{ClientIP: "invalid-ip"}
	result := rule.Evaluate(ctx)
	if result.Matched {
		t.Error("expected no match for invalid client IP")
	}
}

func TestIPRuleType(t *testing.T) {
	allowRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "allow")
	if allowRule.Type() != "ip_allow" {
		t.Errorf("expected type 'ip_allow', got %q", allowRule.Type())
	}

	denyRule, _ := NewIPRule([]string{"10.0.0.0/8"}, "deny")
	if denyRule.Type() != "ip_deny" {
		t.Errorf("expected type 'ip_deny', got %q", denyRule.Type())
	}
}

func TestIPRuleIPv6(t *testing.T) {
	rule, err := NewIPRule([]string{"2001:db8::/32"}, "allow")
	if err != nil {
		t.Fatalf("failed to create IPv6 rule: %v", err)
	}

	ctx := &Context{ClientIP: "2001:db8::1"}
	result := rule.Evaluate(ctx)
	if !result.Matched {
		t.Error("expected IPv6 address to match")
	}

	ctx = &Context{ClientIP: "2001:db9::1"}
	result = rule.Evaluate(ctx)
	if result.Matched {
		t.Error("expected different IPv6 address not to match")
	}
}

// UA Rule Edge Cases

func TestUARuleInvalidPattern(t *testing.T) {
	_, err := NewUARule([]string{"[invalid"}, "whitelist")
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestUARuleInvalidMode(t *testing.T) {
	_, err := NewUARule([]string{".*"}, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestUARuleType(t *testing.T) {
	whitelistRule, _ := NewUARule([]string{".*"}, "whitelist")
	if whitelistRule.Type() != "ua_whitelist" {
		t.Errorf("expected type 'ua_whitelist', got %q", whitelistRule.Type())
	}

	blacklistRule, _ := NewUARule([]string{".*"}, "blacklist")
	if blacklistRule.Type() != "ua_blacklist" {
		t.Errorf("expected type 'ua_blacklist', got %q", blacklistRule.Type())
	}
}

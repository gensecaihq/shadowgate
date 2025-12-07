package proxy

import (
	"testing"
	"time"
)

func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	// Should start closed
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state, got %v", cb.State())
	}

	// Should allow requests
	if !cb.Allow() {
		t.Error("expected request to be allowed in closed state")
	}
}

func TestCircuitBreakerOpensOnFailures(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Should be open now
	if cb.State() != CircuitOpen {
		t.Errorf("expected open state after %d failures, got %v", cfg.FailureThreshold, cb.State())
	}

	// Should not allow requests
	if cb.Allow() {
		t.Error("expected request to be blocked in open state")
	}
}

func TestCircuitBreakerTransitionsToHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should allow one request and transition to half-open
	if !cb.Allow() {
		t.Error("expected request to be allowed after timeout")
	}

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected half-open state, got %v", cb.State())
	}
}

func TestCircuitBreakerClosesOnSuccess(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open
	cb.Allow()

	// Record successes to close
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state after successes, got %v", cb.State())
	}
}

func TestCircuitBreakerReOpensOnFailureInHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open
	cb.Allow()

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open state, got %v", cb.State())
	}

	// Fail in half-open
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected open state after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Reset
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state after reset, got %v", cb.State())
	}

	if !cb.Allow() {
		t.Error("expected request to be allowed after reset")
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()

	if stats.State != CircuitClosed {
		t.Errorf("expected closed state in stats, got %v", stats.State)
	}

	if stats.Failures != 3 {
		t.Errorf("expected 3 failures, got %d", stats.Failures)
	}
}

func TestCircuitStateString(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()

	// Record success - should reset failure count
	cb.RecordSuccess()

	// Should still be closed
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state, got %v", cb.State())
	}

	// Need 3 more failures to open
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Errorf("expected closed state after 2 failures, got %v", cb.State())
	}

	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected open state after 3 consecutive failures, got %v", cb.State())
	}
}

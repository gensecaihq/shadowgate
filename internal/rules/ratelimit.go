package rules

import (
	"fmt"
	"sync"
	"time"
)

// RateLimitRule limits requests per source IP
type RateLimitRule struct {
	maxRequests int
	window      time.Duration
	counters    map[string]*rateLimitCounter
	mu          sync.RWMutex
	stopChan    chan struct{}
	stopped     bool
}

type rateLimitCounter struct {
	count     int
	windowEnd time.Time
}

// NewRateLimitRule creates a new rate limiting rule
func NewRateLimitRule(maxRequests int, window time.Duration) *RateLimitRule {
	r := &RateLimitRule{
		maxRequests: maxRequests,
		window:      window,
		counters:    make(map[string]*rateLimitCounter),
		stopChan:    make(chan struct{}),
	}

	// Start cleanup goroutine
	go r.cleanup()

	return r
}

// Stop stops the background cleanup goroutine
func (r *RateLimitRule) Stop() {
	r.mu.Lock()
	if !r.stopped {
		r.stopped = true
		close(r.stopChan)
	}
	r.mu.Unlock()
}

// cleanup periodically removes expired entries
func (r *RateLimitRule) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for ip, counter := range r.counters {
				if now.After(counter.windowEnd) {
					delete(r.counters, ip)
				}
			}
			r.mu.Unlock()
		}
	}
}

// Evaluate checks if the client has exceeded the rate limit
func (r *RateLimitRule) Evaluate(ctx *Context) Result {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	counter, exists := r.counters[ctx.ClientIP]

	if !exists || now.After(counter.windowEnd) {
		// Start new window
		r.counters[ctx.ClientIP] = &rateLimitCounter{
			count:     1,
			windowEnd: now.Add(r.window),
		}
		return Result{
			Matched: true,
			Reason:  fmt.Sprintf("rate limit: 1/%d requests", r.maxRequests),
			Labels:  []string{"rate-ok"},
		}
	}

	counter.count++
	if counter.count > r.maxRequests {
		return Result{
			Matched: false,
			Reason:  fmt.Sprintf("rate limit exceeded: %d/%d requests in window", counter.count, r.maxRequests),
			Labels:  []string{"rate-exceeded"},
		}
	}

	return Result{
		Matched: true,
		Reason:  fmt.Sprintf("rate limit: %d/%d requests", counter.count, r.maxRequests),
		Labels:  []string{"rate-ok"},
	}
}

// Type returns the rule type
func (r *RateLimitRule) Type() string {
	return "rate_limit"
}

// GetStats returns current rate limit statistics
func (r *RateLimitRule) GetStats() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]int)
	for ip, counter := range r.counters {
		stats[ip] = counter.count
	}
	return stats
}

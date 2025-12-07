package listener

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestHTTPListener(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	listener := NewHTTPListener(HTTPListenerConfig{
		Addr:    "127.0.0.1:0", // Use port 0 to get a random available port
		Handler: handler,
	})

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Stop(ctx)

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Get the actual bound address
	addr := listener.Addr()
	t.Logf("Listener bound to: %s", addr)

	// Make a test request
	resp, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("expected body 'OK', got %q", string(body))
	}
}

func TestHTTPListenerStop(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	listener := NewHTTPListener(HTTPListenerConfig{
		Addr:    "127.0.0.1:18081",
		Handler: handler,
	})

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	// Stop should not error
	if err := listener.Stop(ctx); err != nil {
		t.Errorf("failed to stop listener: %v", err)
	}
}

func TestHTTPListenerConnectionTracking(t *testing.T) {
	// Create a handler that holds the connection briefly
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done // Wait for signal
		w.WriteHeader(http.StatusOK)
	})

	listener := NewHTTPListener(HTTPListenerConfig{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Stop(ctx)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Initial connection count should be 0
	if count := listener.ActiveConnections(); count != 0 {
		t.Errorf("expected 0 initial connections, got %d", count)
	}

	// Make a request in background
	go func() {
		http.Get("http://" + listener.Addr())
	}()

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Should have at least 1 active connection
	if count := listener.ActiveConnections(); count < 1 {
		t.Errorf("expected at least 1 active connection, got %d", count)
	}

	// Release the handler
	close(done)

	// Wait for connection to close
	time.Sleep(100 * time.Millisecond)

	// Connection count should go back down
	if count := listener.ActiveConnections(); count > 1 {
		t.Errorf("expected connections to decrease, got %d", count)
	}
}

func TestHTTPListenerGracefulShutdown(t *testing.T) {
	requestStarted := make(chan struct{})
	requestComplete := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(200 * time.Millisecond) // Simulate slow request
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("done"))
		close(requestComplete)
	})

	listener := NewHTTPListener(HTTPListenerConfig{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})

	ctx := context.Background()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Start a slow request
	go http.Get("http://" + listener.Addr())

	// Wait for request to start
	<-requestStarted

	// Start graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Shutdown should wait for the request to complete
	err := listener.Stop(shutdownCtx)
	if err != nil {
		t.Errorf("graceful shutdown returned error: %v", err)
	}

	// Request should have completed
	select {
	case <-requestComplete:
		// Success - request completed before shutdown finished
	default:
		t.Error("request did not complete during graceful shutdown")
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shadowgate/internal/admin"
	"shadowgate/internal/config"
	"shadowgate/internal/gateway"
	"shadowgate/internal/geoip"
	"shadowgate/internal/logging"
	"shadowgate/internal/metrics"
	"shadowgate/internal/profile"
	"shadowgate/internal/proxy"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// Command-line flags
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	validateOnly := flag.Bool("validate", false, "validate configuration and exit")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()

	// Version info
	if *showVersion {
		fmt.Printf("shadowgate %s (commit: %s, built: %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	// Load and validate configuration
	fmt.Printf("Loading configuration from: %s\n", *configPath)
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *validateOnly {
		fmt.Println("Configuration is valid")
		os.Exit(0)
	}

	// Initialize logging
	logger, err := logging.New(logging.Config{
		Level:  cfg.Global.Log.Level,
		Format: cfg.Global.Log.Format,
		Output: cfg.Global.Log.Output,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Info("ShadowGate starting", map[string]interface{}{
		"version":  version,
		"profiles": len(cfg.Profiles),
	})

	// Initialize GeoIP if configured
	if cfg.Global.GeoIPDBPath != "" {
		if err := geoip.LoadGlobal(cfg.Global.GeoIPDBPath); err != nil {
			logger.Warn("Failed to load GeoIP database", map[string]interface{}{
				"path":  cfg.Global.GeoIPDBPath,
				"error": err.Error(),
			})
		} else {
			logger.Info("GeoIP database loaded", map[string]interface{}{
				"path": cfg.Global.GeoIPDBPath,
			})
			defer geoip.CloseGlobal()
		}
	}

	// Initialize metrics
	metricsCollector := metrics.New()

	// Track backend pools for admin API
	backendPools := make(map[string]*proxy.Pool)

	// Create profile manager
	profileMgr := profile.NewManager()

	// Handler factory creates gateway handlers for each profile
	handlerFactory := func(p *profile.Profile) http.Handler {
		// Create backend pool first (shared with admin API for health checking)
		pool := proxy.NewPool()
		for _, bc := range p.Config.Backends {
			weight := bc.Weight
			if weight == 0 {
				weight = 1
			}
			backend, err := proxy.NewBackend(bc.Name, bc.URL, weight)
			if err != nil {
				logger.Error("Failed to create backend", map[string]interface{}{
					"profile": p.ID,
					"backend": bc.Name,
					"error":   err.Error(),
				})
				continue
			}
			pool.Add(backend)
		}
		backendPools[p.ID] = pool

		// Create handler with the shared pool
		h, err := gateway.NewHandler(gateway.Config{
			ProfileID:   p.ID,
			Profile:     p.Config,
			Logger:      logger,
			Metrics:     metricsCollector,
			BackendPool: pool,
		})
		if err != nil {
			logger.Error("Failed to create handler", map[string]interface{}{
				"profile": p.ID,
				"error":   err.Error(),
			})
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
		}

		return h
	}

	// Load profiles from config
	if err := profileMgr.LoadFromConfig(cfg, handlerFactory); err != nil {
		logger.Error("Failed to load profiles", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	// Reload function for admin API (validates config, requires restart for changes)
	reloadFunc := func() error {
		newCfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		// Note: Currently only validates config. Actual changes require restart.
		// TODO: Implement hot handler swapping for true hot reload.
		logger.Info("Configuration validated", map[string]interface{}{
			"profiles": len(newCfg.Profiles),
			"note":     "restart required for changes to take effect",
		})
		return nil
	}

	// Start Admin API if configured
	var adminAPI *admin.API
	if cfg.Global.MetricsAddr != "" {
		adminAPI = admin.New(admin.Config{
			Addr:       cfg.Global.MetricsAddr,
			Metrics:    metricsCollector,
			ReloadFunc: reloadFunc,
			Version:    version,
		})

		// Register backend pools
		for profileID, pool := range backendPools {
			adminAPI.RegisterPool(profileID, pool)
		}

		if err := adminAPI.Start(); err != nil {
			logger.Error("Failed to start admin API", map[string]interface{}{
				"addr":  cfg.Global.MetricsAddr,
				"error": err.Error(),
			})
		} else {
			logger.Info("Admin API started", map[string]interface{}{
				"addr": cfg.Global.MetricsAddr,
			})
		}
	}

	// Start health checks for all backend pools
	healthCheckers := make([]*proxy.HealthChecker, 0)
	for profileID, pool := range backendPools {
		checker := proxy.NewHealthChecker(pool, proxy.HealthConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
			Path:     "/",
		})
		checker.Start()
		healthCheckers = append(healthCheckers, checker)
		logger.Info("Health checker started", map[string]interface{}{
			"profile": profileID,
		})
	}

	// Start all profiles (listeners)
	ctx := context.Background()
	if err := profileMgr.Start(ctx); err != nil {
		logger.Error("Failed to start profiles", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	logger.Info("ShadowGate started", map[string]interface{}{
		"profiles": len(cfg.Profiles),
	})
	fmt.Printf("ShadowGate running with %d profile(s). Press Ctrl+C to stop.\n", len(cfg.Profiles))

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGHUP:
			logger.Info("Received SIGHUP, validating configuration", nil)
			fmt.Println("Received SIGHUP, validating configuration...")

			if err := reloadFunc(); err != nil {
				logger.Error("Configuration validation failed", map[string]interface{}{
					"error": err.Error(),
				})
				fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
				continue
			}

			fmt.Println("Configuration valid. Restart required for changes to take effect.")

		case syscall.SIGINT, syscall.SIGTERM:
			logger.Info("Shutting down", nil)
			fmt.Println("Shutting down...")

			// Stop health checkers
			for _, checker := range healthCheckers {
				checker.Stop()
			}

			// Stop admin API
			if adminAPI != nil {
				shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				adminAPI.Stop(shutdownCtx)
				cancel()
			}

			// Stop all profiles
			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := profileMgr.Stop(shutdownCtx); err != nil {
				logger.Error("Error during shutdown", map[string]interface{}{
					"error": err.Error(),
				})
			}
			cancel()

			logger.Info("Shutdown complete", nil)
			os.Exit(0)
		}
	}
}

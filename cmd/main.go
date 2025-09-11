package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/metrics"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/monitor"
)

func main() {
	cfg := config.ParseFlags(os.Args[1:])

	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	gatewayMonitor, err := monitor.New(cfg)
	if err != nil {
		slog.Error("Failed to create gateway monitor", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting gateway monitor", 
		"check_period", cfg.CheckPeriod, 
		"timeout", cfg.Timeout)

	// Create context for graceful shutdown with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start metrics server
	if err := metrics.StartMetricsServer(ctx, cancel, cfg.MetricsPort); err != nil {
		slog.Error("Failed to start metrics server", "error", err)
		os.Exit(1)
	}

	// Run the monitoring loop
	if err := gatewayMonitor.Run(ctx); err != nil {
		slog.Error("Gateway monitor exited with error", "error", err)
		os.Exit(1)
	}
}

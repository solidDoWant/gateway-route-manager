package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/metrics"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/monitor"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() (err error) {
	cfg := config.ParseFlags(os.Args[1:])

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	slog.SetLogLoggerLevel(cfg.GetSlogLevel())

	gatewayMonitor, err := monitor.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create gateway monitor: %w", err)
	}

	defer func() {
		closeErr := gatewayMonitor.Close()
		err = errors.Join(err, fmt.Errorf("failed to close gateway monitor: %w", closeErr))
	}()

	slog.Info("Starting gateway monitor", "check_period", cfg.CheckPeriod, "timeout", cfg.Timeout)

	// Create context for graceful shutdown with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start metrics server
	if err := metrics.StartMetricsServer(ctx, cancel, cfg.MetricsPort); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to start metrics server: %w", err)
	}

	// Run the monitoring loop
	if err := gatewayMonitor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("gateway monitor error: %w", err)
	}

	return nil
}

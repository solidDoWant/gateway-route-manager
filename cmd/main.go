package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/ddns"
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

	// Create context for graceful shutdown with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	promMetrics, err := metrics.New(prometheus.DefaultRegisterer) // TODO provide as arg
	if err != nil {
		return fmt.Errorf("failed to create metrics: %w", err)
	}

	ddnsUpdater, err := runDDNSUpdater(ctx, cfg, promMetrics)
	if err != nil {
		return fmt.Errorf("failed to start DDNS updater: %w", err)
	}

	// Start the gateway
	gatewayMonitor, err := monitor.New(cfg, promMetrics, ddnsUpdater)
	if err != nil {
		return fmt.Errorf("failed to create gateway monitor: %w", err)
	}

	defer func() {
		closeErr := gatewayMonitor.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("failed to close gateway monitor: %w", closeErr)
		}
		err = errors.Join(err, closeErr)
	}()

	slog.Info("Starting gateway monitor", "check_period", cfg.CheckPeriod, "timeout", cfg.Timeout)

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

// Runs the DDNS updater in a goroutine and handles cleanup
func runDDNSUpdater(ctx context.Context, cfg config.Config, metrics *metrics.Metrics) (*ddns.Updater, error) {
	ddnsUpdater, err := ddns.NewUpdater(cfg, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create DDNS updater: %w", err)
	}

	slog.Info("Starting DDNS updater", "timeout", cfg.DDNSTimeout)
	go func() {
		defer ddnsUpdater.Close()
		ddnsUpdater.Run(ctx)
	}()

	return ddnsUpdater, nil
}

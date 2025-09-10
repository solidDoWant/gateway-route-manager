package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/monitor"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/server"
)

func main() {
	cfg := config.ParseFlags()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	gatewayMonitor, err := monitor.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create gateway monitor: %v", err)
	}

	log.Printf("Starting gateway monitor with check period: %v, timeout: %v",
		cfg.CheckPeriod, cfg.Timeout)

	// Create context for graceful shutdown with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start metrics server
	if err := server.StartMetricsServer(ctx, cancel, cfg.MetricsPort); err != nil {
		log.Fatalf("Failed to start metrics server: %v", err)
	}

	// Run the monitoring loop
	if err := gatewayMonitor.Run(ctx); err != nil {
		log.Fatalf("Gateway monitor exited with error: %v", err)
	}
}

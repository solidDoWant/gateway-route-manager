package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartMetricsServer starts the Prometheus metrics HTTP server
func StartMetricsServer(ctx context.Context, cancel context.CancelFunc, port int) error {
	// Start metrics server
	metricsAddr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr: metricsAddr,
	}
	http.Handle("/metrics", promhttp.Handler())

	// Create a listener to signal when the server is ready
	listener, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		return fmt.Errorf("failed to bind to %s: %v", metricsAddr, err)
	}
	go func() {
		log.Printf("Starting metrics server on %s", metricsAddr)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Metrics server failed: %v", err)
		}
		cancel() // Cancel main context when the metrics server is stopped
	}()

	// Start server shutdown goroutine
	go func() {
		<-ctx.Done()
		log.Println("Shutting down metrics server...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Metrics server shutdown error: %v", err)
		}
	}()

	return nil
}

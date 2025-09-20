package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/ddns"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/gateway"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/metrics"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/routes"
)

// GatewayMonitor manages the monitoring of gateways and route updates
type GatewayMonitor struct {
	config       config.Config
	gateways     []gateway.Gateway
	client       *http.Client
	metrics      *metrics.Metrics
	routeManager routes.Manager
	ddnsUpdater  *ddns.Updater
}

// New creates a new GatewayMonitor instance
func New(cfg config.Config, metrics *metrics.Metrics, ddnsUpdater *ddns.Updater) (*GatewayMonitor, error) {
	gateways, err := gateway.GenerateGateways(cfg.StartIP, cfg.EndIP, cfg.Port, cfg.URLPath, cfg.Scheme, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to generate gateways: %w", err)
	}

	// Set total gateway count
	metrics.TotalGatewayCount.Set(float64(len(gateways)))

	routeManager, err := routes.NewNetlinkManager(cfg.CIDRsToExclude, cfg.FirstRoutingTableID, cfg.FirstRulePreference)
	if err != nil {
		return nil, fmt.Errorf("failed to create route manager: %w", err)
	}

	var ddnsProvider ddns.Provider
	if cfg.IsDDNSEnabled() {
		ddnsProvider, err = ddns.NewProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create DDNS provider: %w", err)
		}
		slog.Info("DDNS enabled", "provider", ddnsProvider.Name(), "hostname", cfg.DDNSHostname)
	}

	return &GatewayMonitor{
		config:   cfg,
		gateways: gateways,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		metrics:      metrics,
		routeManager: routeManager,
		ddnsUpdater:  ddnsUpdater,
	}, nil
}

func (gm *GatewayMonitor) Close() error {
	if closeableManager, ok := gm.routeManager.(routes.CloseableManager); ok {
		return closeableManager.Close()
	}

	return nil
}

// Run starts the main monitoring loop
func (gm *GatewayMonitor) Run(ctx context.Context) error {
	ticker := time.NewTicker(gm.config.CheckPeriod)
	defer ticker.Stop()

	// Initial check
	if err := gm.performCheckCycle(ctx); err != nil {
		return err
	}

	// Periodic checks
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "Gateway monitor stopped")
			return nil
		case <-ticker.C:
			if err := gm.performCheckCycle(ctx); err != nil {
				return err
			}
		}
	}
}

func (gm *GatewayMonitor) performCheckCycle(ctx context.Context) error {
	start := time.Now()
	gm.checkGateways(ctx)

	// Collect active gateways
	activeGateways := make([]gateway.Gateway, 0, len(gm.gateways))
	for _, gateway := range gm.gateways {
		if gateway.IsActive {
			activeGateways = append(activeGateways, gateway)
		}
	}

	if err := gm.updateRoutes(activeGateways); err != nil {
		gm.metrics.ErrorsTotal.WithLabelValues("route_error").Inc()
		return fmt.Errorf("failed to update routes: %w", err)
	}

	// This must be done after the routes are updated to ensure that the DDNS provider
	// can make network requests
	gm.ddnsUpdater.ScheduleUpdate(activeGateways)

	gm.metrics.CheckCycleDurationSeconds.Observe(time.Since(start).Seconds())
	gm.metrics.CheckCyclesTotal.Inc()
	return nil
}

func (gm *GatewayMonitor) checkGateways(ctx context.Context) {
	slog.DebugContext(ctx, "Checking gateways", "count", len(gm.gateways))

	var wg sync.WaitGroup

	for i := range gm.gateways {
		wg.Add(1)
		go func(gateway *gateway.Gateway) {
			defer wg.Done()
			gateway.IsActive = gm.checkGateway(ctx, gateway)

			if gateway.IsActive {
				gateway.ConsecutiveFailures = 0
			} else {
				gateway.ConsecutiveFailures++
			}

			// Update consecutive failures metric
			gm.metrics.ConsecutiveFailures.WithLabelValues(gateway.IP.String()).Set(float64(gateway.ConsecutiveFailures))
		}(&gm.gateways[i])
	}

	wg.Wait()

	activeCount := 0
	for _, gateway := range gm.gateways {
		if gateway.IsActive {
			activeCount++
		}
	}

	// Update metrics
	gm.metrics.ActiveGatewayCount.Set(float64(activeCount))

	slog.DebugContext(ctx, "Gateway check complete", "active_count", activeCount, "total_count", len(gm.gateways))
}

func (gm *GatewayMonitor) checkGateway(ctx context.Context, gw *gateway.Gateway) bool {
	start := time.Now()
	gatewayIP := gw.IP.String()

	reqCtx, cancel := context.WithTimeout(ctx, gm.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", gw.URL, nil)
	if err != nil {
		gm.metrics.ErrorsTotal.WithLabelValues("network_error").Inc()
		gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "failure").Inc()
		gm.metrics.HealthCheckDurationSeconds.WithLabelValues(gatewayIP).Observe(time.Since(start).Seconds())
		slog.DebugContext(ctx, "Failed to create request", "gateway", gw.IP, "error", err)
		return false
	}

	resp, err := gm.client.Do(req)
	duration := time.Since(start).Seconds()

	if err != nil {
		errorType := "network_error"
		if errors.Is(err, context.DeadlineExceeded) {
			errorType = "timeout"
		}
		gm.metrics.ErrorsTotal.WithLabelValues(errorType).Inc()
		gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "failure").Inc()
		gm.metrics.HealthCheckDurationSeconds.WithLabelValues(gatewayIP).Observe(duration)
		gm.metrics.HTTPRequestDurationSeconds.WithLabelValues(gatewayIP).Observe(duration)

		slog.DebugContext(ctx, "Health check failed", "gateway", gw.IP, "error", err)
		return false
	}
	defer resp.Body.Close()

	// Record HTTP metrics
	gm.metrics.HTTPRequestsTotal.WithLabelValues(gatewayIP, strconv.Itoa(resp.StatusCode), "GET").Inc()
	gm.metrics.HTTPRequestDurationSeconds.WithLabelValues(gatewayIP).Observe(duration)
	gm.metrics.HealthCheckDurationSeconds.WithLabelValues(gatewayIP).Observe(duration)

	// Check for any 2xx status code (200-299) as successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "success").Inc()
		slog.DebugContext(ctx, "Gateway is healthy", "gateway", gw.IP, "status", resp.StatusCode)
		return true
	}

	gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "failure").Inc()
	gm.metrics.ErrorsTotal.WithLabelValues("invalid_response").Inc()
	slog.DebugContext(ctx, "Gateway returned unhealthy status", "gateway", gw.IP, "status", resp.StatusCode)
	return false
}

func (gm *GatewayMonitor) updateRoutes(activeGateways []gateway.Gateway) error {
	start := time.Now()
	defer func() {
		gm.metrics.RouteUpdateDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	activeGatewayAddresses := make([]net.IP, len(activeGateways))
	for i, gw := range activeGateways {
		activeGatewayAddresses[i] = gw.IP
	}

	if err := gm.routeManager.UpdateRoutes(gm.config.Routes, activeGatewayAddresses); err != nil {
		gm.metrics.RouteUpdatesTotal.WithLabelValues("update", "failure").Inc()
		return err
	}

	gm.metrics.RouteUpdatesTotal.WithLabelValues("update", "success").Inc()
	gm.metrics.DefaultRouteGateways.Set(float64(len(activeGateways)))
	return nil
}

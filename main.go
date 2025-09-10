package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vishvananda/netlink"
)

type Config struct {
	StartIP     string
	EndIP       string
	Timeout     time.Duration
	CheckPeriod time.Duration
	Port        int
	URLPath     string
	Scheme      string
	Verbose     bool
	MetricsPort int
}

type Gateway struct {
	IP                  net.IP
	URL                 string
	IsActive            bool
	ConsecutiveFailures int
}

type Metrics struct {
	// Gateway Health Metrics
	HealthCheckTotal           *prometheus.CounterVec
	HealthCheckDurationSeconds *prometheus.HistogramVec
	ActiveGatewayCount         prometheus.Gauge
	TotalGatewayCount          prometheus.Gauge

	// Route Management Metrics
	RouteUpdatesTotal          *prometheus.CounterVec
	RouteUpdateDurationSeconds prometheus.Histogram
	DefaultRouteGateways       prometheus.Gauge

	// HTTP Client Metrics
	HTTPRequestsTotal          *prometheus.CounterVec
	HTTPRequestDurationSeconds *prometheus.HistogramVec

	// Application Metrics
	CheckCyclesTotal          prometheus.Counter
	CheckCycleDurationSeconds prometheus.Histogram
	ApplicationUptimeSeconds  prometheus.Gauge

	// Error Metrics
	ErrorsTotal         *prometheus.CounterVec
	ConsecutiveFailures *prometheus.GaugeVec
}

type GatewayMonitor struct {
	config   Config
	gateways []Gateway
	client   *http.Client
	metrics  *Metrics
}

func main() {
	config := parseFlags()

	if err := validateConfig(config); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	monitor, err := NewGatewayMonitor(config)
	if err != nil {
		log.Fatalf("Failed to create gateway monitor: %v", err)
	}

	log.Printf("Starting gateway monitor with %d gateways, check period: %v, timeout: %v",
		len(monitor.gateways), config.CheckPeriod, config.Timeout)

	// Create context for graceful shutdown with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start metrics server
	if err := startMetricsServer(ctx, cancel, config.MetricsPort); err != nil {
		log.Fatalf("Failed to start metrics server: %v", err)
	}

	// Run the monitoring loop
	if err := monitor.Run(ctx); err != nil {
		log.Fatalf("Gateway monitor exited with error: %v", err)
	}
}

func parseFlags() Config {
	var config Config

	flag.StringVar(&config.StartIP, "start-ip", "", "Starting IP address for the range")
	flag.StringVar(&config.EndIP, "end-ip", "", "Ending IP address for the range")
	flag.DurationVar(&config.Timeout, "timeout", 1*time.Second, "Timeout for health checks")
	flag.DurationVar(&config.CheckPeriod, "check-period", 3*time.Second, "How often to check gateways")
	flag.IntVar(&config.Port, "port", 80, "Port to target for health checks")
	flag.StringVar(&config.URLPath, "path", "/", "URL path for health checks")
	flag.StringVar(&config.Scheme, "scheme", "http", "Scheme to use (http or https)")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")
	flag.IntVar(&config.MetricsPort, "metrics-port", 9090, "Port for Prometheus metrics endpoint")

	flag.Parse()

	return config
}

func validateConfig(config Config) error {
	if config.StartIP == "" || config.EndIP == "" {
		return fmt.Errorf("start-ip and end-ip are required")
	}

	// Validate that start and end IPs are valid
	startIP := net.ParseIP(config.StartIP)
	if startIP == nil {
		return fmt.Errorf("invalid start-ip: %s", config.StartIP)
	}

	endIP := net.ParseIP(config.EndIP)
	if endIP == nil {
		return fmt.Errorf("invalid end-ip: %s", config.EndIP)
	}

	// Validate that end IP is after start IP
	if startIP.Equal(endIP) {
		// Allow equal IPs (single IP range)
	} else if isIPGreater(startIP, endIP) {
		return fmt.Errorf("start-ip (%s) must be less than or equal to end-ip (%s)", config.StartIP, config.EndIP)
	}

	if config.CheckPeriod < config.Timeout {
		return fmt.Errorf("check-period (%v) must be at least as long as timeout (%v)",
			config.CheckPeriod, config.Timeout)
	}

	if config.Scheme != "http" && config.Scheme != "https" {
		return fmt.Errorf("scheme must be 'http' or 'https'")
	}

	if config.MetricsPort < 1 || config.MetricsPort > 65535 {
		return fmt.Errorf("metrics port must be between 1 and 65535")
	}

	return nil
}

func startMetricsServer(ctx context.Context, cancel context.CancelFunc, port int) error {
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

func NewMetrics() (*Metrics, error) {
	metrics := &Metrics{
		// Gateway Health Metrics
		HealthCheckTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_health_check_total",
				Help: "Total number of health checks performed",
			},
			[]string{"gateway_ip", "status"},
		),
		HealthCheckDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gateway_health_check_duration_seconds",
				Help:    "Duration of health checks",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"gateway_ip"},
		),
		ActiveGatewayCount: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "gateway_active_count",
				Help: "Current number of active/healthy gateways",
			},
		),
		TotalGatewayCount: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "gateway_total_count",
				Help: "Total number of configured gateways",
			},
		),

		// Route Management Metrics
		RouteUpdatesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "route_updates_total",
				Help: "Total number of route update attempts",
			},
			[]string{"operation", "status"},
		),
		RouteUpdateDurationSeconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "route_update_duration_seconds",
				Help:    "Time taken to update routes",
				Buckets: prometheus.DefBuckets,
			},
		),
		DefaultRouteGateways: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "default_route_gateways_count",
				Help: "Current number of gateways in the default route",
			},
		),

		// HTTP Client Metrics
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total HTTP requests made to gateways",
			},
			[]string{"gateway_ip", "status_code", "method"},
		),
		HTTPRequestDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"gateway_ip"},
		),

		// Application Metrics
		CheckCyclesTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "check_cycles_total",
				Help: "Total number of gateway check cycles completed",
			},
		),
		CheckCycleDurationSeconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "check_cycle_duration_seconds",
				Help:    "Duration of complete check cycles",
				Buckets: prometheus.DefBuckets,
			},
		),
		ApplicationUptimeSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "application_uptime_seconds",
				Help: "Application uptime in seconds",
			},
		),

		// Error Metrics
		ErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "errors_total",
				Help: "Total errors encountered",
			},
			[]string{"type"},
		),
		ConsecutiveFailures: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "consecutive_failures_count",
				Help: "Current consecutive failures per gateway",
			},
			[]string{"gateway_ip"},
		),
	}
	prometheus.MustRegister()

	// Register all metrics
	collectors := []prometheus.Collector{
		metrics.HealthCheckTotal,
		metrics.HealthCheckDurationSeconds,
		metrics.ActiveGatewayCount,
		metrics.TotalGatewayCount,
		metrics.RouteUpdatesTotal,
		metrics.RouteUpdateDurationSeconds,
		metrics.DefaultRouteGateways,
		metrics.HTTPRequestsTotal,
		metrics.HTTPRequestDurationSeconds,
		metrics.CheckCyclesTotal,
		metrics.CheckCycleDurationSeconds,
		metrics.ApplicationUptimeSeconds,
		metrics.ErrorsTotal,
		metrics.ConsecutiveFailures,
	}

	for _, collector := range collectors {
		if err := prometheus.Register(collector); err != nil {
			return nil, fmt.Errorf("failed to register metric: %v", err)
		}
	}

	return metrics, nil
}

func NewGatewayMonitor(config Config) (*GatewayMonitor, error) {
	gateways, err := generateGateways(config.StartIP, config.EndIP, config.Port, config.URLPath, config.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to generate gateways: %v", err)
	}

	metrics, err := NewMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %v", err)
	}

	// Set total gateway count
	metrics.TotalGatewayCount.Set(float64(len(gateways)))

	return &GatewayMonitor{
		config:   config,
		gateways: gateways,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		metrics: metrics,
	}, nil
}

func generateGateways(startIPStr, endIPStr string, port int, path, scheme string) ([]Gateway, error) {
	startIP := net.ParseIP(startIPStr)
	if startIP == nil {
		return nil, fmt.Errorf("invalid start IP: %s", startIPStr)
	}

	endIP := net.ParseIP(endIPStr)
	if endIP == nil {
		return nil, fmt.Errorf("invalid end IP: %s", endIPStr)
	}

	// Convert to 4-byte representation for easier iteration
	if startIP.To4() != nil {
		startIP = startIP.To4()
	}
	if endIP.To4() != nil {
		endIP = endIP.To4()
	}

	var gateways []Gateway

	// Create a copy of startIP to iterate
	currentIP := make(net.IP, len(startIP))
	copy(currentIP, startIP)

	// Iterate from startIP to endIP (inclusive)
	for {
		// Create a copy of the current IP
		ipCopy := make(net.IP, len(currentIP))
		copy(ipCopy, currentIP)

		url := fmt.Sprintf("%s://%s:%d%s", scheme, ipCopy.String(), port, path)

		gateways = append(gateways, Gateway{
			IP:  ipCopy,
			URL: url,
		})

		// Check if we've reached the end IP
		if currentIP.Equal(endIP) {
			break
		}

		// Increment IP
		incrementIP(currentIP)

		// Safety check to prevent infinite loop
		if isIPGreater(currentIP, endIP) {
			break
		}
	}

	return gateways, nil
}

// Helper function to check if ip1 > ip2
func isIPGreater(ip1, ip2 net.IP) bool {
	// Ensure both IPs are the same length
	if len(ip1) != len(ip2) {
		return false
	}

	for i := range ip1 {
		if ip1[i] == ip2[i] {
			continue
		}

		return ip1[i] > ip2[i]
	}

	return false // They are equal
}

func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

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
			log.Println("Gateway monitor stopped")
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
	if err := gm.updateRoutes(); err != nil {
		gm.metrics.ErrorsTotal.WithLabelValues("route_error").Inc()
		return fmt.Errorf("failed to update routes: %v", err)
	}
	gm.metrics.CheckCycleDurationSeconds.Observe(time.Since(start).Seconds())
	gm.metrics.CheckCyclesTotal.Inc()
	return nil
}

func (gm *GatewayMonitor) checkGateways(ctx context.Context) {
	log.Printf("Checking %d gateways...", len(gm.gateways))

	var wg sync.WaitGroup

	for i := range gm.gateways {
		wg.Add(1)
		go func(gateway *Gateway) {
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

	log.Printf("Gateway check complete: %d/%d gateways active", activeCount, len(gm.gateways))
}

func (gm *GatewayMonitor) checkGateway(ctx context.Context, gateway *Gateway) bool {
	start := time.Now()
	gatewayIP := gateway.IP.String()

	reqCtx, cancel := context.WithTimeout(ctx, gm.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", gateway.URL, nil)
	if err != nil {
		gm.metrics.ErrorsTotal.WithLabelValues("network_error").Inc()
		gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "failure").Inc()
		gm.metrics.HealthCheckDurationSeconds.WithLabelValues(gatewayIP).Observe(time.Since(start).Seconds())
		if gm.config.Verbose {
			log.Printf("Failed to create request for %s: %v", gateway.IP, err)
		}
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

		if gm.config.Verbose {
			log.Printf("Health check failed for %s: %v", gateway.IP, err)
		}
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
		if gm.config.Verbose {
			log.Printf("Gateway %s is healthy (status %d)", gateway.IP, resp.StatusCode)
		}
		return true
	}

	gm.metrics.HealthCheckTotal.WithLabelValues(gatewayIP, "failure").Inc()
	gm.metrics.ErrorsTotal.WithLabelValues("invalid_response").Inc()
	if gm.config.Verbose {
		log.Printf("Gateway %s returned status %d", gateway.IP, resp.StatusCode)
	}
	return false
}

func (gm *GatewayMonitor) updateRoutes() error {
	start := time.Now()
	defer func() {
		gm.metrics.RouteUpdateDurationSeconds.Observe(time.Since(start).Seconds())
	}()

	activeGateways := make([]net.IP, 0, len(gm.gateways))
	for _, gateway := range gm.gateways {
		if gateway.IsActive {
			activeGateways = append(activeGateways, gateway.IP)
		}
	}

	if err := gm.updateDefaultRoute(activeGateways); err != nil {
		gm.metrics.RouteUpdatesTotal.WithLabelValues("update", "failure").Inc()
		return err
	}

	gm.metrics.RouteUpdatesTotal.WithLabelValues("update", "success").Inc()
	gm.metrics.DefaultRouteGateways.Set(float64(len(activeGateways)))
	return nil
}

// Update the default route to use ECMP with the provided active gateways.
// Only returns an error if a fatal error occurs during route manipulation.
func (gm *GatewayMonitor) updateDefaultRoute(activeGateways []net.IP) error {
	if len(activeGateways) == 0 {
		// Remove existing default route if no gateways are active
		if err := gm.removeDefaultRoute(); err != nil {
			return fmt.Errorf("failed to remove default route: %v", err)
		}

		log.Println("No active gateways, default route removed")
		return nil
	}

	// Sort gateways for consistent ordering
	sort.Slice(activeGateways, func(i, j int) bool {
		return activeGateways[i].String() < activeGateways[j].String()
	})

	// Replace existing default route with new ECMP route
	return gm.replaceDefaultRouteECMP(activeGateways)
}

func (gm *GatewayMonitor) removeDefaultRoute() error {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		gm.metrics.RouteUpdatesTotal.WithLabelValues("remove", "failure").Inc()
		return fmt.Errorf("failed to list routes: %v", err)
	}

	for _, route := range routes {
		if !isDefaultRoute(route) {
			continue
		}

		if err := netlink.RouteDel(&route); err != nil {
			gm.metrics.RouteUpdatesTotal.WithLabelValues("remove", "failure").Inc()
			return fmt.Errorf("failed to delete default route via %s: %v", route.Gw, err)
		}

		gm.metrics.RouteUpdatesTotal.WithLabelValues("remove", "success").Inc()
		log.Printf("Removed default route via %s", route.Gw)
	}

	return nil
}

func (gm *GatewayMonitor) replaceDefaultRouteECMP(gateways []net.IP) error {
	if len(gateways) == 0 {
		return nil
	}

	// Create multipath route for ECMP
	nexthops := make([]*netlink.NexthopInfo, 0, len(gateways))
	for _, gateway := range gateways {
		nexthops = append(nexthops, &netlink.NexthopInfo{
			Gw: gateway,
		})
	}

	route := &netlink.Route{
		Dst: &net.IPNet{
			IP:   net.IPv4zero,
			Mask: net.CIDRMask(0, 32),
		},
		MultiPath: nexthops,
	}

	// Try to replace existing route first
	if err := netlink.RouteReplace(route); err != nil {
		// If replace fails, try to add (in case no route exists)
		if err := netlink.RouteAdd(route); err != nil {
			gm.metrics.RouteUpdatesTotal.WithLabelValues("replace", "failure").Inc()
			return fmt.Errorf("failed to replace/add ECMP route: %v", err)
		}
		gm.metrics.RouteUpdatesTotal.WithLabelValues("add", "success").Inc()
	} else {
		gm.metrics.RouteUpdatesTotal.WithLabelValues("replace", "success").Inc()
	}

	gatewayStrings := make([]string, 0, len(gateways))
	for _, gw := range gateways {
		gatewayStrings = append(gatewayStrings, gw.String())
	}
	log.Printf("Updated ECMP default route via gateways: %v", gatewayStrings)

	return nil
}

// Returns true if the route is a default route (targeting 0.0.0.0/0)
func isDefaultRoute(route netlink.Route) bool {
	if route.Gw == nil {
		return false
	}

	if route.Dst == nil {
		return true
	}

	if !route.Dst.IP.Equal(net.IPv4zero) {
		return false
	}

	ones, bits := route.Dst.Mask.Size()
	return ones == 0 && bits == 32
}

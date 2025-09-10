package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the gateway route manager
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

// New creates and registers all Prometheus metrics
func New() (*Metrics, error) {
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

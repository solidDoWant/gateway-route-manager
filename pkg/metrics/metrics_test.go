package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	metrics, err := New(prometheus.NewRegistry())
	require.NoError(t, err, "New() should not return an error")
	require.NotNil(t, metrics, "New() should return a non-nil Metrics struct")

	// Test that all metric fields are not nil
	t.Run("all metrics are non-nil", func(t *testing.T) {
		v := reflect.ValueOf(metrics).Elem()
		typ := v.Type()

		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldName := typ.Field(i).Name

			require.False(t, field.IsNil(), "metric field %s should not be nil", fieldName)
		}
	})

	// Test that all metrics are registered
	t.Run("all metrics are registered", func(t *testing.T) {
		// For this test, we'll verify that we can use the metrics without error
		// This indirectly confirms they're properly registered
		require.NotPanics(t, func() {
			// Touch each metric to ensure they're accessible
			metrics.HealthCheckTotal.WithLabelValues("test", "test")
			metrics.HealthCheckDurationSeconds.WithLabelValues("test")
			metrics.ActiveGatewayCount.Set(0)
			metrics.TotalGatewayCount.Set(0)
			metrics.RouteUpdatesTotal.WithLabelValues("test", "test")
			metrics.RouteUpdateDurationSeconds.Observe(0)
			metrics.DefaultRouteGateways.Set(0)
			metrics.HTTPRequestsTotal.WithLabelValues("test", "test", "test")
			metrics.HTTPRequestDurationSeconds.WithLabelValues("test")
			metrics.CheckCyclesTotal.Add(0)
			metrics.CheckCycleDurationSeconds.Observe(0)
			metrics.ApplicationUptimeSeconds.Set(0)
			metrics.ErrorsTotal.WithLabelValues("test")
			metrics.ConsecutiveFailures.WithLabelValues("test")
		}, "all metrics should be accessible and registered")
	})
}

// Updating the type of one of these is a breaking change
func TestMetrics_StructureValidation(t *testing.T) {
	metrics, err := New(prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Test specific metric types and configurations
	t.Run("counter metrics are properly configured", func(t *testing.T) {
		// Test CounterVec metrics
		require.IsType(t, &prometheus.CounterVec{}, metrics.HealthCheckTotal)
		require.IsType(t, &prometheus.CounterVec{}, metrics.RouteUpdatesTotal)
		require.IsType(t, &prometheus.CounterVec{}, metrics.HTTPRequestsTotal)
		require.IsType(t, &prometheus.CounterVec{}, metrics.ErrorsTotal)

		// Test Counter metrics (check that they implement the Counter interface)
		require.Implements(t, (*prometheus.Counter)(nil), metrics.CheckCyclesTotal)
	})

	t.Run("gauge metrics are properly configured", func(t *testing.T) {
		// Test Gauge metrics (check that they implement the Gauge interface)
		require.Implements(t, (*prometheus.Gauge)(nil), metrics.ActiveGatewayCount)
		require.Implements(t, (*prometheus.Gauge)(nil), metrics.TotalGatewayCount)
		require.Implements(t, (*prometheus.Gauge)(nil), metrics.DefaultRouteGateways)
		require.Implements(t, (*prometheus.Gauge)(nil), metrics.ApplicationUptimeSeconds)

		// Test GaugeVec metrics
		require.IsType(t, &prometheus.GaugeVec{}, metrics.ConsecutiveFailures)
	})

	t.Run("histogram metrics are properly configured", func(t *testing.T) {
		// Test HistogramVec metrics
		require.IsType(t, &prometheus.HistogramVec{}, metrics.HealthCheckDurationSeconds)
		require.IsType(t, &prometheus.HistogramVec{}, metrics.HTTPRequestDurationSeconds)

		// Test Histogram metrics (check that they implement the Histogram interface)
		require.Implements(t, (*prometheus.Histogram)(nil), metrics.RouteUpdateDurationSeconds)
		require.Implements(t, (*prometheus.Histogram)(nil), metrics.CheckCycleDurationSeconds)
	})
}

// Removing a label is a breaking change
func TestMetrics_FunctionalValidation(t *testing.T) {
	metrics, err := New(prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, metrics)

	t.Run("counter metrics can be incremented", func(t *testing.T) {
		// Test that counter metrics can be used without panicking
		require.NotPanics(t, func() {
			metrics.HealthCheckTotal.WithLabelValues("192.168.1.1", "success").Inc()
			metrics.RouteUpdatesTotal.WithLabelValues("add", "success").Inc()
			metrics.HTTPRequestsTotal.WithLabelValues("192.168.1.1", "200", "GET").Inc()
			metrics.ErrorsTotal.WithLabelValues("network").Inc()
			metrics.CheckCyclesTotal.Inc()
		})
	})

	t.Run("gauge metrics can be set", func(t *testing.T) {
		// Test that gauge metrics can be used without panicking
		require.NotPanics(t, func() {
			metrics.ActiveGatewayCount.Set(5)
			metrics.TotalGatewayCount.Set(10)
			metrics.DefaultRouteGateways.Set(3)
			metrics.ApplicationUptimeSeconds.Set(3600)
			metrics.ConsecutiveFailures.WithLabelValues("192.168.1.1").Set(2)
		})
	})

	t.Run("histogram metrics can observe values", func(t *testing.T) {
		// Test that histogram metrics can be used without panicking
		require.NotPanics(t, func() {
			metrics.HealthCheckDurationSeconds.WithLabelValues("192.168.1.1").Observe(0.1)
			metrics.HTTPRequestDurationSeconds.WithLabelValues("192.168.1.1").Observe(0.05)
			metrics.RouteUpdateDurationSeconds.Observe(0.2)
			metrics.CheckCycleDurationSeconds.Observe(1.5)
		})
	})
}

func TestStartMetricsServer(t *testing.T) {
	t.Run("successful server start and stop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Use port 0 to get a random available port
		err := StartMetricsServer(ctx, cancel, 0)
		require.NoError(t, err)

		// Give the server a moment to start
		time.Sleep(100 * time.Millisecond)

		// Cancel context to trigger shutdown
		cancel()

		// Give the server a moment to shutdown
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("server serves metrics endpoint", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start server on a specific port for testing
		port := findAvailablePort(t)
		err := StartMetricsServer(ctx, cancel, port)
		require.NoError(t, err)

		// Give the server a moment to start
		time.Sleep(100 * time.Millisecond)

		// Test that metrics endpoint is accessible
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode, "metrics endpoint should return 200 OK")
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain", "should return text/plain content type")

		// Read and verify response contains prometheus metrics
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		bodyStr := string(body)
		assert.Contains(t, bodyStr, "# HELP", "response should contain Prometheus help comments")
		assert.Contains(t, bodyStr, "# TYPE", "response should contain Prometheus type comments")

		cancel()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("server handles port binding failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Try to bind to an invalid port (negative port should fail)
		err := StartMetricsServer(ctx, cancel, -1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to bind")
	})

	t.Run("server handles context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		port := findAvailablePort(t)
		err := StartMetricsServer(ctx, cancel, port)
		require.NoError(t, err)

		// Give the server a moment to start
		time.Sleep(50 * time.Millisecond)

		// Verify server is running
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Wait for context timeout (server should shutdown gracefully)
		time.Sleep(300 * time.Millisecond)

		// Verify server is no longer responding
		_, err = http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
		assert.Error(t, err, "server should be shut down and not responding")
	})
}

// findAvailablePort finds an available port for testing
func findAvailablePort(t *testing.T) int {
	// Use a simple approach: start with a base port and increment
	// This isn't perfect but works well enough for tests
	basePort := 18080 // Use a higher port range to avoid conflicts
	for i := range 100 {
		port := basePort + i
		if isPortAvailable(port) {
			return port
		}
	}
	t.Fatal("could not find an available port for testing")
	return 0
}

// isPortAvailable checks if a port is available for binding
func isPortAvailable(port int) bool {
	// Try to make a connection to see if port is in use
	client := &http.Client{Timeout: 50 * time.Millisecond}
	_, err := client.Get(fmt.Sprintf("http://localhost:%d", port))
	// If we get a connection refused error, the port is available
	return err != nil && strings.Contains(err.Error(), "connection refused")
}

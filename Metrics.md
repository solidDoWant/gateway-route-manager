# Gateway Route Manager Metrics

This document describes the Prometheus metrics exposed by the Gateway Route Manager.

## Metrics Endpoint

The metrics are exposed on the `/metrics` endpoint on the port specified by the `--metrics-port` flag (default: 9090).

Example: `http://localhost:9090/metrics`

## Metric Categories

### Gateway Health Metrics

These metrics track the health status and performance of gateway checks.

#### `gateway_health_check_total`
- **Type**: Counter
- **Description**: Total number of health checks performed
- **Labels**:
  - `gateway_ip`: IP address of the gateway
  - `status`: Result of the check (`success` or `failure`)

#### `gateway_health_check_duration_seconds`
- **Type**: Histogram
- **Description**: Duration of health checks in seconds
- **Labels**:
  - `gateway_ip`: IP address of the gateway

#### `gateway_active_count`
- **Type**: Gauge
- **Description**: Current number of active/healthy gateways

#### `gateway_total_count`
- **Type**: Gauge
- **Description**: Total number of configured gateways

### Route Management Metrics

These metrics track routing table operations and their success/failure rates.

#### `route_updates_total`
- **Type**: Counter
- **Description**: Total number of route update attempts
- **Labels**:
  - `operation`: Type of operation (`add`, `replace`, `remove`, `update`)
  - `status`: Result of the operation (`success` or `failure`)

#### `route_update_duration_seconds`
- **Type**: Histogram
- **Description**: Time taken to update routes in seconds

#### `default_route_gateways_count`
- **Type**: Gauge
- **Description**: Current number of gateways in the default route

### HTTP Client Metrics

These metrics track HTTP requests made to gateways for health checking.

#### `http_requests_total`
- **Type**: Counter
- **Description**: Total HTTP requests made to gateways
- **Labels**:
  - `gateway_ip`: IP address of the gateway
  - `status_code`: HTTP response status code
  - `method`: HTTP method (always `GET`)

#### `http_request_duration_seconds`
- **Type**: Histogram
- **Description**: HTTP request duration in seconds
- **Labels**:
  - `gateway_ip`: IP address of the gateway

### Application Metrics

These metrics track the overall application performance and lifecycle.

#### `check_cycles_total`
- **Type**: Counter
- **Description**: Total number of gateway check cycles completed

#### `check_cycle_duration_seconds`
- **Type**: Histogram
- **Description**: Duration of complete check cycles (all gateways) in seconds

#### `application_start_time_seconds`
- **Type**: Gauge
- **Description**: Unix timestamp when the application started

#### `configuration_info`
- **Type**: Gauge (Info metric)
- **Description**: Configuration details
- **Labels**:
  - `check_period`: Check period duration
  - `timeout`: Timeout duration
  - `port`: Target port for health checks
  - `scheme`: URL scheme (`http` or `https`)
  - `path`: URL path for health checks
- **Value**: Always `1`

### Error Metrics

These metrics track various types of errors encountered during operation.

#### `errors_total`
- **Type**: Counter
- **Description**: Total errors encountered
- **Labels**:
  - `type`: Error type (`network_error`, `timeout`, `invalid_response`, `route_error`)

#### `consecutive_failures_count`
- **Type**: Gauge
- **Description**: Current consecutive failures per gateway
- **Labels**:
  - `gateway_ip`: IP address of the gateway

## Example Queries

### PromQL Query Examples

```promql
# Current number of active gateways
gateway_active_count

# Health check success rate over the last 5 minutes
rate(gateway_health_check_total{status="success"}[5m]) /
rate(gateway_health_check_total[5m])

# Average health check duration per gateway
rate(gateway_health_check_duration_seconds_sum[5m]) /
rate(gateway_health_check_duration_seconds_count[5m])

# Route update failures in the last hour
increase(route_updates_total{status="failure"}[1h])

# Gateways with consecutive failures
consecutive_failures_count > 0

# HTTP error rate by status code
rate(http_requests_total{status_code!="200"}[5m])

# Check cycle frequency
rate(check_cycles_total[5m])
```

### Grafana Dashboard Suggestions

1. **Gateway Status Panel**: Show current active vs total gateways
2. **Health Check Success Rate**: Time series of success rates per gateway
3. **Response Time Distribution**: Histogram of health check durations
4. **Route Operations**: Counter of successful vs failed route operations
5. **Error Rate by Type**: Breakdown of errors by type
6. **Consecutive Failures Alert**: Table showing gateways with failures
7. **HTTP Status Code Distribution**: Pie chart of response codes

## Alerting Rules

### Example Prometheus Alerting Rules

```yaml
groups:
- name: gateway-route-manager
  rules:
  - alert: GatewayDown
    expr: gateway_active_count == 0
    for: 30s
    labels:
      severity: critical
    annotations:
      summary: "No gateways are active"
      description: "All configured gateways are down"

  - alert: GatewayHighFailureRate
    expr: rate(gateway_health_check_total{status="failure"}[5m]) / rate(gateway_health_check_total[5m]) > 0.5
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "High gateway failure rate"
      description: "Gateway {{ $labels.gateway_ip }} has >50% failure rate"

  - alert: RouteUpdateFailures
    expr: increase(route_updates_total{status="failure"}[10m]) > 0
    for: 0s
    labels:
      severity: warning
    annotations:
      summary: "Route update failures detected"
      description: "{{ $value }} route update failures in the last 10 minutes"

  - alert: ConsecutiveFailures
    expr: consecutive_failures_count > 5
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "Gateway consecutive failures"
      description: "Gateway {{ $labels.gateway_ip }} has {{ $value }} consecutive failures"
```

## Metric Collection

The metrics are compatible with standard Prometheus scraping. Add the following job to your Prometheus configuration:

```yaml
scrape_configs:
- job_name: 'gateway-route-manager'
  static_configs:
  - targets: ['localhost:8080']
  scrape_interval: 15s
  metrics_path: /metrics
```

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/iputil"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/metrics"
)

// Gateway represents a single gateway with its health status
type Gateway struct {
	IP                  net.IP
	URL                 string
	IsActive            bool
	ConsecutiveFailures int
	PublicIP            string // Public IP address obtained from public IP service
	metrics             *metrics.Metrics
}

// GenerateGateways creates a slice of Gateway structs for the IP range
func GenerateGateways(startIPStr, endIPStr string, port int, path, scheme string, m *metrics.Metrics) ([]Gateway, error) {
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
			IP:      ipCopy,
			URL:     url,
			metrics: m,
		})

		// Check if we've reached the end IP
		if currentIP.Equal(endIP) {
			break
		}

		// Increment IP
		if err := iputil.IncrementIP(currentIP); err != nil {
			return nil, fmt.Errorf("failed to increment IP: %w", err)
		}

		// Safety check to prevent infinite loop
		if iputil.IsIPGreater(currentIP, endIP) {
			break
		}
	}

	return gateways, nil
}

// FetchPublicIP fetches the public IP address from the gateway's public IP service
func (g *Gateway) FetchPublicIP(ctx context.Context, cfg config.PublicIPServiceConfig, timeout time.Duration) error {
	gatewayIP := g.IP.String()

	if !g.IsActive {
		g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
		return fmt.Errorf("gateway %s is not active", g.IP.String())
	}

	client := &http.Client{
		Timeout: timeout,
	}

	// Determine the host to use - either the configured hostname or the gateway IP
	if cfg.Hostname == "" {
		cfg.Hostname = g.IP.String()
	}
	host := net.JoinHostPort(cfg.Hostname, fmt.Sprintf("%d", cfg.Port))

	url := &url.URL{
		Scheme: cfg.Scheme,
		Host:   host,
		Path:   cfg.Path,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request for public IP: %w", err)
	}

	// Add HTTP basic auth if credentials are provided
	if cfg.Username != "" && cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	// Prefer a JSON response
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, reqErr := client.Do(req)
	g.metrics.PublicIPFetchDurationSeconds.WithLabelValues(gatewayIP).Observe(time.Since(start).Seconds())

	var body []byte
	if resp != nil {
		// Read the response body
		if body, err = io.ReadAll(resp.Body); err != nil {
			g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
			return fmt.Errorf("failed to read response body from gateway %s: %w", g.IP.String(), err)
		}
	}

	if reqErr != nil {
		g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
		return fmt.Errorf("failed to fetch public IP from gateway %s: %w, %s", g.IP.String(), reqErr, string(body))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
		return fmt.Errorf("public IP service server returned status %d when fetching public IP from gateway %s", resp.StatusCode, g.IP.String())
	}

	var publicIP string

	// First try to parse as JSON
	var jsonResp map[string]any
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		// Check for common IP address ipAddressKeys in order of preference
		ipAddressKeys := []string{"public_ip", "ip_address", "ip_addr", "ip"}
		for _, ipAddressKey := range ipAddressKeys {
			if value, exists := jsonResp[ipAddressKey]; exists {
				if ipStr, ok := value.(string); ok && ipStr != "" {
					publicIP = ipStr
					break
				}
			}
		}

		if publicIP == "" {
			g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
			return fmt.Errorf("gateway returned a valid JSON response but no recognized IP address field: %s", string(body))
		}
	} else {
		publicIP = string(body)
	}

	publicIP = strings.TrimSpace(publicIP)

	// Validate that the public IP is a valid IP address
	parsedIP := net.ParseIP(publicIP)
	if parsedIP == nil {
		g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
		return fmt.Errorf("received invalid public IP '%s' from gateway %s", publicIP, g.IP.String())
	}

	if parsedIP.To4() == nil {
		g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "failure").Inc()
		return fmt.Errorf("received non-IPv4 public IP '%s' from gateway %s", publicIP, g.IP.String())
	}

	// Success case - record successful metric
	g.metrics.PublicIPFetchTotal.WithLabelValues(gatewayIP, "success").Inc()

	g.PublicIP = publicIP
	return nil
}

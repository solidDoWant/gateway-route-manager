package ddns

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var changeIPBaseURL = "https://nic.changeip.com/nic/update"

// ChangeIPProvider implements the DDNS Provider interface for ChangeIP.com
type ChangeIPProvider struct {
	username string
	password string
	hostname string
	client   *http.Client
}

// NewChangeIPProvider creates a new ChangeIP DDNS provider
func NewChangeIPProvider(username, password, hostname string, timeout time.Duration) *ChangeIPProvider {
	return &ChangeIPProvider{
		username: username,
		password: password,
		hostname: hostname,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the provider name
func (c *ChangeIPProvider) Name() string {
	return "ChangeIP"
}

// UpdateRecords updates the DNS records with the provided IP addresses
func (c *ChangeIPProvider) UpdateRecords(ctx context.Context, ips []string) error {
	logger := slog.With("provider", c.Name(), "hostname", c.hostname)

	if len(ips) == 0 {
		// The provider does not support an update with `ip=<nil>`. In the event that this happens
		// (and that somehow the manager process can still even talk to the DDNS provider),
		// set the hostname to an invalid IP address to make the error somewhat more clear to clients.
		ips = []string{"0.0.0.0"}
		logger.WarnContext(ctx, "No IPs provided for DDNS update; setting to 0.0.0.0")
	}

	logger = logger.With("ips", ips)

	// Build the URL with parameters
	updateURL, err := url.Parse(changeIPBaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse ChangeIP base URL (bug): %w", err)
	}

	updateURL.RawQuery = url.Values{
		// This must be comman joined instead of repeating the parameter
		"myip":     {strings.Join(ips, ",")},
		"hostname": {c.hostname},
	}.Encode()

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "GET", updateURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create DDNS update request: %w", err)
	}

	// Add basic auth header
	auth := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))
	req.Header.Add("Authorization", "Basic "+auth)

	// Set User-Agent
	req.Header.Set("User-Agent", "curl/7.54.1")

	logger.DebugContext(ctx, "Sending DDNS update request")

	// Make the request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send DDNS update request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DDNS response: %w", err)
	}

	responseBody := strings.TrimSpace(string(body))

	// Check if the request was successful
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DDNS update failed with status %d: %s", resp.StatusCode, responseBody)
	}

	logger.InfoContext(ctx, "DDNS update successful", "response", responseBody)
	return nil
}

package ddns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

var dynuDNSBaseURL = "https://api.dynu.com/v2"

// DynuDNSProvider implements the DDNS Provider interface for DynuDNS
type DynuDNSProvider struct {
	apiKey    string
	hostname  string
	recordTTL time.Duration
	client    *http.Client

	// Cached domain information
	initialized  atomic.Bool
	rootDomainID int
	nodeName     string
}

// DynuDNSAPIResponse represents the common structure for API responses
type DynuDNSExceptionAPIResponse struct {
	Exception *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"exception"`
}

// DynuDNSHostnameResponse represents the response from /dns/getroot/{hostname}
type DynuDNSHostnameResponse struct {
	ID   int    `json:"id"`
	Node string `json:"node"`
}

// DynuDNSRecord represents a DNS record from the DynuDNS API
type DynuDNSRecord struct {
	ID          int    `json:"id"`
	NodeName    string `json:"nodeName"`
	RecordType  string `json:"recordType"`
	IPv4Address string `json:"ipv4Address,omitempty"`
}

// DynuDNSRecordsResponse represents the response from /dns/{id}/record
type DynuDNSRecordsResponse struct {
	StatusCode int             `json:"statusCode"`
	DNSRecords []DynuDNSRecord `json:"dnsRecords"`
}

// DynuDNSRecordRequest represents a request to create/update a DNS record
type DynuDNSRecordRequest struct {
	NodeName    string `json:"nodeName"`
	RecordType  string `json:"recordType"`
	TTL         int    `json:"ttl"`
	State       bool   `json:"state"`
	IPv4Address string `json:"ipv4Address,omitempty"`
}

// makeAPIRequest is a helper function that makes HTTP requests to the DynuDNS API
// It handles common tasks like setting headers, reading response, and checking for API errors
func (d *DynuDNSProvider) makeAPIRequest(ctx context.Context, method, url string, body io.Reader, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set common headers
	req.Header.Set("API-Key", d.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "curl/7.54.1")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check if status code indicates an error
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Unmarshal into an exception result
		var exceptionResponse DynuDNSExceptionAPIResponse
		if err := json.Unmarshal(bodyBytes, &exceptionResponse); err != nil {
			return fmt.Errorf("failed to parse response into error response type: %w", err)
		}

		// Check for API-level errors if result implements the exception interface
		if exceptionResponse.Exception != nil {
			return fmt.Errorf("DynuDNS API error: %s (%d)", exceptionResponse.Exception.Message, resp.StatusCode)
		}

		return fmt.Errorf("DynuDNS API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result == nil {
		return nil
	}

	if err := json.Unmarshal(bodyBytes, result); err != nil {
		return fmt.Errorf("failed to parse response into provided result type: %w", err)
	}

	return nil
}

// NewDynuDNSProvider creates a new DynuDNS DDNS provider
func NewDynuDNSProvider(apiKey, hostname string, timeout time.Duration, recordTTL time.Duration) *DynuDNSProvider {
	return &DynuDNSProvider{
		apiKey:    apiKey,
		hostname:  hostname,
		recordTTL: recordTTL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the provider name
func (d *DynuDNSProvider) Name() string {
	return "DynuDNS"
}

// initializeDomainInfo queries the DynuDNS API to get root domain ID and node name
func (d *DynuDNSProvider) initializeDomainInfo(ctx context.Context) error {
	logger := slog.With("provider", d.Name(), "hostname", d.hostname)

	url := fmt.Sprintf("%s/dns/getroot/%s", dynuDNSBaseURL, d.hostname)

	logger.DebugContext(ctx, "Fetching domain info from DynuDNS")

	var response DynuDNSHostnameResponse
	if err := d.makeAPIRequest(ctx, "GET", url, nil, &response); err != nil {
		return err
	}

	d.rootDomainID = response.ID
	d.nodeName = response.Node

	logger.InfoContext(ctx, "Initialized DynuDNS domain info", "rootDomainID", d.rootDomainID, "nodeName", d.nodeName)
	return nil
}

// UpdateRecords updates the DNS records with the provided IP addresses
func (d *DynuDNSProvider) UpdateRecords(ctx context.Context, newPublicIPs []string) error {
	logger := slog.With("provider", d.Name(), "hostname", d.hostname, "ips", newPublicIPs)

	// This cannot be done at provider creation time because it requires network access, which may not be available then
	if !d.initialized.Load() {
		logger.Info("Initializing DynuDNS domain info")

		if err := d.initializeDomainInfo(ctx); err != nil {
			return fmt.Errorf("failed to initialize domain info: %w", err)
		}
		d.initialized.Store(true)
	}

	// Get current records
	existingRecords, err := d.getExistingRecords(ctx)
	if err != nil {
		return fmt.Errorf("failed to get existing records: %w", err)
	}

	// Build list of existing IP addresses from A records
	existingIPs := make(map[string]DynuDNSRecord)
	for _, record := range existingRecords {
		existingIPs[record.IPv4Address] = record
	}

	// Calculate records to delete (existing IPs not in target list)
	var recordsToDelete []DynuDNSRecord
	for ip, record := range existingIPs {
		if !slices.Contains(newPublicIPs, ip) {
			recordsToDelete = append(recordsToDelete, record)
		}
	}

	// Calculate IPs to add (target IPs not in existing list)
	var ipsToAdd []string
	for _, targetIP := range newPublicIPs {
		if _, exists := existingIPs[targetIP]; !exists {
			ipsToAdd = append(ipsToAdd, targetIP)
		}
	}

	logger.InfoContext(ctx, "Calculated DNS record changes", "recordsToDelete", len(recordsToDelete), "ipsToAdd", len(ipsToAdd))

	// Execute deletions and additions in parallel using errgroup
	eg, gctx := errgroup.WithContext(ctx)

	// Delete unwanted records
	for _, record := range recordsToDelete {
		eg.Go(func() error {
			if err := d.deleteRecord(gctx, d.rootDomainID, record.ID); err != nil {
				return fmt.Errorf("failed to delete record %d (IP: %s): %w", record.ID, record.IPv4Address, err)
			}

			logger.DebugContext(gctx, "Deleted DNS record", "recordID", record.ID, "ip", record.IPv4Address)
			return nil
		})
	}

	// Add new records
	for _, ip := range ipsToAdd {
		eg.Go(func() error {
			if err := d.createRecord(gctx, ip); err != nil {
				return fmt.Errorf("failed to create record for IP %s: %w", ip, err)
			}

			logger.DebugContext(gctx, "Created DNS record", "ip", ip)
			return nil
		})
	}

	// Wait for all operations to complete
	if err := eg.Wait(); err != nil {
		return fmt.Errorf("DNS record update failed: %w", err)
	}

	logger.InfoContext(ctx, "Successfully updated DNS records")
	return nil
}

// getExistingRecords retrieves existing DNS records for the domain and node
func (d *DynuDNSProvider) getExistingRecords(ctx context.Context) ([]DynuDNSRecord, error) {
	url := fmt.Sprintf("%s/dns/%d/record", dynuDNSBaseURL, d.rootDomainID)

	var response DynuDNSRecordsResponse
	if err := d.makeAPIRequest(ctx, "GET", url, nil, &response); err != nil {
		return nil, err
	}
	records := response.DNSRecords

	// Filter records by node name, type
	var filteredRecords []DynuDNSRecord
	for _, record := range records {
		if record.NodeName != d.nodeName {
			continue
		}

		if record.RecordType != "A" {
			continue
		}

		filteredRecords = append(filteredRecords, record)
	}

	return filteredRecords, nil
}

// createRecord creates a new DNS A record
func (d *DynuDNSProvider) createRecord(ctx context.Context, ipAddress string) error {
	url := fmt.Sprintf("%s/dns/%d/record", dynuDNSBaseURL, d.rootDomainID)

	recordReq := DynuDNSRecordRequest{
		NodeName:    d.nodeName,
		RecordType:  "A",
		TTL:         int(d.recordTTL.Seconds()),
		State:       true,
		IPv4Address: ipAddress,
	}
	slog.DebugContext(ctx, "Creating new DNS record", "provider", d.Name(), "request", fmt.Sprintf("%#v", recordReq))

	jsonData, err := json.Marshal(recordReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	return d.makeAPIRequest(ctx, "POST", url, strings.NewReader(string(jsonData)), nil)
}

// deleteRecord deletes a DNS record by ID
func (d *DynuDNSProvider) deleteRecord(ctx context.Context, rootDomainID, recordID int) error {
	url := fmt.Sprintf("%s/dns/%d/record/%d", dynuDNSBaseURL, rootDomainID, recordID)

	return d.makeAPIRequest(ctx, "DELETE", url, nil, nil)
}

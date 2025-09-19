package ddns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDynuDNSProvider_UpdateRecords tests the core functionality of updating DNS records
func TestDynuDNSProvider_UpdateRecords(t *testing.T) {
	// Track API calls to verify correct behavior
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, fmt.Sprintf("%s %s", r.Method, r.URL.Path))

		switch {
		case r.URL.Path == "/v2/dns/getroot/test.example.com":
			response := DynuDNSHostnameResponse{ID: 12345, Node: "test"}
			json.NewEncoder(w).Encode(response)

		case r.URL.Path == "/v2/dns/12345/record":
			if r.Method == "GET" {
				// Return existing records
				response := DynuDNSRecordsResponse{
					StatusCode: 200,
					DNSRecords: []DynuDNSRecord{
						{
							ID:          1,
							RecordType:  "A",
							NodeName:    "test",
							IPv4Address: "1.2.3.4",
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			} else if r.Method == "POST" {
				// Create record
				w.WriteHeader(http.StatusOK)
			}

		case strings.Contains(r.URL.Path, "/v2/dns/12345/record/"):
			if r.Method == "DELETE" {
				w.WriteHeader(http.StatusOK)
			}

		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	originalURL := dynuDNSBaseURL
	dynuDNSBaseURL = server.URL + "/v2"
	defer func() { dynuDNSBaseURL = originalURL }()

	provider, err := NewDynuDNSProvider("test-api-key", "test.example.com", 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	err = provider.UpdateRecords(t.Context(), []string{"5.6.7.8"})
	if err != nil {
		t.Fatalf("UpdateRecords failed: %v", err)
	}

	// Verify basic API calls were made
	if len(requests) < 3 {
		t.Errorf("Expected at least 3 API calls (getroot, get records, create/delete), got %d: %v", len(requests), requests)
	}
}

// TestDynuDNSProvider_UpdateRecords_EmptyList tests removing all records
func TestDynuDNSProvider_UpdateRecords_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/dns/getroot/test.example.com":
			response := DynuDNSHostnameResponse{ID: 12345, Node: "test"}
			json.NewEncoder(w).Encode(response)

		case r.URL.Path == "/v2/dns/12345/record":
			if r.Method == "GET" {
				response := DynuDNSRecordsResponse{
					StatusCode: 200,
					DNSRecords: []DynuDNSRecord{
						{
							ID:          1,
							RecordType:  "A",
							NodeName:    "test",
							IPv4Address: "1.2.3.4",
						},
					},
				}
				json.NewEncoder(w).Encode(response)
			}

		case strings.Contains(r.URL.Path, "/v2/dns/12345/record/"):
			if r.Method == "DELETE" {
				w.WriteHeader(http.StatusOK)
			}
		}
	}))
	defer server.Close()

	originalURL := dynuDNSBaseURL
	dynuDNSBaseURL = server.URL + "/v2"
	defer func() { dynuDNSBaseURL = originalURL }()

	provider, err := NewDynuDNSProvider("test-api-key", "test.example.com", 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()
	err = provider.UpdateRecords(ctx, []string{})
	if err != nil {
		t.Fatalf("UpdateRecords with empty list failed: %v", err)
	}
}

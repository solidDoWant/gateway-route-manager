package ddns

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeIPProvider_UpdateRecords(t *testing.T) {
	tests := []struct {
		name         string
		ips          []string
		username     string
		password     string
		hostname     string
		serverStatus int
		errFunc      require.ErrorAssertionFunc
	}{
		{
			name:     "successful update",
			ips:      []string{"1.2.3.4", "5.6.7.8"},
			username: "testuser",
			password: "testpass",
			hostname: "test.example.com",
		},
		{
			name:     "successful update - single IP",
			ips:      []string{"1.2.3.4"},
			username: "testuser",
			password: "testpass",
			hostname: "test.example.com",
		},
		{
			name:     "no IPs provided",
			ips:      []string{},
			username: "testuser",
			password: "testpass",
			hostname: "test.example.com",
		},
		{
			name:         "authentication error",
			ips:          []string{"1.2.3.4"},
			username:     "testuser",
			password:     "wrongpass",
			hostname:     "test.example.com",
			serverStatus: http.StatusUnauthorized,
			errFunc:      require.Error,
		},
		{
			name:         "server error status",
			ips:          []string{"1.2.3.4"},
			username:     "testuser",
			password:     "testpass",
			hostname:     "test.example.com",
			serverStatus: http.StatusInternalServerError,
			errFunc:      require.Error,
		},
		{
			// This can happen when the hostname is a root domain
			name:         "payment required",
			ips:          []string{"1.2.3.4"},
			username:     "testuser",
			password:     "testpass",
			hostname:     "test.example.com",
			serverStatus: http.StatusPaymentRequired,
			errFunc:      require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errFunc == nil {
				tt.errFunc = require.NoError
			}

			if tt.serverStatus == 0 {
				tt.serverStatus = http.StatusOK
			}

			var server *httptest.Server
			var receivedAuth string
			var receivedURL string

			// Setup a test HTTP server to mock ChangeIP responses
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Capture the authorization header
				receivedAuth = r.Header.Get("Authorization")
				receivedURL = r.URL.String()

				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			provider := &ChangeIPProvider{
				username: tt.username,
				password: tt.password,
				hostname: tt.hostname,
				client: &http.Client{
					Timeout: 5 * time.Second,
				},
			}

			// Override the base URL for testing
			originalURL := changeIPBaseURL
			changeIPBaseURL = server.URL + "/nic/update"
			defer func() { changeIPBaseURL = originalURL }()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			err := provider.UpdateRecords(ctx, tt.ips)

			tt.errFunc(t, err)

			// Verify the request details if we had a server
			// Check authorization header
			expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(tt.username+":"+tt.password))
			assert.Equal(t, expectedAuth, receivedAuth)

			// Check URL parameters - account for URL encoding
			expectedIPList := strings.Join(tt.ips, ",")
			if len(tt.ips) == 0 {
				expectedIPList = "0.0.0.0"
			}

			assert.Contains(t, receivedURL, fmt.Sprintf("myip=%s", url.QueryEscape(expectedIPList)))
			assert.Contains(t, receivedURL, fmt.Sprintf("hostname=%s", url.QueryEscape(tt.hostname)))
		})
	}
}

func TestChangeIPProvider_Name(t *testing.T) {
	provider := NewChangeIPProvider("user", "pass", "host", time.Second)
	assert.Equal(t, "ChangeIP", provider.Name())
}

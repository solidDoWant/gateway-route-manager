package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_FetchPublicIP(t *testing.T) {
	tests := []struct {
		name           string
		gateway        Gateway
		serverResponse string
		serverStatus   int
		expectedIP     string
		errFunc        require.ErrorAssertionFunc
	}{
		{
			name: "successful fetch",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"public_ip": "203.0.113.45"}`,
			expectedIP:     "203.0.113.45",
		},
		{
			name: "successful fetch - plain text IP",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: "198.51.100.42",
			expectedIP:     "198.51.100.42",
		},
		{
			name: "successful fetch - plain text IP with whitespace",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: "  192.0.2.100  \n",
			expectedIP:     "192.0.2.100",
		},
		{
			name: "successful fetch - JSON with ip_address key",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"ip_address": "203.0.113.50"}`,
			expectedIP:     "203.0.113.50",
		},
		{
			name: "successful fetch - JSON with ip_addr key",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"ip_addr": "198.51.100.75"}`,
			expectedIP:     "198.51.100.75",
		},
		{
			name: "successful fetch - JSON with ip key",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"ip": "192.0.2.200"}`,
			expectedIP:     "192.0.2.200",
		},
		{
			name: "successful fetch - JSON with multiple keys, prefers public_ip",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"public_ip": "203.0.113.10", "ip": "192.0.2.20"}`,
			expectedIP:     "203.0.113.10",
		},
		{
			name: "successful fetch - JSON with non-string value, falls to next key",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"public_ip": 12345, "ip_address": "198.51.100.99"}`,
			expectedIP:     "198.51.100.99",
		},
		{
			name: "gateway not active",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: false,
			},
			errFunc: require.Error,
		},
		{
			name: "server error",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverStatus: http.StatusInternalServerError,
			errFunc:      require.Error,
		},
		{
			name: "invalid JSON response",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"invalid": json}`,
			errFunc:        require.Error,
		},
		{
			name: "empty public IP",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"public_ip": ""}`,
			errFunc:        require.Error,
		},
		{
			name: "empty response",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: "",
			errFunc:        require.Error,
		},
		{
			name: "invalid IP address",
			gateway: Gateway{
				IP:       parseIP("192.168.1.1"),
				IsActive: true,
			},
			serverResponse: `{"public_ip": "not.an.ip"}`,
			errFunc:        require.Error,
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

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			port := 1234
			if tt.gateway.IsActive {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Verify the request path
					assert.Equal(t, "/", r.URL.Path)

					// Verify JSON Accept header is set
					assert.Equal(t, "application/json", r.Header.Get("Accept"))

					w.WriteHeader(tt.serverStatus)
					if tt.serverResponse != "" {
						w.Write([]byte(tt.serverResponse))
					}
				}))
				defer server.Close()

				// Extract host and port from server URL for the test
				host, portStr, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
				require.NoError(t, err)

				port, err = strconv.Atoi(portStr)
				require.NoError(t, err)

				// Set the gateway IP to the test server host for testing
				tt.gateway.IP = net.ParseIP(host)
			}
			cfg := config.PublicIPServiceConfig{
				Port:     port,
				Hostname: "",
				Scheme:   "http",
				Path:     "/",
				Username: "",
				Password: "",
			}
			err := tt.gateway.FetchPublicIP(ctx, cfg, time.Second)

			tt.errFunc(t, err)
			assert.Equal(t, tt.expectedIP, tt.gateway.PublicIP)
		})
	}
}

func TestGateway_FetchPublicIP_Timeout(t *testing.T) {
	// Create a server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Sleep longer than timeout
	}))
	defer server.Close()

	gateway := Gateway{
		IP:       parseIP("192.168.1.1"),
		IsActive: true,
	}

	ctx := context.Background()
	timeout := 100 * time.Millisecond

	// Extract port from server URL
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	cfg := config.PublicIPServiceConfig{
		Port:     port,
		Hostname: "",
		Scheme:   "http",
		Path:     "/",
		Username: "",
		Password: "",
	}
	err = gateway.FetchPublicIP(ctx, cfg, timeout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch public IP from gateway")
}

func TestGateway_FetchPublicIP_WithBasicAuth(t *testing.T) {
	gateway := Gateway{
		IP:       parseIP("127.0.0.1"),
		IsActive: true,
	}

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify basic auth is present
		username, password, ok := r.BasicAuth()
		assert.True(t, ok, "Basic auth should be present")
		assert.Equal(t, "testuser", username)
		assert.Equal(t, "testpass", password)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"public_ip": "1.2.3.4"}`))
	}))
	defer server.Close()

	// Extract port from server URL
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	// Set the gateway IP to the test server host for testing
	gateway.IP = net.ParseIP("127.0.0.1")

	cfg := config.PublicIPServiceConfig{
		Port:     port,
		Hostname: "",
		Scheme:   "http",
		Path:     "/",
		Username: "testuser",
		Password: "testpass",
	}
	err = gateway.FetchPublicIP(ctx, cfg, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3.4", gateway.PublicIP)
}

func TestGateway_FetchPublicIP_WithCustomHostname(t *testing.T) {
	gateway := Gateway{
		IP:       parseIP("192.168.1.1"), // This should be ignored when hostname is provided
		IsActive: true,
	}

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"public_ip": "5.6.7.8"}`))
	}))
	defer server.Close()

	// Extract port from server URL
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	// Use localhost as the hostname (this should override gateway IP)
	cfg := config.PublicIPServiceConfig{
		Port:     port,
		Hostname: "localhost",
		Scheme:   "http",
		Path:     "/",
		Username: "",
		Password: "",
	}
	err = gateway.FetchPublicIP(ctx, cfg, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "5.6.7.8", gateway.PublicIP)
}

func TestGateway_FetchPublicIP_WithCustomPath(t *testing.T) {
	gateway := Gateway{
		IP:       parseIP("127.0.0.1"),
		IsActive: true,
	}

	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom path is used
		assert.Equal(t, "/api/public-ip", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ip": "9.10.11.12"}`))
	}))
	defer server.Close()

	// Extract port from server URL
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	// Set the gateway IP to the test server host for testing
	gateway.IP = net.ParseIP("127.0.0.1")

	cfg := config.PublicIPServiceConfig{
		Port:     port,
		Hostname: "",
		Scheme:   "http",
		Path:     "/api/public-ip",
		Username: "",
		Password: "",
	}
	err = gateway.FetchPublicIP(ctx, cfg, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "9.10.11.12", gateway.PublicIP)
}

// Helper function to parse IP for tests
func parseIP(s string) net.IP {
	return net.ParseIP(s)
}

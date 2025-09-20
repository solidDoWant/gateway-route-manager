package config

import (
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		errFunc require.ErrorAssertionFunc
		errMsg  string
	}{
		{
			name: "valid config with single IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "valid config with IP range",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/health",
				Scheme:      "https",
				LogLevel:    "debug",
				MetricsPort: 8080,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "valid config with timeout equal to check period",
			config: Config{
				StartIP:     "10.0.0.1",
				EndIP:       "10.0.0.5",
				Timeout:     5 * time.Second,
				CheckPeriod: 5 * time.Second,
				Port:        443,
				URLPath:     "/api/health",
				Scheme:      "https",
				LogLevel:    "warn",
				MetricsPort: 3000,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "missing start IP",
			config: Config{
				StartIP:     "",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "missing end IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "missing both IPs",
			config: Config{
				StartIP:     "",
				EndIP:       "",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "invalid start IP",
			config: Config{
				StartIP:     "invalid-ip",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "invalid start-ip: invalid-ip",
		},
		{
			name: "invalid end IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "not-an-ip",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "invalid end-ip: not-an-ip",
		},
		{
			name: "start IP greater than end IP",
			config: Config{
				StartIP:     "192.168.1.10",
				EndIP:       "192.168.1.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "start-ip (192.168.1.10) must be less than or equal to end-ip (192.168.1.1)",
		},
		{
			name: "check period less than timeout",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     5 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "check-period (3s) must be at least as long as timeout (5s)",
		},
		{
			name: "invalid scheme - empty",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "scheme must be 'http' or 'https'",
		},
		{
			name: "invalid scheme - wrong value",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "ftp",
				MetricsPort: 9090,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "scheme must be 'http' or 'https'",
		},
		{
			name: "metrics port too low",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 0,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "metrics port must be between 1 and 65535",
		},
		{
			name: "metrics port too high",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 65536,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "metrics port must be between 1 and 65535",
		},
		{
			name: "edge case - port 1 (minimum valid port)",
			config: Config{
				StartIP:     "127.0.0.1",
				EndIP:       "127.0.0.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 1,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "edge case - port 65535 (maximum valid port)",
			config: Config{
				StartIP:     "127.0.0.1",
				EndIP:       "127.0.0.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "error",
				MetricsPort: 65535,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "valid DDNS config with require IP address",
			config: Config{
				StartIP:              "192.168.1.1",
				EndIP:                "192.168.1.1",
				Timeout:              1 * time.Second,
				CheckPeriod:          3 * time.Second,
				Port:                 80,
				URLPath:              "/",
				Scheme:               "http",
				LogLevel:             "info",
				MetricsPort:          9090,
				DDNSProvider:         "changeip",
				DDNSUsername:         "user",
				DDNSPassword:         "pass",
				DDNSHostname:         "example.com",
				DDNSRequireIPAddress: "192.168.1.100",
				DDNSTimeout:          time.Minute,
				DDNSTTL:              time.Minute,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
		},
		{
			name: "invalid DDNS require IP address - not a valid IP",
			config: Config{
				StartIP:              "192.168.1.1",
				EndIP:                "192.168.1.1",
				Timeout:              1 * time.Second,
				CheckPeriod:          3 * time.Second,
				Port:                 80,
				URLPath:              "/",
				Scheme:               "http",
				LogLevel:             "info",
				MetricsPort:          9090,
				DDNSProvider:         "changeip",
				DDNSUsername:         "user",
				DDNSPassword:         "pass",
				DDNSHostname:         "example.com",
				DDNSRequireIPAddress: "not.an.ip",
				DDNSTimeout:          time.Minute,
				DDNSTTL:              time.Minute,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "invalid ddns-require-ip-address: not.an.ip",
		},
		{
			name: "invalid DDNS require IP address - IPv6 address",
			config: Config{
				StartIP:              "192.168.1.1",
				EndIP:                "192.168.1.1",
				Timeout:              1 * time.Second,
				CheckPeriod:          3 * time.Second,
				Port:                 80,
				URLPath:              "/",
				Scheme:               "http",
				LogLevel:             "info",
				MetricsPort:          9090,
				DDNSProvider:         "changeip",
				DDNSUsername:         "user",
				DDNSPassword:         "pass",
				DDNSHostname:         "example.com",
				DDNSRequireIPAddress: "2001:db8::1",
				DDNSTimeout:          time.Minute,
				DDNSTTL:              time.Minute,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
			},
			errFunc: require.Error,
			errMsg:  "ddns-require-ip-address must be an IPv4 address: 2001:db8::1",
		},
		{
			name: "valid DynuDNS config - API key only",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.100",
				CheckPeriod: time.Minute,
				Timeout:     30 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 8080,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
				DDNSProvider: "dynudns",
				DDNSPassword: "api-key-12345",
				DDNSHostname: "test.example.com",
				DDNSTimeout:  time.Minute,
				DDNSTTL:      time.Minute,
			},
			errFunc: require.NoError,
		},
		{
			name: "invalid DynuDNS config - missing API key",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.100",
				CheckPeriod: time.Minute,
				Timeout:     30 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 8080,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
				DDNSProvider: "dynudns",
				DDNSHostname: "test.example.com",
				DDNSTimeout:  time.Minute,
				DDNSTTL:      time.Minute,
			},
			errFunc: require.Error,
			errMsg:  "ddns-password is required when ddns-provider is dynudns",
		},
		{
			name: "invalid provider",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.100",
				CheckPeriod: time.Minute,
				Timeout:     30 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 8080,
				PublicIPService: PublicIPServiceConfig{
					Port: 443,
				},
				DDNSProvider: "invalid-provider",
				DDNSUsername: "user",
				DDNSPassword: "pass",
				DDNSHostname: "test.example.com",
				DDNSTimeout:  time.Minute,
				DDNSTTL:      time.Minute,
			},
			errFunc: require.Error,
			errMsg:  "ddns-provider must be one of: changeip, dynudns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errFunc == nil {
				tt.errFunc = require.NoError
			}

			err := tt.config.Validate()
			tt.errFunc(t, err)
		})
	}
}

func TestConfig_GetSlogLevel(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		expected slog.Level
	}{
		{
			name:     "debug level",
			logLevel: "debug",
			expected: slog.LevelDebug,
		},
		{
			name:     "info level",
			logLevel: "info",
			expected: slog.LevelInfo,
		},
		{
			name:     "warn level",
			logLevel: "warn",
			expected: slog.LevelWarn,
		},
		{
			name:     "error level",
			logLevel: "error",
			expected: slog.LevelError,
		},
		{
			name:     "case insensitive DEBUG",
			logLevel: "DEBUG",
			expected: slog.LevelDebug,
		},
		{
			name:     "case insensitive Info",
			logLevel: "Info",
			expected: slog.LevelInfo,
		},
		{
			name:     "invalid level defaults to info",
			logLevel: "invalid",
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{LogLevel: tt.logLevel}
			actual := config.GetSlogLevel()
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestExcludeReservedCIDRs(t *testing.T) {
	// Test that all reserved CIDRs are valid and parseable
	for _, cidrStr := range reservedCIDRs {
		t.Run(fmt.Sprintf("parse_%s", cidrStr), func(t *testing.T) {
			_, cidr, err := net.ParseCIDR(cidrStr)
			require.NoError(t, err, "Reserved CIDR %s should be valid", cidrStr)
			require.NotNil(t, cidr, "Parsed CIDR should not be nil")
		})
	}
}

func TestReservedCIDRsContent(t *testing.T) {
	// Test that the reserved CIDRs list contains expected entries
	expectedCIDRs := []string{
		"0.0.0.0/8",       // "This" Network
		"10.0.0.0/8",      // Private network
		"100.64.0.0/10",   // Carrier-grade NAT
		"127.0.0.0/8",     // Loopback
		"169.254.0.0/16",  // Link-local
		"172.16.0.0/12",   // Private network
		"192.0.0.0/24",    // IETF Protocol Assignments
		"192.0.2.0/24",    // TEST-NET-1
		"192.88.99.0/24",  // 6to4 Relay Anycast
		"192.168.0.0/16",  // Private network
		"198.18.0.0/15",   // Network benchmark tests
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/3",     // Multicast + MCAST-TEST-NET + Reserved for future use + Broadcast
	}

	assert.Equal(t, expectedCIDRs, reservedCIDRs, "Reserved CIDRs list should match expected values")
}

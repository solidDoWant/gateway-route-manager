package iputil

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsIPGreater(t *testing.T) {
	tests := []struct {
		name     string
		ip1      string
		ip2      string
		expected bool
	}{
		{
			name:     "ip1 greater than ip2",
			ip1:      "192.168.1.10",
			ip2:      "192.168.1.1",
			expected: true,
		},
		{
			name: "ip1 less than ip2",
			ip1:  "192.168.1.1",
			ip2:  "192.168.1.10",
		},
		{
			name: "ip1 equal to ip2",
			ip1:  "192.168.1.5",
			ip2:  "192.168.1.5",
		},
		{
			name:     "different subnets - ip1 greater",
			ip1:      "192.168.2.1",
			ip2:      "192.168.1.255",
			expected: true,
		},
		{
			name: "different subnets - ip1 less",
			ip1:  "192.168.1.255",
			ip2:  "192.168.2.1",
		},
		{
			name: "invalid IPv6 address should return false",
			ip1:  "2001:db8::1",
			ip2:  "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip1 := parseIP(t, tt.ip1)
			ip2 := parseIP(t, tt.ip2)

			require.Equal(t, tt.expected, IsIPGreater(ip1, ip2))
		})
	}
}

func TestIncrementIP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		errFunc  require.ErrorAssertionFunc
	}{
		{
			name:     "simple increment",
			input:    "192.168.1.1",
			expected: "192.168.1.2",
		},
		{
			name:     "increment with carry",
			input:    "192.168.1.255",
			expected: "192.168.2.0",
		},
		{
			name:     "increment with multiple carry",
			input:    "192.168.255.255",
			expected: "192.169.0.0",
		},
		{
			name:     "increment zero",
			input:    "0.0.0.0",
			expected: "0.0.0.1",
		},
		{
			name:    "IPv4 overflow should error",
			input:   "255.255.255.255",
			errFunc: require.Error,
		},
		{
			name:    "invalid IPv6 address should error",
			input:   "2001:db8::1",
			errFunc: require.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errFunc == nil {
				tt.errFunc = require.NoError
			}

			ip := parseIP(t, tt.input)
			err := IncrementIP(ip)

			tt.errFunc(t, err)
		})
	}
}

// Helper function to parse IP and fail test if invalid
func parseIP(t *testing.T, ip string) net.IP {
	t.Helper()

	parsed := net.ParseIP(ip)
	require.NotNil(t, parsed, "Failed to parse IP: %s", ip)

	return parsed
}

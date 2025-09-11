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

// Helper function to parse CIDR and fail test if invalid
func parseCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()

	_, network, err := net.ParseCIDR(cidr)
	require.NoError(t, err, "Failed to parse CIDR: %s", cidr)

	return network
}

// Helper function to convert CIDR strings to []*net.IPNet
func parseCIDRs(t *testing.T, cidrs []string) []*net.IPNet {
	t.Helper()

	networks := make([]*net.IPNet, len(cidrs))
	for i, cidr := range cidrs {
		networks[i] = parseCIDR(t, cidr)
	}
	return networks
}

// Helper function to convert []*net.IPNet to string slice for comparison
func networksToStrings(networks []*net.IPNet) []string {
	result := make([]string, len(networks))
	for i, network := range networks {
		result[i] = network.String()
	}
	return result
}

func TestReduceNetworks(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single network",
			input:    []string{"10.0.0.0/8"},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name:     "exact duplicates",
			input:    []string{"10.0.0.0/8", "10.0.0.0/8"},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name:     "multiple exact duplicates",
			input:    []string{"10.0.0.0/8", "192.168.1.0/24", "10.0.0.0/8", "192.168.1.0/24"},
			expected: []string{"10.0.0.0/8", "192.168.1.0/24"},
		},
		{
			name:     "subset removal - /24 in /8",
			input:    []string{"10.0.0.0/8", "10.0.10.0/24"},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name:     "subset removal - multiple subnets",
			input:    []string{"10.0.0.0/8", "10.0.10.0/24", "10.1.1.0/24"},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name:     "adjacent network merging - /9 networks",
			input:    []string{"10.0.0.0/9", "10.128.0.0/9"},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name:     "adjacent network merging - /25 networks",
			input:    []string{"192.168.1.0/25", "192.168.1.128/25"},
			expected: []string{"192.168.1.0/24"},
		},
		{
			name:     "adjacent network merging - /30 networks",
			input:    []string{"192.168.1.0/30", "192.168.1.4/30"},
			expected: []string{"192.168.1.0/29"},
		},
		{
			name:     "complex merging - four /26 networks",
			input:    []string{"192.168.1.0/26", "192.168.1.64/26", "192.168.1.128/26", "192.168.1.192/26"},
			expected: []string{"192.168.1.0/24"},
		},
		{
			name:     "non-adjacent networks - no merging",
			input:    []string{"10.0.0.0/8", "192.168.1.0/24"},
			expected: []string{"10.0.0.0/8", "192.168.1.0/24"},
		},
		{
			name:     "non-adjacent /24 networks",
			input:    []string{"192.168.1.0/24", "192.168.3.0/24"},
			expected: []string{"192.168.1.0/24", "192.168.3.0/24"},
		},
		{
			name:     "mixed operations - duplicates, subsets, and merging",
			input:    []string{"10.0.0.0/8", "10.0.0.0/8", "10.1.1.0/24", "192.168.1.0/25", "192.168.1.128/25"},
			expected: []string{"10.0.0.0/8", "192.168.1.0/24"},
		},
		{
			name:     "partial merge with subset",
			input:    []string{"192.168.0.0/25", "192.168.0.128/25", "192.168.0.0/16"},
			expected: []string{"192.168.0.0/16"},
		},
		{
			name:     "three networks - two merge, one standalone",
			input:    []string{"10.0.0.0/9", "10.128.0.0/9", "192.168.1.0/24"},
			expected: []string{"10.0.0.0/8", "192.168.1.0/24"},
		},
		{
			name:     "overlapping but not mergeable",
			input:    []string{"192.168.1.0/24", "192.168.1.128/25"},
			expected: []string{"192.168.1.0/24"},
		},
		{
			name:     "all /32 networks - no merging possible",
			input:    []string{"192.168.1.1/32", "192.168.1.2/32", "192.168.1.5/32"},
			expected: []string{"192.168.1.1/32", "192.168.1.2/32", "192.168.1.5/32"},
		},
		{
			name:     "adjacent /32 networks that can merge",
			input:    []string{"192.168.1.0/32", "192.168.1.1/32"},
			expected: []string{"192.168.1.0/31"},
		},
		{
			name:     "full /0 network",
			input:    []string{"0.0.0.0/0", "10.0.0.0/8"},
			expected: []string{"0.0.0.0/0"},
		},
		{
			name:     "multiple layers of subsets and merges",
			input:    []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24", "10.0.4.0/22"},
			expected: []string{"10.0.0.0/21"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := parseCIDRs(t, tt.input)
			result := ReduceNetworks(input)
			resultStrings := networksToStrings(result)

			require.ElementsMatch(t, tt.expected, resultStrings)
		})
	}
}

func TestReduceNetworks_NilInput(t *testing.T) {
	result := ReduceNetworks(nil)
	require.Nil(t, result)
}

func TestReduceNetworks_WithNilElements(t *testing.T) {
	input := []*net.IPNet{
		parseCIDR(t, "10.0.0.0/8"),
		nil,
		parseCIDR(t, "192.168.1.0/24"),
		nil,
	}

	result := ReduceNetworks(input)
	expected := []string{"10.0.0.0/8", "192.168.1.0/24"}
	resultStrings := networksToStrings(result)

	require.ElementsMatch(t, expected, resultStrings)
}

func TestIsSubnetOf(t *testing.T) {
	tests := []struct {
		name     string
		subnet   string
		network  string
		expected bool
	}{
		{
			name:     "exact match",
			subnet:   "10.0.0.0/8",
			network:  "10.0.0.0/8",
			expected: true,
		},
		{
			name:     "subnet is contained",
			subnet:   "10.1.1.0/24",
			network:  "10.0.0.0/8",
			expected: true,
		},
		{
			name:     "subnet is not contained",
			subnet:   "192.168.1.0/24",
			network:  "10.0.0.0/8",
			expected: false,
		},
		{
			name:     "network is contained in subnet",
			subnet:   "10.0.0.0/8",
			network:  "10.1.1.0/24",
			expected: false,
		},
		{
			name:     "overlapping but neither contains the other",
			subnet:   "192.168.1.0/24",
			network:  "192.168.1.128/25",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnet := parseCIDR(t, tt.subnet)
			network := parseCIDR(t, tt.network)

			result := isSubnetOf(subnet, network)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTryMergeNetworks(t *testing.T) {
	tests := []struct {
		name     string
		net1     string
		net2     string
		expected string // empty string means nil result
	}{
		{
			name:     "adjacent /9 networks",
			net1:     "10.0.0.0/9",
			net2:     "10.128.0.0/9",
			expected: "10.0.0.0/8",
		},
		{
			name:     "adjacent /25 networks",
			net1:     "192.168.1.0/25",
			net2:     "192.168.1.128/25",
			expected: "192.168.1.0/24",
		},
		{
			name:     "adjacent /32 networks",
			net1:     "192.168.1.0/32",
			net2:     "192.168.1.1/32",
			expected: "192.168.1.0/31",
		},
		{
			name:     "non-adjacent networks",
			net1:     "192.168.1.0/24",
			net2:     "192.168.3.0/24",
			expected: "",
		},
		{
			name:     "different prefix lengths",
			net1:     "10.0.0.0/8",
			net2:     "192.168.1.0/24",
			expected: "",
		},
		{
			name:     "same network",
			net1:     "10.0.0.0/8",
			net2:     "10.0.0.0/8",
			expected: "",
		},
		{
			name:     "overlapping but not adjacent",
			net1:     "192.168.1.0/24",
			net2:     "192.168.1.64/26",
			expected: "",
		},
		{
			name:     "two /0 networks cannot merge",
			net1:     "0.0.0.0/0",
			net2:     "0.0.0.0/0",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net1 := parseCIDR(t, tt.net1)
			net2 := parseCIDR(t, tt.net2)

			result := tryMergeNetworks(net1, net2)

			if tt.expected == "" {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expected, result.String())
			}
		})
	}
}

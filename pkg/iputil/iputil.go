// Package iputil provides utility functions for working with IPv4 addresses
package iputil

import (
	"fmt"
	"net"
)

// IsIPGreater returns true if ip1 is greater than ip2.
// Both IPs must be valid IPv4 addresses.
func IsIPGreater(ip1, ip2 net.IP) bool {
	// Convert to IPv4 representation
	ip1 = ip1.To4()
	ip2 = ip2.To4()

	// Both must be valid IPv4 addresses
	if ip1 == nil || ip2 == nil {
		return false
	}

	for i := range ip1 {
		if ip1[i] == ip2[i] {
			continue
		}

		return ip1[i] > ip2[i]
	}

	return false // They are equal
}

// IncrementIP increments the given IPv4 address by 1.
// The function modifies the IP in place.
// Returns an error if the IP would overflow (e.g., 255.255.255.255 -> 0.0.0.0).
// For example, `192.168.1.1` becomes `192.168.1.2`.
func IncrementIP(ip net.IP) error {
	// Convert to IPv4 representation
	ip = ip.To4()
	if ip == nil {
		return fmt.Errorf("invalid IPv4 address")
	}

	// Check for maximum IPv4 address
	if ip.Equal(net.IPv4bcast) {
		return fmt.Errorf("IP address overflow: maximum IPv4 address reached")
	}

	// Increment from least significant byte
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}

	return nil
}

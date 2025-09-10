package gateway

import (
	"fmt"
	"net"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/iputil"
)

// Gateway represents a single gateway with its health status
type Gateway struct {
	IP                  net.IP
	URL                 string
	IsActive            bool
	ConsecutiveFailures int
}

// GenerateGateways creates a slice of Gateway structs for the IP range
func GenerateGateways(startIPStr, endIPStr string, port int, path, scheme string) ([]Gateway, error) {
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
			IP:  ipCopy,
			URL: url,
		})

		// Check if we've reached the end IP
		if currentIP.Equal(endIP) {
			break
		}

		// Increment IP
		if err := iputil.IncrementIP(currentIP); err != nil {
			return nil, fmt.Errorf("failed to increment IP: %v", err)
		}

		// Safety check to prevent infinite loop
		if iputil.IsIPGreater(currentIP, endIP) {
			break
		}
	}

	return gateways, nil
}

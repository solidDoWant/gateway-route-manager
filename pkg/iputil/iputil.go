// Package iputil provides utility functions for working with IPv4 addresses
package iputil

import (
	"errors"
	"fmt"
	"net"
	"sort"

	"github.com/vishvananda/netlink"
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

// ReduceNetworks reduces a slice of *net.IPNet networks into the smallest possible set.
// This function:
// 1. Removes duplicate networks
// 2. Removes networks that are subsets of other networks
// 3. Merges adjacent networks that can form a single larger network
//
// Examples:
// - [10.0.0.0/8, 10.0.0.0/8] -> [10.0.0.0/8] (removes duplicates)
// - [10.0.0.0/8, 10.0.10.0/24] -> [10.0.0.0/8] (removes subset)
// - [10.0.0.0/9, 10.128.0.0/9] -> [10.0.0.0/8] (merges adjacent networks)
func ReduceNetworks(networks []*net.IPNet) []*net.IPNet {
	if len(networks) <= 1 {
		return networks
	}

	// Create a working copy to avoid modifying the input
	result := make([]*net.IPNet, 0, len(networks))
	for _, network := range networks {
		if network != nil {
			result = append(result, &net.IPNet{
				IP:   make(net.IP, len(network.IP)),
				Mask: make(net.IPMask, len(network.Mask)),
			})
			copy(result[len(result)-1].IP, network.IP)
			copy(result[len(result)-1].Mask, network.Mask)
		}
	}

	// Step 1: Sort networks by IP address and prefix length for consistent processing
	sortNetworks(result)

	// Step 2: Remove exact duplicates
	result = removeDuplicateNetworks(result)

	// Step 3: Remove networks that are subsets of other networks
	result = removeSubsetNetworks(result)

	// Step 4: Repeatedly try to merge adjacent networks until no more merges are possible
	for {
		merged := mergeAdjacentNetworks(result)
		if len(merged) == len(result) {
			break // No more merges possible
		}
		result = merged
		sortNetworks(result)
	}

	return result
}

// sortNetworks sorts networks by IP address first, then by prefix length (longer prefixes first)
func sortNetworks(networks []*net.IPNet) {
	sort.Slice(networks, func(i, j int) bool {
		// First compare by IP address
		iIP := networks[i].IP.To4()
		jIP := networks[j].IP.To4()

		if iIP == nil || jIP == nil {
			return false
		}

		for k := 0; k < 4; k++ {
			if iIP[k] != jIP[k] {
				return iIP[k] < jIP[k]
			}
		}

		// If IPs are equal, sort by prefix length (longer prefixes first)
		iOnes, _ := networks[i].Mask.Size()
		jOnes, _ := networks[j].Mask.Size()
		return iOnes > jOnes
	})
}

// removeDuplicateNetworks removes exact duplicate networks
func removeDuplicateNetworks(networks []*net.IPNet) []*net.IPNet {
	if len(networks) <= 1 {
		return networks
	}

	result := make([]*net.IPNet, 0, len(networks))
	seen := make(map[string]bool)

	for _, network := range networks {
		key := network.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, network)
		}
	}

	return result
}

// removeSubsetNetworks removes networks that are subsets of other networks
func removeSubsetNetworks(networks []*net.IPNet) []*net.IPNet {
	if len(networks) <= 1 {
		return networks
	}

	result := make([]*net.IPNet, 0, len(networks))

	for i, network := range networks {
		isSubset := false
		for j, other := range networks {
			if i != j && isSubnetOf(network, other) {
				isSubset = true
				break
			}
		}
		if !isSubset {
			result = append(result, network)
		}
	}

	return result
}

// isSubnetOf returns true if subnet is a subset of or equal to network
func isSubnetOf(subnet, network *net.IPNet) bool {
	subnetOnes, subnetBits := subnet.Mask.Size()
	networkOnes, networkBits := network.Mask.Size()

	// Different IP versions
	if subnetBits != networkBits {
		return false
	}

	// Subnet must have equal or more specific prefix
	if subnetOnes < networkOnes {
		return false
	}

	// Check if subnet's network address is contained in network
	return network.Contains(subnet.IP)
}

// mergeAdjacentNetworks attempts to merge networks that are adjacent and can form a single larger network
func mergeAdjacentNetworks(networks []*net.IPNet) []*net.IPNet {
	if len(networks) <= 1 {
		return networks
	}

	result := make([]*net.IPNet, 0, len(networks))
	used := make([]bool, len(networks))

	for i, network1 := range networks {
		if used[i] {
			continue
		}

		// Try to find a network that can be merged with network1
		merged := false
		for j := i + 1; j < len(networks); j++ {
			if used[j] {
				continue
			}

			network2 := networks[j]
			if mergedNetwork := tryMergeNetworks(network1, network2); mergedNetwork != nil {
				result = append(result, mergedNetwork)
				used[i] = true
				used[j] = true
				merged = true
				break
			}
		}

		if !merged {
			result = append(result, network1)
			used[i] = true
		}
	}

	return result
}

// tryMergeNetworks attempts to merge two networks into a single larger network
// Returns the merged network if possible, nil otherwise
func tryMergeNetworks(net1, net2 *net.IPNet) *net.IPNet {
	ones1, bits1 := net1.Mask.Size()
	ones2, bits2 := net2.Mask.Size()

	// Networks must have the same prefix length and IP version
	if ones1 != ones2 || bits1 != bits2 {
		return nil
	}

	// Must be IPv4 for simplicity
	if bits1 != 32 {
		return nil
	}

	// Calculate network addresses
	ip1 := net1.IP.To4()
	ip2 := net2.IP.To4()
	if ip1 == nil || ip2 == nil {
		return nil
	}

	// Apply masks to get network addresses
	mask := net1.Mask
	net1Addr := make(net.IP, 4)
	net2Addr := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		net1Addr[i] = ip1[i] & mask[i]
		net2Addr[i] = ip2[i] & mask[i]
	}

	// Check if they can be merged (they must be adjacent networks of the same size)
	if ones1 == 0 {
		return nil // Can't merge two /0 networks
	}

	// Calculate the parent network (one bit less specific)
	parentMask := net.CIDRMask(ones1-1, 32)
	parentNet1 := make(net.IP, 4)
	parentNet2 := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		parentNet1[i] = net1Addr[i] & parentMask[i]
		parentNet2[i] = net2Addr[i] & parentMask[i]
	}

	// They can be merged if they have the same parent and are the two halves
	if !parentNet1.Equal(parentNet2) {
		return nil
	}

	// Calculate the distinguishing bit position
	bytePos := (ones1 - 1) / 8
	bitPos := 7 - ((ones1 - 1) % 8)

	// Check if one network has the bit set and the other doesn't
	bit1 := (net1Addr[bytePos] >> bitPos) & 1
	bit2 := (net2Addr[bytePos] >> bitPos) & 1

	if bit1 == bit2 {
		return nil // Not adjacent
	}

	// They can be merged - return the parent network
	return &net.IPNet{
		IP:   parentNet1,
		Mask: parentMask,
	}
}

// HasInterfaceWithIP checks if any network interface has the specified IP address assigned.
// It uses the vishvananda/netlink library to query interface addresses.
// Returns true if any interface has the IP address, false otherwise.
// Returns an error if there's an issue querying the network interfaces.
func HasInterfaceWithIP(targetIP string) (bool, error) {
	// Parse the target IP to ensure it's valid
	target := net.ParseIP(targetIP)
	if target == nil {
		return false, fmt.Errorf("invalid IP address: %s", targetIP)
	}

	// Get all network interfaces
	links, err := netlink.LinkList()
	if err != nil {
		return false, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	// Check each interface for addresses
	addrListErrs := make([]error, 0, len(links))
	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			addrListErrs = append(addrListErrs, fmt.Errorf("failed to list addresses for interface %s: %w", link.Attrs().Name, err))
			continue
		}

		for _, addr := range addrs {
			if addr.IP.Equal(target) {
				return true, nil
			}
		}
	}

	return false, errors.Join(addrListErrs...)
}

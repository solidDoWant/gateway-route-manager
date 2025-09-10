package routes

import (
	"fmt"
	"log"
	"net"
	"sort"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

func init() {
	// Configure the netlink library to provide actually useful error messages
	nl.EnableErrorMessageReporting = true
}

// Manager defines the interface for route management operations
type Manager interface {
	// UpdateDefaultRoute updates the default route to use ECMP with the provided active gateways.
	// Only returns an error if a fatal error occurs during route manipulation.
	UpdateDefaultRoute(activeGateways []net.IP) error
}

// NetlinkManager is the netlink-based implementation of the Manager interface
type NetlinkManager struct {
	handle netlinkHandle
}

var _ Manager = (*NetlinkManager)(nil)

// NewNetlinkManager creates a new netlink route manager
func NewNetlinkManager() (*NetlinkManager, error) {
	handle, err := netlink.NewHandle()
	if err != nil {
		// This should never happen
		return nil, fmt.Errorf("failed to create netlink handle: %v", err)
	}

	return &NetlinkManager{
		handle: handle,
	}, nil
}

// UpdateDefaultRoute updates the default route to use ECMP with the provided active gateways.
// Only returns an error if a fatal error occurs during route manipulation.
func (m *NetlinkManager) UpdateDefaultRoute(activeGateways []net.IP) error {
	if len(activeGateways) == 0 {
		// Remove existing default route if no gateways are active
		if err := m.removeDefaultRoute(); err != nil {
			return fmt.Errorf("failed to remove default route: %v", err)
		}

		log.Println("No active gateways, default route removed")
		return nil
	}

	// Sort gateways for consistent ordering
	sort.Slice(activeGateways, func(i, j int) bool {
		return activeGateways[i].String() < activeGateways[j].String()
	})

	// Replace existing default route with new ECMP route
	return m.replaceDefaultRouteECMP(activeGateways)
}

func (m *NetlinkManager) removeDefaultRoute() error {
	routes, err := m.handle.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list routes: %v", err)
	}

	for _, route := range routes {
		if !isDefaultRoute(route) {
			continue
		}

		if err := m.handle.RouteDel(&route); err != nil {
			return fmt.Errorf("failed to delete default route via %s: %v", route.Gw, err)
		}

		log.Printf("Removed default route via %s", route.Gw)
	}

	return nil
}

func (m *NetlinkManager) replaceDefaultRouteECMP(gateways []net.IP) error {
	if len(gateways) == 0 {
		return nil
	}

	// Create multipath route for ECMP
	nexthops := make([]*netlink.NexthopInfo, 0, len(gateways))
	for _, gateway := range gateways {
		nexthops = append(nexthops, &netlink.NexthopInfo{
			Gw: gateway,
		})
	}

	route := &netlink.Route{
		Dst: &net.IPNet{
			IP:   net.IPv4zero,
			Mask: net.CIDRMask(0, 32),
		},
		MultiPath: nexthops,
	}

	// Try to replace existing route. This is an upsert operation, so if the route
	// does not exist, it will be created.
	if err := m.handle.RouteReplace(route); err != nil {
		return fmt.Errorf("failed to replace/add ECMP route: %v", err)
	}

	gatewayStrings := make([]string, 0, len(gateways))
	for _, gw := range gateways {
		gatewayStrings = append(gatewayStrings, gw.String())
	}
	log.Printf("Updated ECMP default route via gateways: %v", gatewayStrings)

	return nil
}

// isDefaultRoute returns true if the route is a default route (targeting 0.0.0.0/0)
func isDefaultRoute(route netlink.Route) bool {
	if route.Gw == nil {
		return false
	}

	if route.Dst == nil {
		return true
	}

	if !route.Dst.IP.Equal(net.IPv4zero) {
		return false
	}

	ones, bits := route.Dst.Mask.Size()
	return ones == 0 && bits == 32
}

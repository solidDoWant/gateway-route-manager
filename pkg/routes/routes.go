package routes

import (
	"fmt"
	"log"
	"net"
	"sort"

	"github.com/vishvananda/netlink"
)

// Manager handles route management operations
type Manager struct{}

// New creates a new route manager
func New() *Manager {
	return &Manager{}
}

// UpdateDefaultRoute updates the default route to use ECMP with the provided active gateways.
// Only returns an error if a fatal error occurs during route manipulation.
func (m *Manager) UpdateDefaultRoute(activeGateways []net.IP) error {
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

func (m *Manager) removeDefaultRoute() error {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list routes: %v", err)
	}

	for _, route := range routes {
		if !isDefaultRoute(route) {
			continue
		}

		if err := netlink.RouteDel(&route); err != nil {
			return fmt.Errorf("failed to delete default route via %s: %v", route.Gw, err)
		}

		log.Printf("Removed default route via %s", route.Gw)
	}

	return nil
}

func (m *Manager) replaceDefaultRouteECMP(gateways []net.IP) error {
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

	// Try to replace existing route first
	if err := netlink.RouteReplace(route); err != nil {
		// If replace fails, try to add (in case no route exists)
		if err := netlink.RouteAdd(route); err != nil {
			return fmt.Errorf("failed to replace/add ECMP route: %v", err)
		}
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

package routes

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/iputil"
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

// CloseableManager extends Manager with a Close method for cleanup
type CloseableManager interface {
	Manager
	Close() error
}

// NetlinkManager is the netlink-based implementation of the Manager interface
type NetlinkManager struct {
	handle netlinkHandle

	gatewayTableID     int
	fallthroughTableID int

	firstExcludeRulePreference     int
	fallthroughTableRulePreference int
	gatewayTableRulePreference     int

	excludeNets []*net.IPNet
}

var _ Manager = (*NetlinkManager)(nil)

// NewNetlinkManager creates a new netlink route manager
func NewNetlinkManager(netsToExclude []*net.IPNet, firstTableID, firstRulePreference int) (*NetlinkManager, error) {
	// handle, err := netlink.NewHandle()
	// if err != nil {
	// 	// This should never happen
	// 	return nil, fmt.Errorf("failed to create netlink handle: %w", err)
	// }

	// For some inexplicable reason, using netlink.NewHandle() causes the program to hang indefinitely when
	// trying to delete specific routes. This seems to only happen on default routes attached to a table.
	// Using an empty handle seems to work fine, and is what the netlink package uses internally when no
	// specific handle is provided.
	// TODO file a bug report with the netlink package about this issue
	handle := &netlink.Handle{}

	manager := &NetlinkManager{
		handle: handle,
	}

	if err := manager.excludeNetworks(netsToExclude, firstTableID, firstRulePreference); err != nil {
		handle.Close()
		return nil, fmt.Errorf("failed to exclude networks: %w", err)
	}

	return manager, nil
}

// Destinations are excluded by using IP rules to jump over the gateway routing table. This allows these destinations
// to be routed via the normal system routing tables, while all other traffic is routed via the gateways. This can be
// useful for things like avoiding routing the health check and metrics response traffic via the gateways themselves.
//
// Here is what the IP rule list should look like:
// 0:	from all lookup local
// startRule+0: from netToExclude1 jump fallthrough table rule
// ...
// startRule+M-1: from netToExcludeM jump to the fallthrough table rule at startRule+N
// startRule+N-1: from all lookup gatewayTableID
// startRule+N: from all lookup fallthroughTableID
// ...
// 32766: from all lookup main
// 32767: from all lookup default
//
// where M is the number of networks to exclude, and N is M+2. This makes the total number of required rules M + 2
// (for the jump to the fallthrough table and the jump to gateway table).
//
// The gateway table will contain a single rule: the ECMP default route via the active gateways. This means
// that this table will never return, as all packets that hit it will be routed via one of the gateways.
//
// The fallthrough table will contain no rules. Its only purpose is to allow a valid rule to exist after the gateway
// table rule, so that packets that do not match any of the excluded networks will continue to the rest of the
// system routing tables (main, default, etc).
//
// It is important that the rules are added in the following order to prevent disruption of existing traffic:
// 1. Add the jump to the fallthrough table rule
// 2. Add the network exclude rules, which jump to the fallthrough table jump rule (traffic has not been impacted yet)
// 3. Add the jump to the gateway table rule (this is where traffic starts being routed via the gateways)
//
// When removing the rules, they should be removed in reverse order to prevent disruption.

// excludeNetworks configures the manager to exclude the specified networks from route management.
func (m *NetlinkManager) excludeNetworks(netsToExclude []*net.IPNet, firstTableID, firstRulePreference int) error {
	maxTableID := 255 - 1 // first is reserved for the gateway table, second for the fallthrough table
	if firstTableID < 1 || firstTableID > maxTableID {
		return fmt.Errorf("invalid first table ID: %d (must be between 1 and %d)", firstTableID, maxTableID)
	}

	// Reduce netsToExclude to the smallest possible set, removing duplicates,
	// subsets, and merging adjacent networks
	netsToExclude = iputil.ReduceNetworks(netsToExclude)

	// Validate that there are enough rule priorities available
	requiredRuleCount := len(netsToExclude) + 2             // 2 for the table rules, rest for the exclude jumps
	maxFirstRulePreference := 32766 - requiredRuleCount + 1 // +1 because the firstRuleID is inclusive
	if firstRulePreference < 1 || firstRulePreference > maxFirstRulePreference {
		return fmt.Errorf("invalid first rule preference: %d (must be between 1 and %d)", firstRulePreference, maxFirstRulePreference)
	}

	// Update the manager state with the provided parameters
	m.excludeNets = netsToExclude

	m.gatewayTableID = firstTableID
	m.fallthroughTableID = firstTableID + 1

	m.firstExcludeRulePreference = firstRulePreference
	m.fallthroughTableRulePreference = firstRulePreference + len(netsToExclude) + 1 // Last rule is the fallthrough table rule
	m.gatewayTableRulePreference = m.fallthroughTableRulePreference - 1             // Second last rule is the gateway table rule

	// Add the rules to the system
	if err := m.addRules(); err != nil {
		// Cleanup on failure
		cleanupErr := m.removeRules()
		if cleanupErr != nil {
			slog.Error("Failed to clean up rules after add failure", "error", cleanupErr)
		}
		return fmt.Errorf("failed to add rules: %w", err)
	}

	slog.Info("Configured route manager", "gateway_table", m.gatewayTableID, "fallthrough_table", m.fallthroughTableID, "first_rule_preference", m.firstExcludeRulePreference, "excluded_networks", netsToExclude)
	return nil
}

func (m *NetlinkManager) addRules() error {
	// Delete any existing rules. Netlink rules do no support replacements, only additions and deletions.
	// This is important to handle in case the program is restarted.
	if err := m.removeRules(); err != nil {
		return fmt.Errorf("failed to remove existing rules: %w", err)
	}

	// First add the fallthrough table rule
	fallthroughRule := netlink.NewRule()
	fallthroughRule.Table = m.fallthroughTableID
	fallthroughRule.Priority = m.fallthroughTableRulePreference

	if err := m.handle.RuleAdd(fallthroughRule); err != nil {
		return fmt.Errorf("failed to add fallthrough table rule: %w", err)
	}
	slog.Debug("Added fallthrough table rule", "table", m.fallthroughTableID, "preference", fallthroughRule.Priority)

	// Then add the exclude rules
	for i, excludeNet := range m.excludeNets {
		excludeRule := netlink.NewRule()
		excludeRule.Dst = excludeNet
		excludeRule.Goto = m.fallthroughTableRulePreference // Jump to fallthrough table rule, skippping over the gateway table rule
		excludeRule.Priority = m.firstExcludeRulePreference + i

		if err := m.handle.RuleAdd(excludeRule); err != nil {
			return fmt.Errorf("failed to add exclude rule for %s: %w", excludeNet.String(), err)
		}
		slog.Debug("Added exclude rule", "network", excludeNet.String(), "table", excludeRule.Table, "preference", excludeRule.Priority)
	}

	// Finally add the gateway table rule
	gatewayRule := netlink.NewRule()
	gatewayRule.Table = m.gatewayTableID
	gatewayRule.Priority = m.gatewayTableRulePreference

	if err := m.handle.RuleAdd(gatewayRule); err != nil {
		return fmt.Errorf("failed to add gateway table rule: %w", err)
	}
	slog.Debug("Added gateway table rule", "table", m.gatewayTableID, "preference", gatewayRule.Priority)

	return nil
}

func (m *NetlinkManager) removeRules() error {
	rules, err := m.handle.RuleList(netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	// Transform the slice to a map for easier lookup
	ruleMap := make(map[int]netlink.Rule, len(rules))
	for _, rule := range rules {
		ruleMap[rule.Priority] = rule
	}

	// Remove rules in reverse order
	// First remove the gateway table rule
	if gatewayTableRule, ok := ruleMap[m.gatewayTableRulePreference]; ok {
		if err := m.handle.RuleDel(&gatewayTableRule); err != nil {
			return fmt.Errorf("failed to delete gateway table rule: %w", err)
		}
		slog.Debug("Removed gateway table rule", "preference", m.gatewayTableRulePreference)
	} else {
		slog.Debug("Gateway table rule not found, skipping removal", "preference", m.gatewayTableRulePreference)
	}

	// Then remove the exclude rules
	for i, excludeNet := range m.excludeNets {
		rulePreference := m.firstExcludeRulePreference + i
		if excludeRule, ok := ruleMap[rulePreference]; ok {
			if err := m.handle.RuleDel(&excludeRule); err != nil {
				return fmt.Errorf("failed to delete exclude rule with preference %d for %s: %w", rulePreference, excludeNet.String(), err)
			}
			slog.Debug("Removed exclude rule", "network", excludeNet.String(), "preference", rulePreference)
		} else {
			slog.Debug("Exclude rule not found, skipping removal", "network", excludeNet.String(), "preference", rulePreference)
		}
	}

	// Finally remove the fallthrough table rule
	if fallthroughTableRule, ok := ruleMap[m.fallthroughTableRulePreference]; ok {
		if err := m.handle.RuleDel(&fallthroughTableRule); err != nil {
			return fmt.Errorf("failed to delete fallthrough table rule: %w", err)
		}
		slog.Debug("Removed fallthrough table rule", "preference", m.fallthroughTableRulePreference)
	} else {
		slog.Debug("Fallthrough table rule not found, skipping removal", "preference", m.fallthroughTableRulePreference)
	}

	return nil
}

func (m *NetlinkManager) removeRoutes() error {
	if m.gatewayTableID == 0 {
		return nil
	}

	var cleanupErr error
	err := m.handle.RouteListFilteredIter(netlink.FAMILY_V4, &netlink.Route{Table: m.gatewayTableID}, netlink.RT_FILTER_TABLE, func(route netlink.Route) bool {
		if err := m.handle.RouteDel(&route); err != nil {
			cleanupErr = fmt.Errorf("failed to delete default route via %s: %v", route.Gw, err)
			return false
		}

		slog.Debug("Removed default route", "gateway", route.Gw)
		return true
	})

	return errors.Join(err, cleanupErr)
}

func (m *NetlinkManager) Close() error {
	removeRoutesErr := m.removeRoutes()
	if removeRoutesErr != nil {
		removeRoutesErr = fmt.Errorf("failed to remove routes during close: %w", removeRoutesErr)
	}

	removeRulesErr := m.removeRules()
	if removeRulesErr != nil {
		removeRulesErr = fmt.Errorf("failed to remove rules during close: %w", removeRulesErr)
	}

	m.handle.Close()
	return errors.Join(removeRoutesErr, removeRulesErr)
}

// UpdateDefaultRoute updates the default route to use ECMP with the provided active gateways.
// Only returns an error if a fatal error occurs during route manipulation.
func (m *NetlinkManager) UpdateDefaultRoute(activeGateways []net.IP) error {
	if len(activeGateways) == 0 {
		// Remove existing default route if no gateways are active
		if err := m.removeDefaultRoute(); err != nil {
			return fmt.Errorf("failed to remove default route: %w", err)
		}

		slog.Debug("No active gateways, default route removed")
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
	var cleanupErr error
	err := m.handle.RouteListFilteredIter(netlink.FAMILY_V4, &netlink.Route{Table: m.gatewayTableID}, netlink.RT_FILTER_TABLE, func(route netlink.Route) bool {

		if err := m.handle.RouteDel(&route); err != nil {
			cleanupErr = fmt.Errorf("failed to delete default route via %s: %v", route.Gw, err)
			return false
		}

		slog.Debug("Removed default route", "gateway", route.Gw)
		return true
	})

	if err != nil {
		err = fmt.Errorf("failed to list routes for deletion: %w", err)
	}

	return errors.Join(err, cleanupErr)
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
		Table:     m.gatewayTableID,
	}

	// Try to replace existing route. This is an upsert operation, so if the route
	// does not exist, it will be created.
	if err := m.handle.RouteReplace(route); err != nil {
		return fmt.Errorf("failed to replace/add ECMP route: %w", err)
	}

	gatewayStrings := make([]string, 0, len(gateways))
	for _, gw := range gateways {
		gatewayStrings = append(gatewayStrings, gw.String())
	}
	slog.Debug("Updated ECMP default route", "gateways", gatewayStrings)

	return nil
}

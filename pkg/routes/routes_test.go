package routes

import (
	"errors"
	"net"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

// mockNetlinkHandle is a mock implementation of the netlinkHandle interface
type mockNetlinkHandle struct {
	mock.Mock
}

func (m *mockNetlinkHandle) RouteListFilteredIter(family int, filter *netlink.Route, filterMask uint64, f func(netlink.Route) (cont bool)) error {
	args := m.Called(family, filter, filterMask, f)

	// Get the routes that should be iterated over
	if len(args) > 1 && args.Get(1) != nil {
		routes := args.Get(1).([]netlink.Route)
		for _, route := range routes {
			if !f(route) {
				break
			}
		}
	}

	return args.Error(0)
}

func (m *mockNetlinkHandle) RouteReplace(route *netlink.Route) error {
	args := m.Called(route)
	return args.Error(0)
}

func (m *mockNetlinkHandle) RouteDel(route *netlink.Route) error {
	args := m.Called(route)
	return args.Error(0)
}

func (m *mockNetlinkHandle) RuleList(family int) ([]netlink.Rule, error) {
	args := m.Called(family)
	return args.Get(0).([]netlink.Rule), args.Error(1)
}

func (m *mockNetlinkHandle) RuleAdd(rule *netlink.Rule) error {
	args := m.Called(rule)
	return args.Error(0)
}

func (m *mockNetlinkHandle) RuleDel(rule *netlink.Rule) error {
	args := m.Called(rule)
	return args.Error(0)
}

func (m *mockNetlinkHandle) Close() {
	m.Called()
}

// createTestNetlinkManager creates a NetlinkManager with a mocked handle for testing
func createTestNetlinkManager(mockHandle *mockNetlinkHandle) *NetlinkManager {
	return &NetlinkManager{
		handle:                         mockHandle,
		gatewayTableID:                 100,
		fallthroughTableID:             101,
		firstExcludeRulePreference:     1000,
		fallthroughTableRulePreference: 1002,
		gatewayTableRulePreference:     1001,
		excludeNets:                    []*net.IPNet{},
	}
}

func TestNetlinkManager_UpdateDefaultRoute_NoGateways(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteListFilteredIter to return existing routes in the gateway table
	existingRoutes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 100, // gateway table
		},
	}
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, existingRoutes)
	mockHandle.On("RouteDel", &existingRoutes[0]).Return(nil)

	err := manager.UpdateDefaultRoute([]net.IP{})

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_NoGateways_RouteListError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	expectedError := errors.New("failed to list routes")
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(expectedError, []netlink.Route{})

	err := manager.UpdateDefaultRoute([]net.IP{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove default route")
	assert.Contains(t, err.Error(), "failed to list routes")
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_NoGateways_RouteDelError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	existingRoutes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 100, // gateway table
		},
	}
	expectedError := errors.New("failed to delete route")

	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, existingRoutes)
	mockHandle.On("RouteDel", &existingRoutes[0]).Return(expectedError)

	err := manager.UpdateDefaultRoute([]net.IP{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove default route")
	assert.Contains(t, err.Error(), "failed to delete default route via 192.168.1.1")
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_SingleGateway(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	gateways := []net.IP{net.ParseIP("192.168.1.1")}

	// Mock RouteReplace to succeed
	mockHandle.On("RouteReplace", mock.MatchedBy(func(route *netlink.Route) bool {
		// Verify the route is a default route with the correct gateway and table
		if route.Dst == nil {
			return false
		}
		if !route.Dst.IP.Equal(net.IPv4zero) {
			return false
		}
		ones, bits := route.Dst.Mask.Size()
		if ones != 0 || bits != 32 {
			return false
		}
		if route.Table != 100 { // gateway table
			return false
		}
		if len(route.MultiPath) != 1 {
			return false
		}
		return route.MultiPath[0].Gw.Equal(gateways[0])
	})).Return(nil)

	err := manager.UpdateDefaultRoute(gateways)

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_MultipleGateways(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	gateways := []net.IP{
		net.ParseIP("192.168.1.1"),
		net.ParseIP("192.168.1.2"),
		net.ParseIP("192.168.1.3"),
	}

	// Mock RouteReplace to succeed
	mockHandle.On("RouteReplace", mock.MatchedBy(func(route *netlink.Route) bool {
		// Verify the route is a default route with the correct gateways
		if route.Dst == nil {
			return false
		}
		if !route.Dst.IP.Equal(net.IPv4zero) {
			return false
		}
		ones, bits := route.Dst.Mask.Size()
		if ones != 0 || bits != 32 {
			return false
		}
		if route.Table != 100 { // gateway table
			return false
		}
		if len(route.MultiPath) != 3 {
			return false
		}

		// Verify all gateways are present (order might be different due to sorting)
		gatewayMap := make(map[string]bool)
		for _, gw := range gateways {
			gatewayMap[gw.String()] = false
		}

		for _, nexthop := range route.MultiPath {
			if _, exists := gatewayMap[nexthop.Gw.String()]; !exists {
				return false
			}
			gatewayMap[nexthop.Gw.String()] = true
		}

		// All gateways should be found
		for _, found := range gatewayMap {
			if !found {
				return false
			}
		}

		return true
	})).Return(nil)

	err := manager.UpdateDefaultRoute(gateways)

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_RouteReplaceError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	gateways := []net.IP{net.ParseIP("192.168.1.1")}
	expectedError := errors.New("failed to replace route")

	mockHandle.On("RouteReplace", mock.AnythingOfType("*netlink.Route")).Return(expectedError)

	err := manager.UpdateDefaultRoute(gateways)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to replace/add ECMP route")
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_GatewaySorting(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Provide gateways in unsorted order
	gateways := []net.IP{
		net.ParseIP("192.168.1.3"),
		net.ParseIP("192.168.1.1"),
		net.ParseIP("192.168.1.2"),
	}

	// Expected sorted order: 192.168.1.1, 192.168.1.2, 192.168.1.3
	mockHandle.On("RouteReplace", mock.MatchedBy(func(route *netlink.Route) bool {
		if len(route.MultiPath) != 3 {
			return false
		}
		if route.Table != 100 { // gateway table
			return false
		}
		// Check that gateways are in sorted order
		expectedOrder := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
		for i, nexthop := range route.MultiPath {
			if nexthop.Gw.String() != expectedOrder[i] {
				return false
			}
		}
		return true
	})).Return(nil)

	err := manager.UpdateDefaultRoute(gateways)

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeDefaultRoute_NoExistingRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteListFilteredIter to return no routes
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, []netlink.Route{})

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeDefaultRoute_NonDefaultRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteListFilteredIter to return no routes in gateway table
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, []netlink.Route{})

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeDefaultRoute_WithDefaultRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteListFilteredIter to return routes only from gateway table
	gatewayRoutes := []netlink.Route{
		{
			// Route in gateway table (should be deleted)
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 100, // gateway table
		},
		{
			// Another route in gateway table with ECMP (should be deleted)
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			MultiPath: []*netlink.NexthopInfo{
				{
					Gw: net.ParseIP("192.168.1.2"),
				},
				{
					Gw: net.ParseIP("192.168.1.3"),
				},
			},
			Table: 100, // gateway table
		},
	}

	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, gatewayRoutes)
	mockHandle.On("RouteDel", &gatewayRoutes[0]).Return(nil) // First gateway table route
	mockHandle.On("RouteDel", &gatewayRoutes[1]).Return(nil) // Second gateway table route

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNewNetlinkManager(t *testing.T) {
	// Skip this test if we don't have root privileges, as it tries to modify actual network rules
	if !hasNetworkPrivileges() {
		t.Skip("Skipping test that requires network privileges")
	}

	// This test verifies that NewNetlinkManager creates a manager with a real netlink handle
	_, cidr, err := net.ParseCIDR("10.0.0.0/8")
	require.NoError(t, err)

	manager, err := NewNetlinkManager([]*net.IPNet{cidr}, 100, 1000)

	require.NoError(t, err)
	require.NotNil(t, manager)
	require.NotNil(t, manager.handle)

	// Get the rules via iproute2 to see what was actually added
	output, err := exec.CommandContext(t.Context(), "ip", "rule", "list").CombinedOutput()
	require.NoError(t, err, "ip rule list failed: %s", string(output))

	lines := slices.Collect(strings.Lines(string(output)))
	require.Len(t, lines, 6)

	assert.Equal(t, "0:	from all lookup local\n", lines[0])
	assert.Equal(t, "1000:	from all to 10.0.0.0/8 goto 1002\n", lines[1])
	assert.Equal(t, "1001:	from all lookup 100\n", lines[2])
	assert.Equal(t, "1002:	from all lookup 101\n", lines[3])
	assert.Equal(t, "32766:	from all lookup main\n", lines[4])
	assert.Equal(t, "32767:	from all lookup default\n", lines[5])

	// Clean up the manager to remove any rules that were added
	err = manager.Close()
	require.NoError(t, err)

	// Get the rules again to verify they were removed
	output, err = exec.CommandContext(t.Context(), "ip", "rule", "list").CombinedOutput()
	require.NoError(t, err, "ip rule list failed: %s", string(output))

	lines = slices.Collect(strings.Lines(string(output)))
	require.Len(t, lines, 3)

	assert.Equal(t, "0:	from all lookup local\n", lines[0])
	assert.Equal(t, "32766:	from all lookup main\n", lines[1])
	assert.Equal(t, "32767:	from all lookup default\n", lines[2])

	// Verify that the manager implements the Manager interface
	var _ Manager = manager
}

// hasNetworkPrivileges checks if we have the necessary privileges to modify network rules
func hasNetworkPrivileges() bool {
	// Try to create a dummy handle to see if we have the necessary privileges
	handle, err := netlink.NewHandle()
	if err != nil {
		return false
	}
	defer handle.Close()

	// Try to add a dummy rule to a high table ID to test privileges
	// We use a high table ID to avoid conflicts with existing rules
	testRule := netlink.NewRule()
	testRule.Table = 253      // High table ID unlikely to conflict
	testRule.Priority = 32765 // Very low priority

	err = handle.RuleAdd(testRule)
	if err != nil {
		return false
	}

	// Clean up the test rule
	handle.RuleDel(testRule)
	return true
}

// TestNetlinkManager_InterfaceCompliance verifies that NetlinkManager implements Manager interface
func TestNetlinkManager_InterfaceCompliance(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// This should compile if NetlinkManager properly implements Manager
	var _ Manager = manager

	assert.NotNil(t, manager)
}

// TestMockNetlinkHandle_InterfaceCompliance verifies that our mock implements netlinkHandle interface
func TestMockNetlinkHandle_InterfaceCompliance(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}

	// This should compile if mockNetlinkHandle properly implements netlinkHandle
	var _ netlinkHandle = mockHandle

	assert.NotNil(t, mockHandle)
}

func TestNewNetlinkManager_WithNetworkExclusion(t *testing.T) {
	// Skip this test if we don't have root privileges, as it tries to modify actual network rules
	if !hasNetworkPrivileges() {
		t.Skip("Skipping test that requires network privileges")
	}

	// Test with network exclusion parameters
	excludeNets := []*net.IPNet{
		{
			IP:   net.ParseIP("10.0.0.0"),
			Mask: net.CIDRMask(8, 32),
		},
		{
			IP:   net.ParseIP("192.168.0.0"),
			Mask: net.CIDRMask(16, 32),
		},
	}

	manager, err := NewNetlinkManager(excludeNets, 100, 1000)

	require.NoError(t, err)
	require.NotNil(t, manager)
	require.Equal(t, 100, manager.gatewayTableID)
	require.Equal(t, 101, manager.fallthroughTableID)
	require.Equal(t, 1000, manager.firstExcludeRulePreference)
	require.Equal(t, 1003, manager.fallthroughTableRulePreference) // 1000 + 2 networks + 1
	require.Equal(t, 1002, manager.gatewayTableRulePreference)     // fallthrough - 1
	require.Equal(t, excludeNets, manager.excludeNets)

	// Clean up
	defer manager.Close()
}

func TestNewNetlinkManager_NetworkReduction(t *testing.T) {
	// Test that the network reduction functionality works correctly
	// This test doesn't require network privileges as it uses mock handles

	// Create input with duplicates, subsets, and mergeable networks
	inputNets := []*net.IPNet{
		// Duplicates
		{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
		{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
		// Subsets (these should be removed)
		{IP: net.ParseIP("10.1.1.0"), Mask: net.CIDRMask(24, 32)},
		{IP: net.ParseIP("10.2.2.0"), Mask: net.CIDRMask(24, 32)},
		// Mergeable networks
		{IP: net.ParseIP("192.168.1.0"), Mask: net.CIDRMask(25, 32)},
		{IP: net.ParseIP("192.168.1.128"), Mask: net.CIDRMask(25, 32)},
		// Standalone network
		{IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
	}

	// Create a manager with a mock handle to avoid needing network privileges
	mockHandle := &mockNetlinkHandle{}
	manager := &NetlinkManager{
		handle: mockHandle,
	}

	// Mock the operations that addRules will call
	// First it calls removeRules, which calls RuleList
	mockHandle.On("RuleList", 2).Return([]netlink.Rule{}, nil) // Return empty rule list

	// Then it adds rules for each excluded network plus the two table rules
	// We expect 3 reduced networks + 2 table rules = 5 RuleAdd calls
	mockHandle.On("RuleAdd", mock.Anything).Return(nil).Times(5)

	// Call excludeNetworks to test the reduction
	err := manager.excludeNetworks(inputNets, 100, 1000)
	require.NoError(t, err)

	// Verify the networks were properly reduced
	// Expected result: [10.0.0.0/8, 192.168.1.0/24, 172.16.0.0/12]
	expectedCount := 3
	require.Len(t, manager.excludeNets, expectedCount)

	// Check that the specific expected networks are present
	expectedNetworks := map[string]bool{
		"10.0.0.0/8":     false,
		"192.168.1.0/24": false,
		"172.16.0.0/12":  false,
	}

	for _, network := range manager.excludeNets {
		networkStr := network.String()
		if _, exists := expectedNetworks[networkStr]; exists {
			expectedNetworks[networkStr] = true
		} else {
			t.Errorf("Unexpected network in result: %s", networkStr)
		}
	}

	// Verify all expected networks were found
	for networkStr, found := range expectedNetworks {
		require.True(t, found, "Expected network %s not found in result", networkStr)
	}

	// Verify the rule preferences were calculated correctly for the reduced set
	require.Equal(t, 1000, manager.firstExcludeRulePreference)
	require.Equal(t, 1004, manager.fallthroughTableRulePreference) // 1000 + 3 networks + 1
	require.Equal(t, 1003, manager.gatewayTableRulePreference)     // fallthrough - 1

	// Verify all mock expectations were met
	mockHandle.AssertExpectations(t)
}

func TestNewNetlinkManager_InvalidParameters(t *testing.T) {
	tests := []struct {
		name           string
		excludeNets    []*net.IPNet
		firstTableID   int
		firstRulePref  int
		expectedErrMsg string
		skipIfNoPrivs  bool
	}{
		{
			name:           "invalid table ID too low",
			excludeNets:    []*net.IPNet{},
			firstTableID:   0,
			firstRulePref:  1000,
			expectedErrMsg: "invalid first table ID",
			skipIfNoPrivs:  false,
		},
		{
			name:           "invalid table ID too high",
			excludeNets:    []*net.IPNet{},
			firstTableID:   255,
			firstRulePref:  1000,
			expectedErrMsg: "invalid first table ID",
			skipIfNoPrivs:  false,
		},
		{
			name:           "invalid rule preference too low",
			excludeNets:    []*net.IPNet{},
			firstTableID:   100,
			firstRulePref:  0,
			expectedErrMsg: "invalid first rule preference",
			skipIfNoPrivs:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipIfNoPrivs && !hasNetworkPrivileges() {
				t.Skip("Skipping test that requires network privileges")
			}

			manager, err := NewNetlinkManager(tt.excludeNets, tt.firstTableID, tt.firstRulePref)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
			assert.Nil(t, manager)
		})
	}
}

func TestNetlinkManager_Close(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock the route removal operations (removeRoutes)
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, []netlink.Route{})

	// Mock the rule list and deletion operations
	rules := []netlink.Rule{
		{Priority: 1001, Table: 100}, // gateway table rule
		{Priority: 1002, Table: 101}, // fallthrough table rule
	}

	mockHandle.On("RuleList", netlink.FAMILY_V4).Return(rules, nil)
	mockHandle.On("RuleDel", &rules[0]).Return(nil) // gateway rule
	mockHandle.On("RuleDel", &rules[1]).Return(nil) // fallthrough rule
	mockHandle.On("Close").Return()

	err := manager.Close()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeRoutes_NoRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteListFilteredIter to return no routes
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, []netlink.Route{})

	err := manager.removeRoutes()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeRoutes_WithRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock routes that should be removed
	routes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 100,
		},
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.2"),
			Table: 100,
		},
	}

	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, routes)
	mockHandle.On("RouteDel", &routes[0]).Return(nil)
	mockHandle.On("RouteDel", &routes[1]).Return(nil)

	err := manager.removeRoutes()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeRoutes_ListError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	expectedError := errors.New("failed to list routes")
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(expectedError, []netlink.Route{})

	err := manager.removeRoutes()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list routes")
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeRoutes_DeleteError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	routes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 100,
		},
	}

	expectedError := errors.New("failed to delete route")
	mockHandle.On("RouteListFilteredIter", netlink.FAMILY_V4, &netlink.Route{Table: 100}, uint64(netlink.RT_FILTER_TABLE), mock.AnythingOfType("func(netlink.Route) bool")).Return(nil, routes)
	mockHandle.On("RouteDel", &routes[0]).Return(expectedError)

	err := manager.removeRoutes()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete default route via 192.168.1.1")
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeRoutes_GatewayTableIDZero(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Set gatewayTableID to 0 to test the early return
	manager.gatewayTableID = 0

	err := manager.removeRoutes()

	assert.NoError(t, err)
	// No mock expectations should be called since the function returns early
}

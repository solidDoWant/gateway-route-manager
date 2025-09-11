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

func (m *mockNetlinkHandle) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	args := m.Called(link, family)
	return args.Get(0).([]netlink.Route), args.Error(1)
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

	// Mock RouteList to return existing routes in the gateway table
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
	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return(existingRoutes, nil)
	mockHandle.On("RouteDel", &existingRoutes[0]).Return(nil)

	err := manager.UpdateDefaultRoute([]net.IP{})

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_UpdateDefaultRoute_NoGateways_RouteListError(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	expectedError := errors.New("failed to list routes")
	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return([]netlink.Route{}, expectedError)

	err := manager.UpdateDefaultRoute([]net.IP{})

	assert.Error(t, err)
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

	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return(existingRoutes, nil)
	mockHandle.On("RouteDel", &existingRoutes[0]).Return(expectedError)

	err := manager.UpdateDefaultRoute([]net.IP{})

	assert.Error(t, err)
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

	assert.Error(t, err)
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

	// Mock RouteList to return no routes
	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return([]netlink.Route{}, nil)

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeDefaultRoute_NonDefaultRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteList to return non-default routes
	routes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.ParseIP("10.0.0.0"),
				Mask: net.CIDRMask(8, 32),
			},
			Gw: net.ParseIP("192.168.1.1"),
		},
	}
	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return(routes, nil)

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestNetlinkManager_removeDefaultRoute_WithDefaultRoutes(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteList to return mix of routes in different tables
	routes := []netlink.Route{
		{
			// Route in main table (should be ignored)
			Dst: &net.IPNet{
				IP:   net.ParseIP("10.0.0.0"),
				Mask: net.CIDRMask(8, 32),
			},
			Gw:    net.ParseIP("192.168.1.1"),
			Table: 254, // main table
		},
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

	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return(routes, nil)
	mockHandle.On("RouteDel", &routes[1]).Return(nil) // First gateway table route
	mockHandle.On("RouteDel", &routes[2]).Return(nil) // Second gateway table route

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

package routes

import (
	"errors"
	"net"
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

// createTestNetlinkManager creates a NetlinkManager with a mocked handle for testing
func createTestNetlinkManager(mockHandle *mockNetlinkHandle) *NetlinkManager {
	return &NetlinkManager{
		handle: mockHandle,
	}
}

func TestNetlinkManager_UpdateDefaultRoute_NoGateways(t *testing.T) {
	mockHandle := &mockNetlinkHandle{}
	manager := createTestNetlinkManager(mockHandle)

	// Mock RouteList to return existing default routes
	existingRoutes := []netlink.Route{
		{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw: net.ParseIP("192.168.1.1"),
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
			Gw: net.ParseIP("192.168.1.1"),
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
		// Verify the route is a default route with the correct gateway
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

	// Mock RouteList to return mix of default and non-default routes
	routes := []netlink.Route{
		{
			// Non-default route
			Dst: &net.IPNet{
				IP:   net.ParseIP("10.0.0.0"),
				Mask: net.CIDRMask(8, 32),
			},
			Gw: net.ParseIP("192.168.1.1"),
		},
		{
			// Default route
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Gw: net.ParseIP("192.168.1.1"),
		},
		{
			// Another default route
			Dst: nil, // nil Dst also indicates default route
			Gw:  net.ParseIP("192.168.1.2"),
		},
	}

	mockHandle.On("RouteList", nil, netlink.FAMILY_V4).Return(routes, nil)
	mockHandle.On("RouteDel", &routes[1]).Return(nil) // First default route
	mockHandle.On("RouteDel", &routes[2]).Return(nil) // Second default route

	err := manager.removeDefaultRoute()

	assert.NoError(t, err)
	mockHandle.AssertExpectations(t)
}

func TestIsDefaultRoute(t *testing.T) {
	tests := []struct {
		name     string
		route    netlink.Route
		expected bool
	}{
		{
			name: "default route with 0.0.0.0/0",
			route: netlink.Route{
				Dst: &net.IPNet{
					IP:   net.IPv4zero,
					Mask: net.CIDRMask(0, 32),
				},
				Gw: net.ParseIP("192.168.1.1"),
			},
			expected: true,
		},
		{
			name: "default route with nil destination",
			route: netlink.Route{
				Dst: nil,
				Gw:  net.ParseIP("192.168.1.1"),
			},
			expected: true,
		},
		{
			name: "non-default route",
			route: netlink.Route{
				Dst: &net.IPNet{
					IP:   net.ParseIP("10.0.0.0"),
					Mask: net.CIDRMask(8, 32),
				},
				Gw: net.ParseIP("192.168.1.1"),
			},
			expected: false,
		},
		{
			name: "route without gateway",
			route: netlink.Route{
				Dst: &net.IPNet{
					IP:   net.IPv4zero,
					Mask: net.CIDRMask(0, 32),
				},
				Gw: nil,
			},
			expected: false,
		},
		{
			name: "route with non-zero IP",
			route: netlink.Route{
				Dst: &net.IPNet{
					IP:   net.ParseIP("192.168.1.0"),
					Mask: net.CIDRMask(24, 32),
				},
				Gw: net.ParseIP("192.168.1.1"),
			},
			expected: false,
		},
		{
			name: "route with non-zero mask",
			route: netlink.Route{
				Dst: &net.IPNet{
					IP:   net.IPv4zero,
					Mask: net.CIDRMask(8, 32),
				},
				Gw: net.ParseIP("192.168.1.1"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDefaultRoute(tt.route)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewNetlinkManager(t *testing.T) {
	// This test verifies that NewNetlinkManager creates a manager with a real netlink handle
	// We can't easily mock netlink.NewHandle(), so we just verify the function works
	manager, err := NewNetlinkManager()

	require.NoError(t, err)
	require.NotNil(t, manager)
	require.NotNil(t, manager.handle)

	// Verify that the manager implements the Manager interface
	var _ Manager = manager
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

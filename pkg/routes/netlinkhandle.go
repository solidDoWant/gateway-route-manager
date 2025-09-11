package routes

import (
	"github.com/vishvananda/netlink"
)

// netlinkHandle is an interface that abstracts the netlink.Handle methods used in this package.
// This allows for easier testing and mocking of netlink interactions.
// These should be a 1:1 mapping of the methods used from netlink.Handle for normal operations.
type netlinkHandle interface {
	// Routes
	RouteListFilteredIter(family int, filter *netlink.Route, filterMask uint64, f func(netlink.Route) (cont bool)) error
	RouteReplace(route *netlink.Route) error
	RouteDel(route *netlink.Route) error

	// Rules
	RuleList(family int) ([]netlink.Rule, error)
	RuleAdd(rule *netlink.Rule) error
	RuleDel(rule *netlink.Rule) error

	// Cleanup
	Close()
}

var _ netlinkHandle = (*netlink.Handle)(nil)

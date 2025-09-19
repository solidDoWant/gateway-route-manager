package iputil

import (
	"github.com/vishvananda/netlink"
)

// netlinkHandle is an interface that abstracts the netlink.Handle methods used in this package.
// This allows for easier testing and mocking of netlink interactions.
// These should be a 1:1 mapping of the methods used from netlink.Handle for normal operations.
type NetlinkHandle interface {
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

var _ NetlinkHandle = (*netlink.Handle)(nil)

func NewRealNetlinkHandle() NetlinkHandle {
	// For some inexplicable reason, using netlink.NewHandle() causes the program to hang indefinitely when
	// trying to delete specific routes. This seems to only happen on default routes attached to a table.
	// Using an empty handle seems to work fine, and is what the netlink package uses internally when no
	// specific handle is provided.
	// TODO file a bug report with the netlink package about this issue
	return &netlink.Handle{}
}

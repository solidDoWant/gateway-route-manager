package ddns

import (
	"context"
)

// Provider defines the interface for DDNS providers.
type Provider interface {
	// UpdateRecords updates the DNS records with the provided IP addresses.
	// If no IP addresses are provided, the provider should remove any existing records.
	// Extra A records should be removed.
	UpdateRecords(ctx context.Context, ips []string) error
	// Name returns the name of the provider.
	Name() string
}

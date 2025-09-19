package ddns

import (
	"fmt"
	"strings"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
)

// NewProvider creates a new DDNS provider based on the configuration
func NewProvider(cfg config.Config) (Provider, error) {
	if !cfg.IsDDNSEnabled() {
		return nil, fmt.Errorf("DDNS is not enabled")
	}

	switch strings.ToLower(cfg.DDNSProvider) {
	case "changeip":
		return NewChangeIPProvider(
			cfg.DDNSUsername,
			cfg.DDNSPassword,
			cfg.DDNSHostname,
			cfg.Timeout,
		), nil
	case "dynudns":
		return NewDynuDNSProvider(
			cfg.DDNSPassword, // API key
			cfg.DDNSHostname,
			cfg.Timeout,
		)
	default:
		return nil, fmt.Errorf("unsupported DDNS provider: %s", cfg.DDNSProvider)
	}
}

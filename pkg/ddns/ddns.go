package ddns

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/gateway"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/iputil"
	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/metrics"
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

type Updater struct {
	provider Provider
	config   config.Config
	metrics  *metrics.Metrics
	handle   iputil.NetlinkHandle

	nextActiveGateways atomic.Value
	updateChan         chan struct{}
	lastActiveIPs      atomic.Value
}

func NewUpdater(cfg config.Config, m *metrics.Metrics) (*Updater, error) {
	u := &Updater{
		// provider: provider,
		config:     cfg,
		metrics:    m,
		handle:     iputil.NewRealNetlinkHandle(),
		updateChan: make(chan struct{}),
	}

	if cfg.DDNSProvider != "" {
		provider, err := NewProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create DDNS provider: %w", err)
		}

		u.provider = provider
		u.nextActiveGateways.Store([]gateway.Gateway{})
		u.lastActiveIPs.Store([]string{})
	}

	return u, nil
}

func (u *Updater) Close() {
	u.handle.Close()
	if u.updateChan != nil {
		close(u.updateChan)
	}
}

func (u *Updater) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.updateChan:
		}

		updateCtx, cancel := context.WithTimeout(ctx, u.config.DDNSTimeout)
		defer cancel()

		if err := u.update(updateCtx); err != nil {
			slog.ErrorContext(updateCtx, "DDNS update failed", "error", err)
		}
	}
}

func (u *Updater) ScheduleUpdate(activeGateways []gateway.Gateway) {
	if u.provider == nil {
		return
	}

	// Use this for a comparison. Gateways are uniquely identified by their IP.
	nextActiveIPs := make([]string, 0, len(activeGateways))
	for _, gw := range activeGateways {
		nextActiveIPs = append(nextActiveIPs, gw.IP.String())
	}
	slices.Sort(nextActiveIPs)

	currentlyScheduledNextActiveGateways := u.nextActiveGateways.Load().([]gateway.Gateway)
	currentlyScheduledNextActiveGatewayIPs := make([]string, 0, len(currentlyScheduledNextActiveGateways))
	for _, gw := range currentlyScheduledNextActiveGateways {
		currentlyScheduledNextActiveGatewayIPs = append(currentlyScheduledNextActiveGatewayIPs, gw.IP.String())
	}
	slices.Sort(currentlyScheduledNextActiveGatewayIPs)

	if slices.Compare(nextActiveIPs, currentlyScheduledNextActiveGatewayIPs) == 0 {
		// No change, no need to schedule an update
		return
	}

	u.nextActiveGateways.Swap(activeGateways)
	select {
	case u.updateChan <- struct{}{}:
	default:
	}
}

func (u *Updater) update(ctx context.Context) error {
	if u.provider == nil {
		return nil
	}

	activeGateways := u.nextActiveGateways.Load().([]gateway.Gateway)

	// Check if DDNS requires a specific IP address to be present on an interface
	if u.config.DDNSRequireIPAddress != "" {
		hasRequiredIP, err := iputil.HasInterfaceWithIP(u.config.DDNSRequireIPAddress)
		if err != nil {
			return fmt.Errorf("failed to check for required IP address %s: %w", u.config.DDNSRequireIPAddress, err)
		}

		if !hasRequiredIP {
			// Skip DDNS update but record the event
			u.metrics.DDNSUpdatesSkippedTotal.WithLabelValues(u.provider.Name(), "required_ip_not_found").Inc()
			slog.DebugContext(ctx, "Skipping DDNS update: required IP address not found on any interface",
				"required_ip", u.config.DDNSRequireIPAddress)
			return nil
		}

		slog.DebugContext(ctx, "Required IP address found on interface, proceeding with DDNS update",
			"required_ip", u.config.DDNSRequireIPAddress)
	}

	// Collect public IPs from active gateways
	publicIPs := make([]string, 0, len(activeGateways))
	for _, gw := range activeGateways {
		if err := gw.FetchPublicIP(ctx, u.config.PublicIPService, u.config.Timeout); err != nil {
			slog.WarnContext(ctx, "Failed to fetch public IP from gateway", "gateway", gw.IP.String(), "error", err)
			continue
		}
		publicIPs = append(publicIPs, gw.PublicIP)
	}

	// Remove duplicates and sort
	uniqueIPs := make(map[string]struct{}, len(publicIPs))
	for _, ip := range publicIPs {
		uniqueIPs[ip] = struct{}{}
	}

	publicIPs = publicIPs[:0]
	for ip := range uniqueIPs {
		publicIPs = append(publicIPs, ip)
	}
	slices.Sort(publicIPs)

	u.metrics.UniquePublicIPsGauge.Set(float64(len(publicIPs)))

	lastActivePublicIPs := u.lastActiveIPs.Load().([]string)
	if !slices.Equal(publicIPs, lastActivePublicIPs) {
		// Record that public IPs have changed
		u.metrics.PublicIPChangesTotal.Inc()

		providerName := u.provider.Name()
		slog.InfoContext(ctx, "Public IPs changed, updating DDNS", "ips", publicIPs)

		start := time.Now()
		err := u.provider.UpdateRecords(ctx, publicIPs)
		u.metrics.DDNSUpdateDurationSeconds.WithLabelValues(providerName).Observe(time.Since(start).Seconds())

		if err != nil {
			// Record failed DDNS update
			u.metrics.DDNSUpdatesTotal.WithLabelValues(providerName, "failure").Inc()
			return fmt.Errorf("failed to update DNS records: %w", err)
		}

		// Record successful DDNS update
		u.metrics.DDNSUpdatesTotal.WithLabelValues(providerName, "success").Inc()
	} else {
		// Record skipped DDNS update
		u.metrics.DDNSUpdatesSkippedTotal.WithLabelValues(u.provider.Name(), "no_change").Inc()
		slog.DebugContext(ctx, "Public IPs unchanged, skipping DDNS update", "ips", publicIPs)
	}

	u.lastActiveIPs.Store(publicIPs)
	return nil
}

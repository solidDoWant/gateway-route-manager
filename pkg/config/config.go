package config

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"slices"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/iputil"
)

// See https://en.wikipedia.org/wiki/Reserved_IP_addresses#IPv4 for a full list
var reservedCIDRs = []string{
	"0.0.0.0/8",       // "This" Network
	"10.0.0.0/8",      // Private network
	"100.64.0.0/10",   // Carrier-grade NAT
	"127.0.0.0/8",     // Loopback
	"169.254.0.0/16",  // Link-local
	"172.16.0.0/12",   // Private network
	"192.0.0.0/24",    // IETF Protocol Assignments
	"192.0.2.0/24",    // TEST-NET-1
	"192.88.99.0/24",  // 6to4 Relay Anycast
	"192.168.0.0/16",  // Private network
	"198.18.0.0/15",   // Network benchmark tests
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/3",     // Multicast + MCAST-TEST-NET + Reserved for future use + Broadcast
}

// Config holds all configuration options for the gateway route manager
type Config struct {
	StartIP             string
	EndIP               string
	Timeout             time.Duration
	CheckPeriod         time.Duration
	Port                int
	URLPath             string
	Scheme              string
	LogLevel            string
	MetricsPort         int
	CIDRsToExclude      []*net.IPNet
	FirstRoutingTableID int
	FirstRulePreference int
}

// ParseFlags parses command line flags and returns a Config struct
func ParseFlags(args []string) Config {
	var config Config

	var cidrsToExclude []*net.IPNet

	flag.StringVar(&config.StartIP, "start-ip", "", "Starting IP address for the range")
	flag.StringVar(&config.EndIP, "end-ip", "", "Ending IP address for the range")
	flag.DurationVar(&config.Timeout, "timeout", 1*time.Second, "Timeout for health checks")
	flag.DurationVar(&config.CheckPeriod, "check-period", 3*time.Second, "How often to check gateways")
	flag.IntVar(&config.Port, "port", 80, "Port to target for health checks")
	flag.StringVar(&config.URLPath, "path", "/", "URL path for health checks")
	flag.StringVar(&config.Scheme, "scheme", "http", "Scheme to use (http or https)")
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&config.MetricsPort, "metrics-port", 9090, "Port for Prometheus metrics endpoint")
	flag.IntVar(&config.FirstRoutingTableID, "first-routing-table-id", 180, "First routing table ID to use for gateway route logic")
	flag.IntVar(&config.FirstRulePreference, "first-rule-preference", 10888, "First rule preference to use for gateway route logic")
	flag.Func("exclude-cidr", "CIDR to exclude from gateway routing (can be specified multiple times)", func(s string) error {
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR: %w", err)
		}

		cidrsToExclude = append(cidrsToExclude, cidr)
		return nil
	})
	excludeReservedDestinations := flag.Bool("exclude-reserved-cidrs", true, "Exclude reserved IPv4 destinations (private networks, lookback, multicast, etc.) from gateway routing")

	flag.CommandLine.Parse(args)

	if excludeReservedDestinations != nil && *excludeReservedDestinations {
		for _, cidrStr := range reservedCIDRs {
			_, cidr, err := net.ParseCIDR(cidrStr)
			if err != nil {
				slog.Error("failed to parse reserved CIDR (this is a bug)", "cidr", cidrStr, "error", err)
				panic(err)
			}
			cidrsToExclude = append(cidrsToExclude, cidr)
		}
	}

	config.CIDRsToExclude = cidrsToExclude
	return config
}

// Validate validates the configuration and returns an error if invalid
func (c Config) Validate() error {
	if c.StartIP == "" || c.EndIP == "" {
		return fmt.Errorf("start-ip and end-ip are required")
	}

	// Validate that start and end IPs are valid
	startIP := net.ParseIP(c.StartIP)
	if startIP == nil {
		return fmt.Errorf("invalid start-ip: %s", c.StartIP)
	}

	endIP := net.ParseIP(c.EndIP)
	if endIP == nil {
		return fmt.Errorf("invalid end-ip: %s", c.EndIP)
	}

	// Validate that end IP is after start IP
	if startIP.Equal(endIP) {
		// Allow equal IPs (single IP range)
	} else if iputil.IsIPGreater(startIP, endIP) {
		return fmt.Errorf("start-ip (%s) must be less than or equal to end-ip (%s)", c.StartIP, c.EndIP)
	}

	if c.CheckPeriod < c.Timeout {
		return fmt.Errorf("check-period (%v) must be at least as long as timeout (%v)",
			c.CheckPeriod, c.Timeout)
	}

	if c.Scheme != "http" && c.Scheme != "https" {
		return fmt.Errorf("scheme must be 'http' or 'https'")
	}

	if c.MetricsPort < 1 || c.MetricsPort > 65535 {
		return fmt.Errorf("metrics port must be between 1 and 65535")
	}

	// Validate log level
	normalizedLevel := strings.ToLower(c.LogLevel)
	validLevels := []string{"debug", "info", "warn", "error"}
	isValid := slices.Contains(validLevels, normalizedLevel)
	if !isValid {
		return fmt.Errorf("log level must be one of: %s", strings.Join(validLevels, ", "))
	}

	return nil
}

// GetSlogLevel converts the LogLevel string to a slog.Level
func (c Config) GetSlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default fallback
	}
}

package config

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
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

var ddnsProviders = []string{"dynudns"}

// PublicIPServiceConfig holds configuration for the public IP service
type PublicIPServiceConfig struct {
	Port     int
	Hostname string
	Scheme   string
	Path     string
	Username string
	Password string
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
	Routes              []*net.IPNet
	// DDNS configuration
	DDNSProvider         string
	DDNSUsername         string
	DDNSPassword         string
	DDNSHostname         string
	DDNSRequireIPAddress string
	DDNSTimeout          time.Duration
	DDNSTTL              time.Duration
	// Public IP service configuration
	PublicIPService PublicIPServiceConfig
}

// ParseFlags parses command line flags and returns a Config struct
func ParseFlags(args []string) Config {
	var config Config

	var cidrsToExclude []*net.IPNet

	flag.StringVar(&config.StartIP, "start-ip", "", "Starting IP address for the range")
	flag.StringVar(&config.EndIP, "end-ip", "", "Ending IP address for the range")
	flag.DurationVar(&config.Timeout, "timeout", 1*time.Second, "Timeout for health checks")
	flag.DurationVar(&config.CheckPeriod, "check-period", 3*time.Second, "How often to check gateways")
	flag.IntVar(&config.Port, "port", 9999, "Port to target for health checks")
	flag.StringVar(&config.URLPath, "path", "/", "URL path for health checks")
	flag.StringVar(&config.Scheme, "scheme", "http", "Scheme to use (http or https)")
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&config.MetricsPort, "metrics-port", 9090, "Port for Prometheus metrics endpoint")
	flag.IntVar(&config.FirstRoutingTableID, "first-routing-table-id", 180, "First routing table ID to use for gateway route logic")
	flag.IntVar(&config.FirstRulePreference, "first-rule-preference", 10888, "First rule preference to use for gateway route logic")
	flag.Func("route", "Routes to manage in CIDR notation or 'default'", func(s string) error {
		if s == "default" {
			s = "0.0.0.0/0"
		}

		_, destination, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid route: %w", err)
		}

		config.Routes = append(config.Routes, destination)
		return nil
	})

	// DDNS configuration flags
	flag.StringVar(&config.DDNSProvider, "ddns-provider", "", fmt.Sprintf("DDNS provider (currently supports: %s)", strings.Join(ddnsProviders, ", ")))
	flag.StringVar(&config.DDNSUsername, "ddns-username", "", "DDNS username (required for some providers)")
	flag.StringVar(&config.DDNSPassword, "ddns-password", "", "DDNS password or API key (required if DDNS provider is specified, defaults to DDNS_PASSWORD)")
	flag.StringVar(&config.DDNSHostname, "ddns-hostname", "", "DDNS hostname to update (required if DDNS provider is specified)")
	flag.StringVar(&config.DDNSRequireIPAddress, "ddns-require-ip-address", "", "IPv4 address that must be assigned to an interface for DDNS updates to be performed")
	flag.DurationVar(&config.DDNSTimeout, "ddns-timeout", time.Minute, "Timeout for DDNS updates")
	flag.DurationVar(&config.DDNSTTL, "ddns-record-ttl", time.Minute, "TTL for managed DDNS records")

	// Public IP service configuration flags
	flag.StringVar(&config.PublicIPService.Hostname, "public-ip-service-hostname", "", "Hostname for public IP service (if unset, queries each gateway)")
	flag.IntVar(&config.PublicIPService.Port, "public-ip-service-port", 443, "Port for gateway's public IP service to fetch its public IP addresses")
	flag.StringVar(&config.PublicIPService.Scheme, "public-ip-service-scheme", "https", "Scheme for public IP service (http or https)")
	flag.StringVar(&config.PublicIPService.Path, "public-ip-service-path", "/", "URL path for public IP service")
	flag.StringVar(&config.PublicIPService.Username, "public-ip-service-username", "", "Username for public IP service HTTP basic auth")
	flag.StringVar(&config.PublicIPService.Password, "public-ip-service-password", "", "Password for public IP service HTTP basic auth (defaults to PUBLIC_IP_SERVICE_PASSWORD)")

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

	// Handle DDNS password fallback to environment variable
	if config.DDNSPassword == "" && config.DDNSProvider != "" {
		config.DDNSPassword = os.Getenv("DDNS_PASSWORD")
	}

	// Handle public IP service password fallback to environment variable
	if config.PublicIPService.Password == "" {
		config.PublicIPService.Password = os.Getenv("PUBLIC_IP_SERVICE_PASSWORD")
	}

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

	if len(config.Routes) == 0 {
		config.Routes = []*net.IPNet{
			{
				IP:   net.IPv4(0, 0, 0, 0),
				Mask: net.CIDRMask(0, 32),
			},
		}
	}

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

	// Validate DDNS configuration
	if c.DDNSProvider != "" {
		if !slices.Contains(ddnsProviders, strings.ToLower(c.DDNSProvider)) {
			return fmt.Errorf("ddns-provider must be one of: %s", strings.Join(ddnsProviders, ", "))
		}

		// DynuDNS uses API key authentication via password only
		if strings.ToLower(c.DDNSProvider) != "dynudns" {
			// Other providers require both username and password
			if c.DDNSUsername == "" {
				return fmt.Errorf("ddns-username is required")
			}
		}

		if c.DDNSPassword == "" {
			return fmt.Errorf("ddns-password is required when ddns-provider is specified (can be provided via DDNS_PASSWORD)")
		}

		if c.DDNSHostname == "" {
			return fmt.Errorf("ddns-hostname is required when ddns-provider is specified")
		}

		if c.DDNSTimeout <= 0 {
			return fmt.Errorf("ddns-timeout must be greater than zero")
		}

		if c.DDNSTTL <= 0 {
			return fmt.Errorf("ddns-record-ttl must be greater than zero")
		}
	}

	// Validate DDNS require IP address if provided
	if c.DDNSRequireIPAddress != "" {
		ip := net.ParseIP(c.DDNSRequireIPAddress)
		if ip == nil {
			return fmt.Errorf("invalid ddns-require-ip-address: %s", c.DDNSRequireIPAddress)
		}

		// Ensure it's an IPv4 address
		if ip.To4() == nil {
			return fmt.Errorf("ddns-require-ip-address must be an IPv4 address: %s", c.DDNSRequireIPAddress)
		}
	}

	if c.PublicIPService.Port < 1 || c.PublicIPService.Port > 65535 {
		return fmt.Errorf("public-ip-service-port must be between 1 and 65535")
	}

	// Validate public IP service configuration
	if c.PublicIPService.Scheme != "" {
		if c.PublicIPService.Scheme != "http" && c.PublicIPService.Scheme != "https" {
			return fmt.Errorf("public-ip-service-scheme must be 'http' or 'https'")
		}
	}

	// If username is provided, password should also be provided
	if c.PublicIPService.Username != "" && c.PublicIPService.Password == "" ||
		c.PublicIPService.Username == "" && c.PublicIPService.Password != "" {
		return fmt.Errorf("public-ip-service-username and public-ip-service-password must be specified together or not at all")
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

// IsDDNSEnabled returns true if DDNS is configured
func (c Config) IsDDNSEnabled() bool {
	return c.DDNSProvider != ""
}

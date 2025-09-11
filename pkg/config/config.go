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

// Config holds all configuration options for the gateway route manager
type Config struct {
	StartIP     string
	EndIP       string
	Timeout     time.Duration
	CheckPeriod time.Duration
	Port        int
	URLPath     string
	Scheme      string
	LogLevel    string
	MetricsPort int
}

// ParseFlags parses command line flags and returns a Config struct
func ParseFlags(args []string) Config {
	var config Config

	flag.StringVar(&config.StartIP, "start-ip", "", "Starting IP address for the range")
	flag.StringVar(&config.EndIP, "end-ip", "", "Ending IP address for the range")
	flag.DurationVar(&config.Timeout, "timeout", 1*time.Second, "Timeout for health checks")
	flag.DurationVar(&config.CheckPeriod, "check-period", 3*time.Second, "How often to check gateways")
	flag.IntVar(&config.Port, "port", 80, "Port to target for health checks")
	flag.StringVar(&config.URLPath, "path", "/", "URL path for health checks")
	flag.StringVar(&config.Scheme, "scheme", "http", "Scheme to use (http or https)")
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&config.MetricsPort, "metrics-port", 9090, "Port for Prometheus metrics endpoint")

	flag.CommandLine.Parse(args)

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

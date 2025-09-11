package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		errFunc require.ErrorAssertionFunc
		errMsg  string
	}{
		{
			name: "valid config with single IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 9090,
			},
		},
		{
			name: "valid config with IP range",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/health",
				Scheme:      "https",
				LogLevel:    "debug",
				MetricsPort: 8080,
			},
		},
		{
			name: "valid config with timeout equal to check period",
			config: Config{
				StartIP:     "10.0.0.1",
				EndIP:       "10.0.0.5",
				Timeout:     5 * time.Second,
				CheckPeriod: 5 * time.Second,
				Port:        443,
				URLPath:     "/api/health",
				Scheme:      "https",
				LogLevel:    "warn",
				MetricsPort: 3000,
			},
		},
		{
			name: "missing start IP",
			config: Config{
				StartIP:     "",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "missing end IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "missing both IPs",
			config: Config{
				StartIP:     "",
				EndIP:       "",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "start-ip and end-ip are required",
		},
		{
			name: "invalid start IP",
			config: Config{
				StartIP:     "invalid-ip",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "invalid start-ip: invalid-ip",
		},
		{
			name: "invalid end IP",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "not-an-ip",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "invalid end-ip: not-an-ip",
		},
		{
			name: "start IP greater than end IP",
			config: Config{
				StartIP:     "192.168.1.10",
				EndIP:       "192.168.1.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "start-ip (192.168.1.10) must be less than or equal to end-ip (192.168.1.1)",
		},
		{
			name: "check period less than timeout",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     5 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "check-period (3s) must be at least as long as timeout (5s)",
		},
		{
			name: "invalid scheme - empty",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "scheme must be 'http' or 'https'",
		},
		{
			name: "invalid scheme - wrong value",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "ftp",
				MetricsPort: 9090,
			},
			errFunc: require.Error,
			errMsg:  "scheme must be 'http' or 'https'",
		},
		{
			name: "metrics port too low",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 0,
			},
			errFunc: require.Error,
			errMsg:  "metrics port must be between 1 and 65535",
		},
		{
			name: "metrics port too high",
			config: Config{
				StartIP:     "192.168.1.1",
				EndIP:       "192.168.1.10",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				MetricsPort: 65536,
			},
			errFunc: require.Error,
			errMsg:  "metrics port must be between 1 and 65535",
		},
		{
			name: "edge case - port 1 (minimum valid port)",
			config: Config{
				StartIP:     "127.0.0.1",
				EndIP:       "127.0.0.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "info",
				MetricsPort: 1,
			},
		},
		{
			name: "edge case - port 65535 (maximum valid port)",
			config: Config{
				StartIP:     "127.0.0.1",
				EndIP:       "127.0.0.1",
				Timeout:     1 * time.Second,
				CheckPeriod: 3 * time.Second,
				Port:        80,
				URLPath:     "/",
				Scheme:      "http",
				LogLevel:    "error",
				MetricsPort: 65535,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errFunc == nil {
				tt.errFunc = require.NoError
			}

			err := tt.config.Validate()
			tt.errFunc(t, err)
		})
	}
}

func TestConfig_GetSlogLevel(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		expected slog.Level
	}{
		{
			name:     "debug level",
			logLevel: "debug",
			expected: slog.LevelDebug,
		},
		{
			name:     "info level",
			logLevel: "info",
			expected: slog.LevelInfo,
		},
		{
			name:     "warn level",
			logLevel: "warn",
			expected: slog.LevelWarn,
		},
		{
			name:     "error level",
			logLevel: "error",
			expected: slog.LevelError,
		},
		{
			name:     "case insensitive DEBUG",
			logLevel: "DEBUG",
			expected: slog.LevelDebug,
		},
		{
			name:     "case insensitive Info",
			logLevel: "Info",
			expected: slog.LevelInfo,
		},
		{
			name:     "invalid level defaults to info",
			logLevel: "invalid",
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{LogLevel: tt.logLevel}
			actual := config.GetSlogLevel()
			require.Equal(t, tt.expected, actual)
		})
	}
}

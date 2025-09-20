package ddns

import (
	"testing"
	"time"

	"github.com/solidDoWant/infra-mk3/tooling/gateway-route-manager/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name          string
		config        config.Config
		expectedError string
		expectedType  string
	}{
		{
			name: "DDNS not enabled",
			config: config.Config{
				DDNSProvider: "",
			},
			expectedError: "DDNS is not enabled",
		},
		{
			name: "unsupported provider",
			config: config.Config{
				DDNSProvider: "unsupported",
				DDNSUsername: "testuser",
				DDNSPassword: "testpass",
				DDNSHostname: "test.example.com",
				Timeout:      5 * time.Second,
			},
			expectedError: "unsupported DDNS provider: unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.config)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, provider)
			} else {
				require.NoError(t, err)
				require.NotNil(t, provider)
				assert.Equal(t, tt.expectedType, provider.Name())
			}
		})
	}
}

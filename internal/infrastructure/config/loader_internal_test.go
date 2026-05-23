//nolint:testpackage // These tests exercise unexported config helper functions directly.
package config

import (
	"testing"
	"time"

	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyBitwardenFallbackSkipsWhenCredentialsOrConfigMissing(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "")
	t.Setenv("BITWARDEN_PROJECT_NAME", "")

	cfg := &SystemConfig{APIKey: "key", APISecret: "secret"}
	require.NoError(t, applyBitwardenFallback(cfg))
	assert.Equal(t, "key", cfg.APIKey)
	assert.Equal(t, "secret", cfg.APISecret)

	cfg = &SystemConfig{}
	require.NoError(t, applyBitwardenFallback(cfg))
	assert.Empty(t, cfg.APIKey)
	assert.Empty(t, cfg.APISecret)
}

func TestApplyBitwardenFallbackReturnsWrappedError(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "fake-token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "prod")

	cfg := &SystemConfig{}
	err := applyBitwardenFallback(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bitwarden fallback failed")
}

func TestValidateAndDefaultHelpers(t *testing.T) {
	t.Parallel()

	assert.Error(t, validateCredentials(&SystemConfig{}))
	assert.Error(t, validateCredentials(&SystemConfig{APIKey: "key"}))
	assert.NoError(t, validateCredentials(&SystemConfig{APIKey: "key", APISecret: "secret"}))

	assert.Error(t, validateEndpoints(&SystemConfig{}))
	assert.Error(t, validateEndpoints(&SystemConfig{
		API: APIConfig{Future: RESTConfig{BaseURL: "https://api.example.com"}},
	}))
	assert.NoError(t, validateEndpoints(&SystemConfig{
		API: APIConfig{
			Future:    RESTConfig{BaseURL: "https://api.example.com"},
			WebSocket: WebSocketConfig{WSURL: "wss://ws.example.com"},
		},
	}))

	cfg := &SystemConfig{}
	applySystemDefaults(cfg)
	assert.Equal(t, types.Duration(30*time.Second), cfg.Sync.Time)
	assert.Equal(t, types.Duration(30*time.Second), cfg.Sync.Ticker)
	assert.Equal(t, types.Duration(5*time.Minute), cfg.Sync.Contract)
	assert.Equal(t, 30, cfg.API.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestHasBitwardenConfig(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "project")

	assert.True(t, hasBitwardenConfig())
}

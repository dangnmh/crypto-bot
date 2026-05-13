package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-bot/internal/bots/funding/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSystemConfig_Success(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
		"exchange": "mexc",
		"sync": {
			"ticker": "5s",
			"contract": "30s",
			"funding": "10s"
		},
		"safety": {
			"maxCapitalPctPerSymbol": 10,
			"maxImpactRatio": 5,
			"maxLatency": "100ms",
			"bufferTime": "15ms",
			"holdDuration": "60s",
			"trapAfterSettle": "20ms"
		},
		"api": {
			"future": {
				"baseURL": "https://test.api.com"
			},
			"websocket": {
				"wsURL": "wss://test.example.com",
				"maxPairsPerWSConn": 25
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify safety defaults are applied (percentages normalized).
	assert.Equal(t, 0.10, cfg.Safety.MaxCapitalPctPerSymbol)
	assert.Equal(t, 0.05, cfg.Safety.MaxImpactRatio)
}

func TestLoadSystemConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := config.LoadSystemConfig("/nonexistent/path/system.json")
	assert.Error(t, err)
}

func TestLoadSystemConfig_InvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte(`{not valid`), 0o600))

	_, err := config.LoadSystemConfig(path)
	assert.Error(t, err)
}

func TestLoadSystemConfig_DefaultsApplied(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	// Minimal config — all safety fields at 0, sync fields at 0.
	content := `{
		"exchange": "mexc",
		"sync": {},
		"safety": {},
		"api": {
			"future": {
				"baseURL": "https://test.api.com"
			},
			"websocket": {
				"wsURL": "wss://test.example.com",
				"maxPairsPerWSConn": 25
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	// validate() should set defaults for zero-valued fields.
	assert.Greater(t, int64(cfg.Safety.BufferTime), int64(0), "BufferTime should be defaulted")
	assert.Greater(t, int64(cfg.Safety.HoldDuration), int64(0), "HoldDuration should be defaulted")
	assert.Greater(t, int64(cfg.Safety.TrapAfterSettle), int64(0), "TrapAfterSettle should be defaulted")
	assert.Greater(t, int64(cfg.Sync.FundingSync), int64(0), "FundingSync should be defaulted")
}

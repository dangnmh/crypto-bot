package config_test

import (
	"encoding/json"
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
		"sync": {
			"ticker": "5s",
			"contract": "30s",
			"funding": "10s"
		},
		"safety": {
			"maxImpactRatio": 5,
			"minVol24USD": 1000000,
			"maxLatency": "100ms",
			"bufferTime": "15ms",
			"holdDuration": "60s",
			"trapAfterSettle": "20ms"
		},
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {
					"baseURL": "https://test.api.com"
				},
				"websocket": {
					"wsURL": "wss://test.example.com",
					"maxPairsPerWSConn": 25
				}
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify safety values are loaded and percentages normalized.
	assert.Equal(t, 0.05, cfg.Safety.MaxImpactRatio)
	assert.Equal(t, 1000000.0, cfg.Safety.MinVol24USD)
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
		"sync": {},
		"safety": {},
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {
					"baseURL": "https://test.api.com"
				},
				"websocket": {
					"wsURL": "wss://test.example.com",
					"maxPairsPerWSConn": 25
				}
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	// validate() should set defaults for zero-valued fields.
	assert.Greater(t, int64(cfg.Sync.FundingSync), int64(0), "FundingSync should be defaulted")
}

func TestLoadSystemConfig_MergesSiblingStrategyDefaults(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
		"sync": {},
		"safety": {},
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {"baseURL": "https://test.api.com"},
				"websocket": {"wsURL": "wss://test.example.com", "maxPairsPerWSConn": 25}
			}
		},
		"tradingDefaults": {
			"leverage": 5,
			"openType": "ISOLATED",
			"positionMode": "HEDGE"
		}
	}`
	reversionContent := `{
		"enabled": true,
		"exchanges": {
			"mexc": {
				"takeProfitPct": 3,
				"stopLossPct": 2
			}
		}
	}`
	trapContent := `{
		"enabled": true,
		"depthPct": 2.5,
		"takeProfitPct": 1.5,
		"stopLossPct": 1.5,
		"trailing": {"enabled": true, "callbackPct": 0.5}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trap.jsonc"), []byte(trapContent), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	var defaults config.TradingDefaults
	require.NoError(t, json.Unmarshal(cfg.TradingDefaults, &defaults))

	assert.Equal(t, 5, defaults.Leverage)
	assert.True(t, defaults.FundingReversion.Enabled)
	assert.Equal(t, 3.0, defaults.FundingReversion.Exchanges["mexc"].TakeProfitPct)
	assert.True(t, defaults.FundingTrap.Enabled)
	assert.Equal(t, 2.5, defaults.FundingTrap.DepthPct)
	assert.True(t, defaults.FundingTrap.Trailing.Enabled)
}

func TestLoadSystemConfig_InvalidSiblingStrategyDefaults(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
		"sync": {},
		"safety": {},
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {"baseURL": "https://test.api.com"},
				"websocket": {"wsURL": "wss://test.example.com", "maxPairsPerWSConn": 25}
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`[]`), 0o600))

	_, err := config.LoadSystemConfig(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse strategy config")
}

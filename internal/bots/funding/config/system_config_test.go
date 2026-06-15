package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSystemConfig_Success(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
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
	reversionContent := `{
		"enabled": true,
		"sync": {
			"ticker": "5s",
			"contract": "30s",
			"funding": "10s"
		},
		"safety": {
			"maxImpactRatio": 5,
			"minVol24USD": 1000000
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))

	sysCfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)
	require.NotNil(t, sysCfg)

	fundingPath := filepath.Join(dir, "funding.json")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	fullCfg, err := config.Load(sysCfg, fundingPath)
	require.NoError(t, err)
	require.NotNil(t, fullCfg)

	// Verify safety values are loaded and percentages normalized.
	assert.Equal(t, 0.05, fullCfg.Reversion.Safety.MaxImpactRatio)
	assert.Equal(t, 1000000.0, fullCfg.Reversion.Safety.MinVol24USD)

	// Verify sync overrides are applied.
	assert.Equal(t, types.Duration(5*time.Second), fullCfg.Reversion.Sync.Ticker)
	assert.Equal(t, types.Duration(30*time.Second), fullCfg.Reversion.Sync.Contract)
	assert.Equal(t, types.Duration(10*time.Second), fullCfg.Reversion.Sync.FundingSync)
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

	// Minimal config.
	content := `{
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true}`), 0o600))

	sysCfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	// Load full config to assert strategy FundingSync is defaulted.
	fundingPath := filepath.Join(dir, "funding.json")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	fullCfg, err := config.Load(sysCfg, fundingPath)
	require.NoError(t, err)

	assert.Greater(t, int64(fullCfg.Reversion.Sync.Ticker), int64(0), "Ticker should be defaulted")
	assert.Greater(t, int64(fullCfg.Reversion.Sync.Time), int64(0), "Time should be defaulted")
	assert.Greater(t, int64(fullCfg.Reversion.Sync.FundingSync), int64(0), "FundingSync should be defaulted")
}

func TestLoadSystemConfig_InvalidBybitAccountType(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("BYBIT_API_KEY", "test-key")
	t.Setenv("BYBIT_API_SECRET", "test-secret")

	content := `{
		"sync": {},
		"safety": {},
		"exchange": {
			"bybit": {
				"enable": true,
				"future": {"baseURL": "https://api.bybit.com"},
				"websocket": {
					"publicURL": "wss://stream.bybit.com/v5/public/linear",
					"privateURL": "wss://stream.bybit.com/v5/private",
					"maxPairsPerWSConn": 30
				},
				"accountType": "classic"
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := config.LoadSystemConfig(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_config")
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
		}
	}`
	reversionContent := `{
		"enabled": true,
		"openType": "ISOLATED",
		"positionMode": "HEDGE",
		"default": {
			"leverage": 5
		},
		"exchanges": {
			"mexc": {
				"takeProfitPct": 3,
				"stopLossPct": 2
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))

	cfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	fundingContent := `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 50}]`
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(fundingContent), 0o600))

	fullCfg, err := config.Load(cfg, fundingPath)
	require.NoError(t, err)
	require.Len(t, fullCfg.Symbols, 1)

	sc := fullCfg.Symbols[0]
	assert.Equal(t, 5, sc.Leverage)
	assert.Equal(t, 1, sc.ParsedOpenType)     // ISOLATED
	assert.Equal(t, 1, sc.ParsedPositionMode) // HEDGE
	assert.True(t, sc.FundingReversion.Enabled)
	assert.Equal(t, 0.03, sc.FundingReversion.TakeProfitPct)
}

func TestLoadSystemConfig_InvalidSiblingStrategyDefaults(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
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

	sysCfg, err := config.LoadSystemConfig(path)
	require.NoError(t, err)

	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	_, err = config.Load(sysCfg, fundingPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse reversion config")
}

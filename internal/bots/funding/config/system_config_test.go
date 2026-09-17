package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	_ = os.Setenv("TEST_API_KEY", "mock-key")
	_ = os.Setenv("TEST_API_SECRET", "mock-secret")
}

func createTestAccountsManifest(t *testing.T, dir, exch, revPath, obfPath, dilPath, fundingPath string) string {
	t.Helper()
	if exch == "" {
		exch = "mexc_futures"
	}
	manifestJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": %q,
				"exchange": %q,
				"enabled": true,
				"scanners": {
					"configured": true,
					"schedule": true
				},
				"env": {
					"apiKey": "TEST_API_KEY",
					"apiSecret": "TEST_API_SECRET"
				},
				"configs": {
					"reversion": %q,
					"obfuscator": %q,
					"dilution": %q,
					"funding": %q
				}
			}
		]
	}`, exch+"_main", exch, revPath, obfPath, dilPath, fundingPath)
	manifestPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestJSON), 0o600))
	return manifestPath
}

func TestLoadSystemConfig_Success(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{}`
	exchContent := `{
		"mexc_futures": {
			"enable": true,
			"accountType": "futures",
			"baseURL": "https://test.api.com",
			"websocket": {
				"publicURL": "wss://test.example.com",
				"privateURL": "wss://test.example.com",
				"maxPairsPerWSConn": 25
			}
		}
	}`

	reversionContent := `{
		"enabled": true,
		"default": {
			"minVol24USD": 1000000,
			"bufferTime": "0ms"
		},
		"sync": {
			"ticker": "5s",
			"contract": "30s",
			"funding": "10s"
		},
		"safety": {
			"maxImpactRatio": 5
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dilution.jsonc"), []byte(defaultDilutionJSON), 0o600))

	sysCfg, err := config.LoadSystemConfig(path, exchPath)
	require.NoError(t, err)
	require.NotNil(t, sysCfg)

	fundingPath := filepath.Join(dir, "funding.json")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	manifestPath := createTestAccountsManifest(t, dir, "mexc_futures", filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"), filepath.Join(dir, "dilution.jsonc"), fundingPath)
	fullCfg, err := config.Load(sysCfg, manifestPath, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"))
	require.NoError(t, err)
	require.NotNil(t, fullCfg)

	// Verify safety values are loaded and percentages normalized.
	rev, err := fullCfg.ReversionForAccount("mexc_futures_main")
	require.NoError(t, err)
	require.NotNil(t, rev)
	assert.Equal(t, 0.05, rev.Safety.MaxImpactRatio)
	assert.Equal(t, 1000000.0, rev.Default.MinVol24USD)

	// Verify sync overrides are applied.
	assert.Equal(t, types.Duration(5*time.Second), rev.Sync.Ticker)
	assert.Equal(t, types.Duration(30*time.Second), rev.Sync.Contract)
	assert.Equal(t, types.Duration(10*time.Second), rev.Sync.FundingSync)
}

func TestLoadSystemConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := config.LoadSystemConfig("/nonexistent/path/system.json", "/nonexistent/path/exchange.json")
	assert.Error(t, err)
}

func TestLoadSystemConfig_InvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(`{not valid`), 0o600))

	_, err := config.LoadSystemConfig(path, exchPath)
	assert.Error(t, err)
}

func TestLoadSystemConfig_DefaultsApplied(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	// Minimal config.
	content := `{}`
	exchContent := `{
		"bybit_futures": {
			"enable": true,
			"accountType": "unified",
			"baseURL": "https://test.api.com",
			"websocket": {
				"publicURL": "wss://test.example.com",
				"privateURL": "wss://test.example.com",
				"maxPairsPerWSConn": 25
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dilution.jsonc"), []byte(defaultDilutionJSON), 0o600))

	sysCfg, err := config.LoadSystemConfig(path, exchPath)
	require.NoError(t, err)

	// Load full config to assert strategy FundingSync is defaulted.
	fundingPath := filepath.Join(dir, "funding.json")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	manifestPath := createTestAccountsManifest(t, dir, "bybit_futures", filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"), filepath.Join(dir, "dilution.jsonc"), fundingPath)
	fullCfg, err := config.Load(sysCfg, manifestPath, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"))
	require.NoError(t, err)

	rev, err := fullCfg.ReversionForAccount("bybit_futures_main")
	require.NoError(t, err)
	require.NotNil(t, rev)
	assert.Greater(t, int64(rev.Sync.Ticker), int64(0), "Ticker should be defaulted")
	assert.Greater(t, int64(rev.Sync.Time), int64(0), "Time should be defaulted")
	assert.Greater(t, int64(rev.Sync.FundingSync), int64(0), "FundingSync should be defaulted")
}

func TestLoadSystemConfig_InvalidBybitAccountType(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("BYBIT_API_KEY", "test-key")
	t.Setenv("BYBIT_API_SECRET", "test-secret")

	content := `{
		"sync": {},
		"safety": {}
	}`
	exchContent := `{
		"bybit_futures": {
			"enable": true,
			"baseURL": "https://api.bybit.com",
			"accountType": "classic",
			"websocket": {
				"publicURL": "wss://stream.bybit.com/v5/public/linear",
				"privateURL": "wss://stream.bybit.com/v5/private",
				"maxPairsPerWSConn": 30
			}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	exChangePath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(exChangePath, []byte(exchContent), 0o600))

	_, err := config.LoadSystemConfig(path, exChangePath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported account type")
}

func TestLoadSystemConfig_MergesSiblingStrategyDefaults(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	content := `{
		"sync": {},
		"safety": {}
	}`
	exchContent := `{
		"mexc_futures": {
			"enable": true,
			"baseURL": "https://test.api.com",
			"websocket": {"publicURL": "wss://test.example.com", "privateURL": "wss://test.example.com", "maxPairsPerWSConn": 25}
		}
	}`
	reversionContent := `{
		"enabled": true,
		"openType": "ISOLATED",
		"positionMode": "HEDGE",
		"default": {
			"leverage": 5
		},
		"takeProfitPct": 3,
		"stopLossPct": 2
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dilution.jsonc"), []byte(defaultDilutionJSON), 0o600))

	cfg, err := config.LoadSystemConfig(path, exchPath)
	require.NoError(t, err)

	fundingContent := `[{"symbol": "BTC_USDT", "exchange": "mexc_futures", "marginUSDT": 50}]`
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(fundingContent), 0o600))

	manifestPath := createTestAccountsManifest(t, dir, "mexc_futures", filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"), filepath.Join(dir, "dilution.jsonc"), fundingPath)
	fullCfg, err := config.Load(cfg, manifestPath, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"))
	require.NoError(t, err)
	symbols := fullCfg.AllSymbols()
	require.Len(t, symbols, 1)

	sc := symbols[0]
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

	content := `{}`
	exchContent := `{
		"mexc_futures": {
			"enable": true,
			"baseURL": "https://test.api.com",
			"websocket": {"publicURL": "wss://test.example.com", "privateURL": "wss://test.example.com", "maxPairsPerWSConn": 25}
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "system.jsonc")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`[]`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dilution.jsonc"), []byte(defaultDilutionJSON), 0o600))

	sysCfg, err := config.LoadSystemConfig(path, exchPath)
	require.NoError(t, err)

	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	manifestPath := createTestAccountsManifest(t, dir, "mexc_futures", filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"), filepath.Join(dir, "dilution.jsonc"), fundingPath)
	_, err = config.Load(sysCfg, manifestPath, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse reversion config")
}

func TestLoadSystemConfig_WithExplicitExchange(t *testing.T) {
	// Cannot run parallel: sets env vars.
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	sysDir := t.TempDir()
	exchDir := t.TempDir()

	sysPath := filepath.Join(sysDir, "system.jsonc")
	exchPath := filepath.Join(exchDir, "custom_exchange.jsonc")

	sysContent := `{}`
	exchContent := `{
		"mexc_futures": {
			"enable": true,
			"baseURL": "https://test.api.com",
			"websocket": {"publicURL": "wss://test.example.com", "privateURL": "wss://test.example.com", "maxPairsPerWSConn": 25}
		}
	}`

	require.NoError(t, os.WriteFile(sysPath, []byte(sysContent), 0o600))
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))

	cfg, err := config.LoadSystemConfig(sysPath, exchPath)
	require.NoError(t, err)
	require.True(t, cfg.ExchangeConfig["mexc_futures"].IsEnabled())
}

func TestLoadSystemConfig_InvalidTradeSide(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	dir := t.TempDir()
	path := filepath.Join(dir, "system.json")
	exchPath := filepath.Join(dir, "exchange.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	exchContent := `{
		"mexc_futures": {
			"enable": true,
			"baseURL": "https://test.api.com",
			"websocket": {
				"publicURL": "wss://test.example.com",
				"privateURL": "wss://test.example.com",
				"maxPairsPerWSConn": 25
			}
		}
	}`
	require.NoError(t, os.WriteFile(exchPath, []byte(exchContent), 0o600))

	// tradeSide is "invalid_value"
	reversionContent := `{
		"enabled": true,
		"tradeSide": "invalid_value"
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(reversionContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dilution.jsonc"), []byte(defaultDilutionJSON), 0o600))

	sysCfg, err := config.LoadSystemConfig(path, exchPath)
	require.NoError(t, err)

	fundingPath := filepath.Join(dir, "funding.json")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	manifestPath := createTestAccountsManifest(t, dir, "mexc_futures", filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"), filepath.Join(dir, "dilution.jsonc"), fundingPath)
	_, err = config.Load(sysCfg, manifestPath, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tradeSide")
}

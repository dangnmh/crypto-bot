package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	_ = os.Setenv("TEST_API_KEY", "mock-key")
	_ = os.Setenv("TEST_API_SECRET", "mock-secret")
}

// ──────────────────────────────────────────────────────────────────────
// Helper: creates a temp funding.json and loads it with the given system config.
const (
	defaultObfuscatorJSON = `{"enabled": false, "pollInterval": "1m", "lookbackWindow": "24h"}`
	defaultDilutionJSON   = `{"enabled": false, "pollInterval": "5s"}`
)

type testDefaults struct {
	config.RawFundingReversionConfig
	Safety config.SafetyConfig
}

var (
	testReversionDefaultsMu sync.RWMutex
	testReversionDefaults   = make(map[*config.SystemConfig]testDefaults)
)

func setTestDefaults(sc *config.SystemConfig, defaults testDefaults) {
	testReversionDefaultsMu.Lock()
	defer testReversionDefaultsMu.Unlock()
	testReversionDefaults[sc] = defaults
}

func getTestDefaults(sc *config.SystemConfig) (testDefaults, bool) {
	testReversionDefaultsMu.RLock()
	defer testReversionDefaultsMu.RUnlock()
	d, ok := testReversionDefaults[sc]
	return d, ok
}

func loadWith(t *testing.T, sysCfg *config.SystemConfig, fundingJSON string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(fundingJSON), 0o600))

	defaults, ok := getTestDefaults(sysCfg)
	if !ok {
		defaults = testDefaults{Enabled: true}
	}

	mockRev := struct {
		config.RawFundingReversionConfig
		Safety   config.SafetyConfig            `json:"safety"`
		Notifier config.ReversionNotifierConfig `json:"notifier"`
		Sync     config.SyncConfig              `json:"sync"`
	}{
		RawFundingReversionConfig: defaults.RawFundingReversionConfig,
		Safety:                    defaults.Safety,
		Sync: config.SyncConfig{
			SyncConfig: sysconfig.SyncConfig{
				Ticker:   types.Duration(time.Second),
				Contract: types.Duration(time.Second),
				Time:     types.Duration(time.Second),
			},
			FundingSync: types.Duration(time.Second),
		},
	}
	revData, err := json.Marshal(mockRev)
	require.NoError(t, err)

	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, revData, 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	obfPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, revPath, obfPath, dilPath, path)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysCfg, accountsPath, blacklistPath, revPath)
	require.NoError(t, err)
	return cfg
}

func loadWithError(t *testing.T, sysCfg *config.SystemConfig, fundingJSON string) error {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(fundingJSON), 0o600))
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, []byte(`{"enabled": true, "default": {"bufferTime": "0ms"}}`), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	obfPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, revPath, obfPath, dilPath, path)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	_, err := config.Load(sysCfg, accountsPath, blacklistPath, revPath)
	return err
}

func sysWithDefaults(defaults testDefaults) *config.SystemConfig {
	sc := &config.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc": sysconfig.EndpointConfig{
				Enable:    true,
				BaseURL:   "https://mexc.test",
				WebSocket: sysconfig.WebSocketConfig{PublicURL: "wss://mexc.test", PrivateURL: "wss://mexc.test"},
				APIKey:    "mock-key",
				APISecret: "mock-secret",
			},
		},
	}
	setTestDefaults(sc, defaults)
	return sc
}

func sysWithMexc() *config.SystemConfig {
	return sysWithDefaults(testDefaults{})
}

// ──────────────────────────────────────────────────────────────────────
// config.Load — error cases
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithMexc(),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20}]`)

	symbols := cfg.AllSymbols()
	require.Len(t, symbols, 1)
	assert.Equal(t, "BTC_USDT", symbols[0].Symbol)
	assert.Equal(t, float64(100), symbols[0].MarginUSDT)
	assert.Equal(t, 20, symbols[0].Leverage)
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, []byte(`{"enabled": true}`), 0o600))
	_, err := config.Load(&config.SystemConfig{}, filepath.Join(dir, "nonexistent.json"), blacklistPath, revPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read config")
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, []byte(`{"enabled": true}`), 0o600))
	_, err := config.Load(sysWithMexc(), path, blacklistPath, revPath)
	assert.Error(t, err)
}

func TestLoad_MissingPathsValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.jsonc")
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	revPath := filepath.Join(dir, "reversion.jsonc")

	// Missing accountsPath
	_, err := config.Load(sysWithMexc(), "", blacklistPath, revPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccountsPath")

	// Missing blacklistPath
	_, err = config.Load(sysWithMexc(), path, "", revPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BlacklistPath")

	// Missing commonReversionPath
	_, err = config.Load(sysWithMexc(), path, blacklistPath, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CommonReversionPath")
}

func TestLoad_EmptySymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0o600))

	sysCfg := sysWithDefaults(testDefaults{
		Enabled: true,
		Default: config.ExchangeReversionConfig{
			MarginUSD: 100,
		},
	})

	// Manually write the reversion.jsonc because we are calling Load directly here
	defaults, _ := getTestDefaults(sysCfg)
	mockRev := struct {
		config.RawFundingReversionConfig
		Sync config.SyncConfig `json:"sync"`
	}{
		RawFundingReversionConfig: defaults.RawFundingReversionConfig,
		Sync: config.SyncConfig{
			SyncConfig: sysconfig.SyncConfig{
				Ticker:   types.Duration(time.Second),
				Contract: types.Duration(time.Second),
				Time:     types.Duration(time.Second),
			},
			FundingSync: types.Duration(time.Second),
		},
	}
	revData, err := json.Marshal(mockRev)
	require.NoError(t, err)
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, revData, 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	obfPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
				"enabled": true,
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
	}`, revPath, obfPath, dilPath, path)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysCfg, accountsPath, blacklistPath, revPath)
	require.NoError(t, err)
	assert.Empty(t, cfg.AllSymbols())

	// Verify that NewAccountSymbolConfig works correctly on this config with empty symbols
	symCfg, err := cfg.NewAccountSymbolConfig("mexc_main", "mexc", "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, "BTC_USDT", symCfg.Symbol)
	assert.Equal(t, float64(100), symCfg.MarginUSDT)
}

func TestLoad_MissingSymbolName(t *testing.T) {
	t.Parallel()
	err := loadWithError(t, sysWithMexc(), `[{"exchange": "mexc", "marginUSDT": 100, "leverage": 20}]`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'symbol' failed on the 'required' tag")
}

func TestLoad_InvalidMargin(t *testing.T) {
	t.Parallel()
	err := loadWithError(t, sysWithMexc(), `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 0, "leverage": 20}]`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'marginUSDT' failed on the 'gt' tag")
}

func TestLoad_InvalidLeverage(t *testing.T) {
	t.Parallel()
	err := loadWithError(t, sysWithMexc(), `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 0}]`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "'leverage' failed on the 'gte' tag")
}

func TestLoad_InvalidExchange(t *testing.T) {
	t.Parallel()
	err := loadWithError(t, sysWithMexc(), `[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 2, "exchange": "binance"}]`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `exchange "binance" is not configured`)
}

// ──────────────────────────────────────────────────────────────────────
// Defaults — verified through Load()
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_AppliesDefaults(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(testDefaults{
		Enabled:      true,
		OpenType:     "ISOLATED",
		PositionMode: "HEDGE",
		Default: config.ExchangeReversionConfig{
			Leverage: 10,
		},
		Exchanges: map[string]config.ExchangeReversionConfig{
			"mexc": {
				TakeProfitPct:  15,
				StopLossPct:    3,
				BufferTime:     types.Duration(10 * time.Millisecond),
				MinFundingRate: 0.5,
			},
		},
		Safety: config.SafetyConfig{
			MaxPriceDiffPercent: 0.2,
			MaxLatency:          types.Duration(200 * time.Millisecond),
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100}]`)
	sc := cfg.AllSymbols()[0]

	// Verify defaults were applied (note: values are normalized to ratios by Load).
	assert.Equal(t, 10, sc.Leverage)
	assert.Equal(t, config.OpenType("ISOLATED"), sc.OpenType)
	assert.Equal(t, config.PositionMode("HEDGE"), sc.PositionMode)
	assert.InDelta(t, 0.005, sc.MinFundingRate, 1e-9, "0.5% -> 0.005")
	assert.InDelta(t, 0.2, sc.MaxPriceDiffPercent, 1e-9, "maxPriceDiffPercent remains percent for slippage math")
	assert.InDelta(t, 0.15, sc.FundingReversion.TakeProfitPct, 1e-9, "15% -> 0.15")
	assert.InDelta(t, 0.03, sc.FundingReversion.StopLossPct, 1e-9, "3% -> 0.03")
	assert.Equal(t, types.Duration(200*time.Millisecond), sc.FundingReversion.MaxLatency)
	assert.Equal(t, types.Duration(10*time.Millisecond), sc.FundingReversion.BufferTime)
}

func TestLoad_DefaultsDoNotOverrideExisting(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(testDefaults{
		Default: config.ExchangeReversionConfig{
			Leverage:       10,
			MinFundingRate: 0.5,
		},
		Safety: config.SafetyConfig{},
	})

	cfg := loadWith(t, sysCfg,
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20, "minFundingRate": 1.0}]`)
	sc := cfg.AllSymbols()[0]

	assert.Equal(t, 20, sc.Leverage, "per-symbol value should win")
	assert.InDelta(t, 0.01, sc.MinFundingRate, 1e-9, "per-symbol 1.0% -> 0.01")
}

func TestLoad_DynamicTPSettings(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(testDefaults{
		Enabled: true,
		Default: config.ExchangeReversionConfig{
			Leverage:      10,
			TakeProfitPct: 1,
			StopLossPct:   2,
			DynamicTP: config.DynamicTPConfig{
				Enabled:          true,
				TPMultiplier:     2.0,
				MinTakeProfitPct: 1.0,
				MaxTakeProfitPct: 7.0,
			},
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100}]`)
	sc := cfg.AllSymbols()[0]

	assert.True(t, sc.FundingReversion.DynamicTP.Enabled)
	assert.InDelta(t, 2.0, sc.FundingReversion.DynamicTP.TPMultiplier, 1e-9)
	assert.InDelta(t, 0.01, sc.FundingReversion.DynamicTP.MinTakeProfitPct, 1e-9, "1.0% -> 0.01")
	assert.InDelta(t, 0.07, sc.FundingReversion.DynamicTP.MaxTakeProfitPct, 1e-9, "7.0% -> 0.07")
}

func TestLoad_PnLTrailingSettings(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(testDefaults{
		Enabled: true,
		Default: config.ExchangeReversionConfig{
			Leverage:      10,
			TakeProfitPct: 1,
			StopLossPct:   2,
			PnLTrailing: config.PnLTrailingConfig{
				Enabled:      true,
				DropPct:      10.0,
				ConfirmTicks: 2,
			},
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100}]`)
	sc := cfg.AllSymbols()[0]

	assert.True(t, sc.FundingReversion.PnLTrailing.Enabled)
	assert.InDelta(t, 10.0, sc.FundingReversion.PnLTrailing.DropPct, 1e-9)
	assert.Equal(t, 2, sc.FundingReversion.PnLTrailing.ConfirmTicks)
}

func TestLoad_ValidTradeSide(t *testing.T) {
	t.Parallel()
	sysCfg := sysWithDefaults(testDefaults{
		Enabled:   true,
		TradeSide: " LONG ",
	})
	cfg := loadWith(t, sysCfg, `[]`)
	rev, err := cfg.ReversionForAccount("mexc_main")
	require.NoError(t, err)
	assert.Equal(t, "long", rev.TradeSide)
}

// ──────────────────────────────────────────────────────────────────────
// Normalization — percentages to ratios, defaults for zero values
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_NormalizesPercentages(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithMexc(),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 5,
		   "minFundingRate": 0.3, "maxPriceDiffPercent": 0.8,
		   "fundingReversion": {"enabled": true, "takeProfitPct": 20, "stopLossPct": 5}}]`)
	sc := cfg.AllSymbols()[0]

	assert.InDelta(t, 0.003, sc.MinFundingRate, 1e-9, "0.3% -> 0.003")
	assert.InDelta(t, 0.8, sc.MaxPriceDiffPercent, 1e-9, "maxPriceDiffPercent remains percent for slippage math")
	assert.InDelta(t, 0.20, sc.FundingReversion.TakeProfitPct, 1e-9, "20% -> 0.20")
	assert.InDelta(t, 0.05, sc.FundingReversion.StopLossPct, 1e-9, "5% -> 0.05")
}

func TestLoad_DefaultTPSL_WhenZero(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithDefaults(testDefaults{Enabled: true}),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 5}]`)
	sc := cfg.AllSymbols()[0]

	assert.InDelta(t, 0.20, sc.FundingReversion.TakeProfitPct, 1e-9, "default TP 20% -> 0.20")
	assert.InDelta(t, 0.05, sc.FundingReversion.StopLossPct, 1e-9, "default SL 5% -> 0.05")
}

func TestLoad_AcceptsDecimalPercentRatiosAtConfigBoundary(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithMexc(),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 5,
		   "minFundingRate": 0.003,
		   "fundingReversion": {"enabled": true, "takeProfitPct": 0.03, "stopLossPct": 0.02}}]`)
	sc := cfg.AllSymbols()[0]

	assert.InDelta(t, 0.003, sc.MinFundingRate, 1e-9, "decimal funding threshold preserved")
	assert.InDelta(t, 0.03, sc.FundingReversion.TakeProfitPct, 1e-9, "decimal TP ratio preserved")
	assert.InDelta(t, 0.02, sc.FundingReversion.StopLossPct, 1e-9, "decimal SL ratio preserved")
}

// ──────────────────────────────────────────────────────────────────────
// Symbol modes — ISOLATED/CROSS, HEDGE/ONE_WAY
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_ParsesSymbolModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		json         string
		wantOpen     int
		wantPosition int
	}{
		{"ISOLATED/HEDGE", `[{"symbol":"S","exchange":"mexc","marginUSDT":1,"leverage":1,"openType":"ISOLATED","positionMode":"HEDGE"}]`, 1, 1},
		{"CROSS/ONE_WAY", `[{"symbol":"S","exchange":"mexc","marginUSDT":1,"leverage":1,"openType":"CROSS","positionMode":"ONE_WAY"}]`, 2, 2},
		{"empty defaults to ISOLATED/HEDGE", `[{"symbol":"S","exchange":"mexc","marginUSDT":1,"leverage":1}]`, 1, 1},
		{"lowercase", `[{"symbol":"S","exchange":"mexc","marginUSDT":1,"leverage":1,"openType":"isolated","positionMode":"hedge"}]`, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := loadWith(t, sysWithMexc(), tt.json)
			sc := cfg.AllSymbols()[0]
			assert.Equal(t, tt.wantOpen, sc.ParsedOpenType)
			assert.Equal(t, tt.wantPosition, sc.ParsedPositionMode)
		})
	}
}

// ──────────────────────────────────────────────────────────────────────
// config.Load with TradingDefaults from system config
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_WithTradingDefaults(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(testDefaults{
		Default: config.ExchangeReversionConfig{
			Leverage:       10,
			MinFundingRate: 0.3,
		},
		Safety: config.SafetyConfig{
			MaxPriceDiffPercent: 0.1,
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 50}]`)
	sc := cfg.AllSymbols()[0]
	assert.Equal(t, 10, sc.Leverage, "should inherit from defaults")
}

func TestLoad_WithBlacklist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create funding.jsonc
	fundingPath := filepath.Join(dir, "funding.jsonc")
	fundingContent := `[
		{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20},
		{"symbol": "ETH_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20}
	]`
	require.NoError(t, os.WriteFile(fundingPath, []byte(fundingContent), 0o600))

	// Create blacklist.jsonc
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	blacklistContent := `{
		"common": ["ETH_USDT"]
	}`
	require.NoError(t, os.WriteFile(blacklistPath, []byte(blacklistContent), 0o600))

	// Create reversion.jsonc
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, []byte(`{"enabled": true, "default": {"bufferTime": "0ms"}}`), 0o600))
	obfPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, revPath, obfPath, dilPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysWithMexc(), accountsPath, blacklistPath, revPath)
	require.NoError(t, err)

	assert.NotNil(t, cfg.Blacklist)
	assert.True(t, cfg.Blacklist.IsBlacklisted("mexc", "ETH_USDT"))
	assert.False(t, cfg.Blacklist.IsBlacklisted("mexc", "BTC_USDT"))
}

func TestLoad_WithObfuscator(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20}]`), 0o600))

	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte(`{}`), 0o600))

	reversionPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true, "default": {"bufferTime": "0ms"}}`), 0o600))

	dilutionPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilutionPath, []byte(defaultDilutionJSON), 0o600))

	obfuscatorPath := filepath.Join(dir, "obfuscator.jsonc")
	obfuscatorContent := `{
		"enabled": true,
		"pollInterval": "1m",
		"jitter": "15s",
		"lookbackWindow": "24h",
		"netPnLThresholdUSDT": 5.0,
		"minNotionalUSD": 10.0,
		"maxNotionalUSD": 500.0,
		"marginUSDT": 10.0,
		"leverage": 5,
		"takeProfitPct": 0.5,
		"stopLossPct": 0.5,
		"minHoldSec": 10,
		"maxHoldSec": 60,
		"maxActiveOrders": 1,
		"sacrificeLossPct": 50.0,
		"maxDailyLossUSD": 200.0
	}`
	require.NoError(t, os.WriteFile(obfuscatorPath, []byte(obfuscatorContent), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, reversionPath, obfuscatorPath, dilutionPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysWithMexc(), accountsPath, blacklistPath, reversionPath)
	require.NoError(t, err)
	obf, err := cfg.ObfuscatorForAccount("mexc_main")
	require.NoError(t, err)
	require.NotNil(t, obf)
	assert.True(t, obf.Enabled)
	assert.Equal(t, types.Duration(1*time.Minute), obf.PollInterval)
	assert.Equal(t, types.Duration(15*time.Second), obf.Jitter)
	assert.Equal(t, types.Duration(24*time.Hour), obf.LookbackWindow)
	assert.Equal(t, 5, obf.Leverage)
	assert.Equal(t, 50.0, obf.SacrificeLossPct)
	assert.Equal(t, 200.0, obf.MaxDailyLossUSD)
}

func TestLoad_WithDilution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20}]`), 0o600))

	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte(`{}`), 0o600))

	reversionPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true, "default": {"bufferTime": "0ms"}}`), 0o600))

	obfuscatorPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfuscatorPath, []byte(defaultObfuscatorJSON), 0o600))

	dilutionPath := filepath.Join(dir, "dilution.jsonc")
	dilutionContent := `{
		"enabled": true,
		"pollInterval": "10s",
		"jitter": "2s",
		"symbol": "BTC_USDT",
		"maxPositionUSD": 1000,
		"leverage": 20,
		"marginUSD": 25,
		"unfilledCancelTimeout": "1m",
		"positionCloseTimeout": "3m",
		"spreadOffsetTicks": 0
	}`
	require.NoError(t, os.WriteFile(dilutionPath, []byte(dilutionContent), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, reversionPath, obfuscatorPath, dilutionPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysWithMexc(), accountsPath, blacklistPath, reversionPath)
	require.NoError(t, err)
	dil, err := cfg.DilutionForAccount("mexc_main")
	require.NoError(t, err)
	require.NotNil(t, dil)
	assert.True(t, dil.Enabled)
	assert.Equal(t, types.Duration(10*time.Second), dil.PollInterval)
	assert.Equal(t, types.Duration(2*time.Second), dil.Jitter)
	assert.Equal(t, 500.0, dil.OrderNotionalUSD())
}

func TestLoad_WithDilution_CappedMaxPositionUSD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20}]`), 0o600))

	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte(`{}`), 0o600))

	reversionPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true, "default": {"bufferTime": "0ms"}}`), 0o600))

	obfuscatorPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfuscatorPath, []byte(defaultObfuscatorJSON), 0o600))

	dilutionPath := filepath.Join(dir, "dilution.jsonc")
	// MaxPositionUSD (400) is less than MarginUSD (25) * Leverage (20) = 500 -> capped at 400
	dilutionContent := `{
		"enabled": true,
		"pollInterval": "10s",
		"symbol": "BTC_USDT",
		"maxPositionUSD": 400,
		"leverage": 20,
		"marginUSD": 25,
		"unfilledCancelTimeout": "1m",
		"positionCloseTimeout": "3m",
		"spreadOffsetTicks": 0
	}`
	require.NoError(t, os.WriteFile(dilutionPath, []byte(dilutionContent), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
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
	}`, reversionPath, obfuscatorPath, dilutionPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	cfg, err := config.Load(sysWithMexc(), accountsPath, blacklistPath, reversionPath)
	require.NoError(t, err)
	dil, err := cfg.DilutionForAccount("mexc_main")
	require.NoError(t, err)
	require.NotNil(t, dil)
	assert.Equal(t, 400.0, dil.OrderNotionalUSD())
}

func TestExchangeObfuscationCfg_OrderNotionalUSD(t *testing.T) {
	t.Parallel()

	t.Run("calculates margin * leverage within bounds", func(t *testing.T) {
		t.Parallel()
		cfg := config.ExchangeObfuscationCfg{
			MarginUSDT:     200.0,
			Leverage:       10,
			MinNotionalUSD: 100.0,
			MaxNotionalUSD: 3000.0,
		}
		assert.Equal(t, 2000.0, cfg.OrderNotionalUSD())
	})

	t.Run("clamps to minNotionalUSD", func(t *testing.T) {
		t.Parallel()
		cfg := config.ExchangeObfuscationCfg{
			MarginUSDT:     5.0,
			Leverage:       2,
			MinNotionalUSD: 50.0,
			MaxNotionalUSD: 500.0,
		}
		assert.Equal(t, 50.0, cfg.OrderNotionalUSD())
	})

	t.Run("clamps to maxNotionalUSD", func(t *testing.T) {
		t.Parallel()
		cfg := config.ExchangeObfuscationCfg{
			MarginUSDT:     500.0,
			Leverage:       10,
			MinNotionalUSD: 50.0,
			MaxNotionalUSD: 1000.0,
		}
		assert.Equal(t, 1000.0, cfg.OrderNotionalUSD())
	})

	t.Run("preserves configured MaxPriceDiffPercent", func(t *testing.T) {
		t.Parallel()
		cfg := config.ExchangeObfuscationCfg{
			MarginUSDT:          100.0,
			Leverage:            5,
			MinNotionalUSD:      50.0,
			MaxNotionalUSD:      1000.0,
			MaxPriceDiffPercent: 0.8,
		}
		assert.InDelta(t, 0.8, cfg.MaxPriceDiffPercent, 1e-9)
	})
}

func TestMergeExchangeReversionConfig(t *testing.T) {
	t.Parallel()

	dest := config.ExchangeReversionConfig{
		TakeProfitPct: 1.0,
		StopLossPct:   2.0,
		Leverage:      5,
		MarginUSD:     100,
		PnLTrailing: config.PnLTrailingConfig{
			Enabled:      false,
			DropPct:      5.0,
			ConfirmTicks: 1,
		},
		DynamicTP: config.DynamicTPConfig{
			Enabled:          false,
			TPMultiplier:     1.5,
			MinTakeProfitPct: 0.5,
			MaxTakeProfitPct: 5.0,
		},
	}

	src := config.ExchangeReversionConfig{
		TakeProfitPct: 2.5,
		PnLTrailing: config.PnLTrailingConfig{
			Enabled:      true,
			DropPct:      10.0,
			ConfirmTicks: 3,
		},
		DynamicTP: config.DynamicTPConfig{
			Enabled:          true,
			TPMultiplier:     2.0,
			MinTakeProfitPct: 1.0,
			MaxTakeProfitPct: 8.0,
		},
	}

	config.MergeExchangeReversionConfig(&dest, src)

	assert.InDelta(t, 2.5, dest.TakeProfitPct, 1e-9)
	assert.InDelta(t, 2.0, dest.StopLossPct, 1e-9) // unmerged retains original
	assert.Equal(t, 5, dest.Leverage)              // unmerged retains original
	assert.True(t, dest.PnLTrailing.Enabled)
	assert.InDelta(t, 10.0, dest.PnLTrailing.DropPct, 1e-9)
	assert.Equal(t, 3, dest.PnLTrailing.ConfirmTicks)
	assert.True(t, dest.DynamicTP.Enabled)
	assert.InDelta(t, 2.0, dest.DynamicTP.TPMultiplier, 1e-9)
	assert.InDelta(t, 1.0, dest.DynamicTP.MinTakeProfitPct, 1e-9)
	assert.InDelta(t, 8.0, dest.DynamicTP.MaxTakeProfitPct, 1e-9)
}

func TestExchangeReversionConfig_UnmarshalBufferTime(t *testing.T) {
	t.Parallel()

	// 1. Positive bufferTime (150ms)
	{
		var cfg config.ExchangeReversionConfig
		err := json.Unmarshal([]byte(`{"bufferTime": "150ms"}`), &cfg)
		require.NoError(t, err)
		assert.Equal(t, types.Duration(150*time.Millisecond), cfg.BufferTime)
	}

	// 2. Negative bufferTime (-390ms)
	{
		var cfg config.ExchangeReversionConfig
		err := json.Unmarshal([]byte(`{"bufferTime": "-390ms"}`), &cfg)
		require.NoError(t, err)
		assert.Equal(t, types.Duration(-390*time.Millisecond), cfg.BufferTime)
	}

	// 3. Zero bufferTime (0ms)
	{
		var cfg config.ExchangeReversionConfig
		err := json.Unmarshal([]byte(`{"bufferTime": "0ms"}`), &cfg)
		require.NoError(t, err)
		assert.Equal(t, types.Duration(0), cfg.BufferTime)
	}
}

func TestFundingReversionConfig_UnmarshalBufferTime(t *testing.T) {
	t.Parallel()

	// 1. Positive bufferTime (150ms)
	{
		var cfg domain.FundingReversionConfig
		err := json.Unmarshal([]byte(`{"bufferTime": "150ms"}`), &cfg)
		require.NoError(t, err)
		assert.Equal(t, types.Duration(150*time.Millisecond), cfg.BufferTime)
	}

	// 2. Negative bufferTime (-390ms)
	{
		var cfg domain.FundingReversionConfig
		err := json.Unmarshal([]byte(`{"bufferTime": "-390ms"}`), &cfg)
		require.NoError(t, err)
		assert.Equal(t, types.Duration(-390*time.Millisecond), cfg.BufferTime)
	}
}

func TestMergeExchangeReversionConfig_BufferTime(t *testing.T) {
	t.Parallel()

	dest := config.ExchangeReversionConfig{
		BufferTime: types.Duration(150 * time.Millisecond),
	}
	src := config.ExchangeReversionConfig{
		BufferTime: types.Duration(-390 * time.Millisecond),
	}
	config.MergeExchangeReversionConfig(&dest, src)
	assert.Equal(t, types.Duration(-390*time.Millisecond), dest.BufferTime)
}

func TestLoadMultiAccount(t *testing.T) {
	t.Setenv("ACC1_KEY", "key_mexc_main")
	t.Setenv("ACC1_SECRET", "secret_mexc_main")
	t.Setenv("ACC2_KEY", "key_mexc_sub1")
	t.Setenv("ACC2_SECRET", "secret_mexc_sub1")

	dir := t.TempDir()

	// 1. Account 1 sub-configs
	acc1Dir := filepath.Join(dir, "accounts", "mexc_main")
	require.NoError(t, os.MkdirAll(acc1Dir, 0o755))
	acc1Rev := filepath.Join(acc1Dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(acc1Rev, []byte(`{"takeProfitPct": 2.0, "stopLossPct": 1.0}`), 0o600))
	acc1Obf := filepath.Join(acc1Dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(acc1Obf, []byte(`{"enabled": true, "pollInterval": "15m", "lookbackWindow": "24h"}`), 0o600))
	acc1Dil := filepath.Join(acc1Dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(acc1Dil, []byte(`{"enabled": true, "pollInterval": "45m"}`), 0o600))
	acc1Funding := filepath.Join(acc1Dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(acc1Funding, []byte(`[]`), 0o600))

	// 2. Account 2 sub-configs
	acc2Dir := filepath.Join(dir, "accounts", "mexc_sub1")
	require.NoError(t, os.MkdirAll(acc2Dir, 0o755))
	acc2Rev := filepath.Join(acc2Dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(acc2Rev, []byte(`{"takeProfitPct": 3.0, "stopLossPct": 1.5}`), 0o600))
	acc2Obf := filepath.Join(acc2Dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(acc2Obf, []byte(`{"enabled": false, "pollInterval": "30m", "lookbackWindow": "12h"}`), 0o600))
	acc2Dil := filepath.Join(acc2Dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(acc2Dil, []byte(`{"enabled": false, "pollInterval": "1h"}`), 0o600))
	acc2Funding := filepath.Join(acc2Dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(acc2Funding, []byte(`[]`), 0o600))

	// 3. Blacklist
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte(`{"common": ["LUNA_USDT"]}`), 0o600))

	// 4. Manifest
	manifestPath := filepath.Join(dir, "accounts.jsonc")
	manifestJSON := `{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc",
				"enabled": true,
				"scanners": {
					"configured": true,
					"schedule": true
				},
				"outboundIP": "172.16.0.10",
				"env": {
					"apiKey": "ACC1_KEY",
					"apiSecret": "ACC1_SECRET"
				},
				"configs": {
					"reversion": "` + acc1Rev + `",
					"obfuscator": "` + acc1Obf + `",
					"dilution": "` + acc1Dil + `",
					"funding": "` + acc1Funding + `"
				}
			},
			{
				"id": "mexc_sub1",
				"exchange": "mexc",
				"enabled": true,
				"scanners": {
					"configured": true,
					"schedule": true
				},
				"outboundIP": "172.16.0.11",
				"env": {
					"apiKey": "ACC2_KEY",
					"apiSecret": "ACC2_SECRET"
				},
				"configs": {
					"reversion": "` + acc2Rev + `",
					"obfuscator": "` + acc2Obf + `",
					"dilution": "` + acc2Dil + `",
					"funding": "` + acc2Funding + `"
				}
			}
		]
	}`
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestJSON), 0o600))

	sysCfg := &config.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc": {
				Enable:  true,
				BaseURL: "https://api.mexc.com",
			},
		},
	}

	commonRevPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(commonRevPath, []byte(`{"enabled": true}`), 0o600))

	cfg, err := config.LoadMultiAccount(sysCfg, manifestPath, blacklistPath, commonRevPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Accounts, 2)

	acc1 := cfg.Accounts["mexc_main"]
	require.NotNil(t, acc1)
	assert.Equal(t, "mexc_main", acc1.Account.ID)
	assert.Equal(t, "key_mexc_main", acc1.Account.APIKey)
	assert.Equal(t, "172.16.0.10", acc1.Account.OutboundIP)
	assert.True(t, acc1.Reversion.Enabled)
	assert.InDelta(t, 2.0, acc1.Reversion.Default.TakeProfitPct, 1e-9)
	assert.NotNil(t, acc1.Obfuscator)
	assert.True(t, acc1.Obfuscator.Enabled)
	assert.NotNil(t, acc1.Dilution)
	assert.True(t, acc1.Dilution.Enabled)

	acc2 := cfg.Accounts["mexc_sub1"]
	require.NotNil(t, acc2)
	assert.Equal(t, "mexc_sub1", acc2.Account.ID)
	assert.Equal(t, "key_mexc_sub1", acc2.Account.APIKey)
	assert.Equal(t, "172.16.0.11", acc2.Account.OutboundIP)
	assert.True(t, acc2.Reversion.Enabled)
	assert.InDelta(t, 3.0, acc2.Reversion.Default.TakeProfitPct, 1e-9)
	assert.NotNil(t, acc2.Obfuscator)
	assert.False(t, acc2.Obfuscator.Enabled)
	assert.NotNil(t, acc2.Dilution)
	assert.False(t, acc2.Dilution.Enabled)

	assert.True(t, cfg.Blacklist.IsBlacklisted("mexc", "LUNA_USDT"))
}

func TestLoadAccountBotConfig_MissingConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	revPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(revPath, []byte(`{"enabled": true}`), 0o600))
	obfPath := filepath.Join(dir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(`{"enabled": false, "pollInterval": "1m", "lookbackWindow": "24h"}`), 0o600))
	dilPath := filepath.Join(dir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(`{"enabled": false, "pollInterval": "5s"}`), 0o600))
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))

	baseAcc := sysconfig.AccountConfig{
		ID:       "acc1",
		Exchange: "mexc",
		Enabled:  true,
	}

	// Missing reversion
	acc := baseAcc
	acc.Configs = map[string]string{"obfuscator": obfPath, "dilution": dilPath, "funding": fundingPath}
	_, err := config.LoadAccountBotConfig(acc, nil)
	require.ErrorContains(t, err, "missing reversion config")

	// Missing obfuscator
	acc = baseAcc
	acc.Configs = map[string]string{"reversion": revPath, "dilution": dilPath, "funding": fundingPath}
	_, err = config.LoadAccountBotConfig(acc, nil)
	require.ErrorContains(t, err, "missing obfuscator config")

	// Missing dilution
	acc = baseAcc
	acc.Configs = map[string]string{"reversion": revPath, "obfuscator": obfPath, "funding": fundingPath}
	_, err = config.LoadAccountBotConfig(acc, nil)
	require.ErrorContains(t, err, "missing dilution config")

	// Missing funding
	acc = baseAcc
	acc.Configs = map[string]string{"reversion": revPath, "obfuscator": obfPath, "dilution": dilPath}
	_, err = config.LoadAccountBotConfig(acc, nil)
	require.ErrorContains(t, err, "missing funding config")
}

func TestAccountConfigGetters(t *testing.T) {
	t.Parallel()

	rev := &config.ReversionConfig{Enabled: true}
	cfg := config.NewTestConfig(rev)

	gotRev, err := cfg.ReversionForAccount(config.DefaultAccountID)
	require.NoError(t, err)
	assert.Equal(t, rev, gotRev)

	_, err = cfg.ReversionForAccount("nonexistent")
	require.ErrorContains(t, err, "not found")

	gotObf, err := cfg.ObfuscatorForAccount(config.DefaultAccountID)
	require.NoError(t, err)
	assert.NotNil(t, gotObf)

	_, err = cfg.ObfuscatorForAccount("nonexistent")
	require.ErrorContains(t, err, "not found")

	gotDil, err := cfg.DilutionForAccount(config.DefaultAccountID)
	require.NoError(t, err)
	assert.NotNil(t, gotDil)

	_, err = cfg.DilutionForAccount("nonexistent")
	require.ErrorContains(t, err, "not found")

	var nilCfg *config.Config
	_, err = nilCfg.ReversionForAccount("any")
	require.ErrorContains(t, err, "nil")
	_, err = nilCfg.ObfuscatorForAccount("any")
	require.ErrorContains(t, err, "nil")
	_, err = nilCfg.DilutionForAccount("any")
	require.ErrorContains(t, err, "nil")

	partialCfg := &config.Config{
		Accounts: map[string]*config.AccountBotConfig{
			"acc_empty": {
				Account: sysconfig.AccountConfig{ID: "acc_empty"},
			},
		},
	}
	_, err = partialCfg.ReversionForAccount("acc_empty")
	require.ErrorContains(t, err, "missing reversion config")
	_, err = partialCfg.ObfuscatorForAccount("acc_empty")
	require.ErrorContains(t, err, "missing obfuscator config")
	_, err = partialCfg.DilutionForAccount("acc_empty")
	require.ErrorContains(t, err, "missing dilution config")
}

func TestLoad_CommonReversionInheritanceAndOverrides(t *testing.T) {
	t.Setenv("TEST_KEY", "mock-key")
	t.Setenv("TEST_SECRET", "mock-secret")

	dir := t.TempDir()

	// 1. Common reversion config
	commonRevJSON := `{
		"enabled": true,
		"openType": "ISOLATED",
		"positionMode": "HEDGE",
		"tradeSide": "both",
		"default": {
			"takeProfitPct": 5.0,
			"stopLossPct": 3.0,
			"bufferTime": "1m",
			"postSettleTimeout": "5m",
			"leverage": 5,
			"marginUSD": 20,
			"maxCandidateTrade": 2
		},
		"sync": {
			"ticker": "10s",
			"contract": "30s",
			"time": "15s",
			"funding": "1m"
		},
		"safety": {
			"maxLatency": "2s",
			"maxPriceDiffPercent": 1.5,
			"maxSpreadPercent": 0.5,
			"maxImpactRatio": 2.0
		},
		"notifier": {
			"enable": true
		},
		"statsReporter": {
			"enable": false
		},
		"priceTracker": {
			"enable": true
		},
		"scanners": {
			"configured": true
		}
	}`
	commonRevPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(commonRevPath, []byte(commonRevJSON), 0o600))

	// 2. Account-specific reversion override (strictly exchange overrides)
	accDir := filepath.Join(dir, "accounts", "mexc_main")
	require.NoError(t, os.MkdirAll(accDir, 0o755))
	accRevJSON := `{
		"takeProfitPct": 10.0,
		"stopLossPct": 4.0,
		"bufferTime": "30s",
		"marginUSD": 50,
		"leverage": 10
	}`
	accRevPath := filepath.Join(accDir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(accRevPath, []byte(accRevJSON), 0o600))

	fundingPath := filepath.Join(accDir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))
	obfPath := filepath.Join(accDir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(accDir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc_futures",
				"enabled": true,
				"scanners": {
					"configured": true,
					"schedule": true
				},
				"env": {
					"apiKey": "TEST_KEY",
					"apiSecret": "TEST_SECRET"
				},
				"configs": {
					"reversion": %q,
					"obfuscator": %q,
					"dilution": %q,
					"funding": %q
				}
			}
		]
	}`, accRevPath, obfPath, dilPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	sysCfg := sysWithMexc()

	// Test with explicit commonReversionPath
	cfg, err := config.Load(sysCfg, accountsPath, blacklistPath, commonRevPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.CommonReversion)

	rev, err := cfg.ReversionForAccount("mexc_main")
	require.NoError(t, err)
	require.NotNil(t, rev)

	// Common settings inherited
	assert.True(t, rev.Enabled)
	assert.Equal(t, "ISOLATED", rev.OpenType)
	assert.Equal(t, "HEDGE", rev.PositionMode)
	assert.Equal(t, "both", rev.TradeSide)
	assert.True(t, rev.Notifier.Enabled)
	assert.False(t, rev.StatsReporter.Enabled)
	assert.True(t, rev.PriceTracker.Enabled)
	assert.Equal(t, types.Duration(10*time.Second), rev.Sync.Ticker)
	assert.Equal(t, types.Duration(1*time.Minute), rev.Sync.FundingSync)
	assert.Equal(t, types.Duration(2*time.Second), rev.Safety.MaxLatency)
	assert.InDelta(t, 1.5, rev.Safety.MaxPriceDiffPercent, 1e-9)
	assert.InDelta(t, 0.02, rev.Safety.MaxImpactRatio, 1e-9) // normalized /100

	// Account-level exchange override merged into Default
	assert.InDelta(t, 10.0, rev.Default.TakeProfitPct, 1e-9)
	assert.InDelta(t, 4.0, rev.Default.StopLossPct, 1e-9)
	assert.Equal(t, types.Duration(30*time.Second), rev.Default.BufferTime)
	assert.InDelta(t, 50.0, rev.Default.MarginUSD, 1e-9)
	assert.Equal(t, 10, rev.Default.Leverage)

	// Account-level exchange override merged
	mexcRev, ok := rev.Exchanges["mexc_futures"]
	require.True(t, ok)
	assert.InDelta(t, 10.0, mexcRev.TakeProfitPct, 1e-9)
	assert.InDelta(t, 4.0, mexcRev.StopLossPct, 1e-9)
	assert.Equal(t, types.Duration(30*time.Second), mexcRev.BufferTime)
	assert.InDelta(t, 50.0, mexcRev.MarginUSD, 1e-9)
	assert.Equal(t, 10, mexcRev.Leverage)

	// Symbol inherits PostSettleTimeout from Default and overrides from mexc_futures
	sc, err := cfg.NewAccountSymbolConfig("mexc_main", "mexc_futures", "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, types.Duration(5*time.Minute), sc.FundingReversion.PostSettleTimeout)
	assert.InDelta(t, 0.1, sc.FundingReversion.TakeProfitPct, 1e-9) // normalized to ratio (10% -> 0.1)

	// Verify omitting commonReversionPath fails validation
	_, err = config.Load(sysCfg, accountsPath, blacklistPath, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CommonReversionPath")
}

func TestLoad_WithFlatAccountReversionConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	commonRevJSON := `{
		"enabled": true,
		"openType": "ISOLATED",
		"positionMode": "HEDGE",
		"tradeSide": "both",
		"default": {
			"takeProfitPct": 5.0,
			"stopLossPct": 3.0,
			"bufferTime": "-150ms",
			"postSettleTimeout": "3s",
			"leverage": 5,
			"marginUSD": 3,
			"minVol24USD": 1000000
		},
		"sync": {
			"ticker": "10s",
			"funding": "1m"
		},
		"safety": {
			"maxLatency": "2s",
			"maxPriceDiffPercent": 1.5,
			"maxImpactRatio": 2
		},
		"notifier": {"enable": true},
		"statsReporter": {"enable": false},
		"priceTracker": {"enable": true},
		"scanners": {"configured": true}
	}`
	commonRevPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(commonRevPath, []byte(commonRevJSON), 0o600))

	accDir := filepath.Join(dir, "accounts", "mexc_main")
	require.NoError(t, os.MkdirAll(accDir, 0o750))

	// Flat reversion config as requested by user
	accRevJSON := `{
		"takeProfitPct": 1,
		"stopLossPct": 2,
		"bufferTime": "-52ms",
		"postSettleTimeout": "300s"
	}`
	accRevPath := filepath.Join(accDir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(accRevPath, []byte(accRevJSON), 0o600))

	fundingPath := filepath.Join(accDir, "funding.jsonc")
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[]`), 0o600))
	obfPath := filepath.Join(accDir, "obfuscator.jsonc")
	require.NoError(t, os.WriteFile(obfPath, []byte(defaultObfuscatorJSON), 0o600))
	dilPath := filepath.Join(accDir, "dilution.jsonc")
	require.NoError(t, os.WriteFile(dilPath, []byte(defaultDilutionJSON), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))

	accountsJSON := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc_futures",
				"enabled": true,
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
	}`, accRevPath, obfPath, dilPath, fundingPath)
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsJSON), 0o600))

	sysCfg := sysWithMexc()

	cfg, err := config.Load(sysCfg, accountsPath, blacklistPath, commonRevPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.CommonReversion)

	rev, err := cfg.ReversionForAccount("mexc_main")
	require.NoError(t, err)
	require.NotNil(t, rev)

	// Flat overrides merged into Default
	assert.InDelta(t, 1.0, rev.Default.TakeProfitPct, 1e-9)
	assert.InDelta(t, 2.0, rev.Default.StopLossPct, 1e-9)
	assert.Equal(t, types.Duration(-52*time.Millisecond), rev.Default.BufferTime)
	assert.Equal(t, types.Duration(300*time.Second), rev.Default.PostSettleTimeout)
	// Base values preserved
	assert.Equal(t, 5, rev.Default.Leverage)
	assert.InDelta(t, 3.0, rev.Default.MarginUSD, 1e-9)
	assert.InDelta(t, 1000000.0, rev.Default.MinVol24USD, 1e-9)

	// Symbol resolves flat overrides correctly
	sc, err := cfg.NewAccountSymbolConfig("mexc_main", "mexc_futures", "BTC_USDT")
	require.NoError(t, err)
	assert.InDelta(t, 0.01, sc.FundingReversion.TakeProfitPct, 1e-9) // 1% -> 0.01
	assert.InDelta(t, 0.02, sc.FundingReversion.StopLossPct, 1e-9)   // 2% -> 0.02
	assert.Equal(t, types.Duration(-52*time.Millisecond), sc.FundingReversion.BufferTime)
	assert.Equal(t, types.Duration(300*time.Second), sc.FundingReversion.PostSettleTimeout)
	assert.Equal(t, 5, sc.Leverage)
	assert.InDelta(t, 3.0, sc.MarginUSDT, 1e-9)
}

//nolint:paralleltest // mutates process working directory and env
func TestLoad_RealConfigs(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "mock-key")
	t.Setenv("MEXC_API_SECRET", "mock-secret")
	t.Setenv("MEXC_MAIN_API_KEY", "mock-key")
	t.Setenv("MEXC_MAIN_API_SECRET", "mock-secret")
	t.Setenv("BYBIT_API_KEY", "mock-key")
	t.Setenv("BYBIT_API_SECRET", "mock-secret")

	origWd, err := os.Getwd()
	require.NoError(t, err)
	baseDir := filepath.Join("..", "..", "..", "..")
	require.NoError(t, os.Chdir(baseDir))
	defer func() { _ = os.Chdir(origWd) }()

	testCases := []struct {
		env string
	}{
		{"local"},
		{"prod"},
		{"prod-sg"},
	}

	for _, tc := range testCases {
		t.Run(tc.env, func(t *testing.T) {
			cfgDir := filepath.Join("configs", "funding", tc.env)
			sysPath := filepath.Join(cfgDir, "system.jsonc")
			exchPath := filepath.Join(cfgDir, "exchange.jsonc")
			accountsPath := filepath.Join(cfgDir, "accounts.jsonc")
			blacklistPath := filepath.Join(cfgDir, "blacklist.jsonc")
			revPath := filepath.Join(cfgDir, "reversion.jsonc")

			sysCfg, err := config.LoadSystemConfig(sysPath, exchPath)
			require.NoError(t, err, "failed to load system config for %s", tc.env)

			cfg, err := config.Load(sysCfg, accountsPath, blacklistPath, revPath)
			require.NoError(t, err, "failed to load funding config for %s", tc.env)
			assert.NotEmpty(t, cfg.Accounts, "accounts should not be empty for %s", tc.env)
			assert.NotNil(t, cfg.CommonReversion, "common reversion should not be nil for %s", tc.env)

			rev, err := cfg.ReversionForAccount("mexc_main")
			require.NoError(t, err)
			assert.NotNil(t, rev)
			assert.True(t, rev.Enabled)
		})
	}
}

func TestMaxImpactRatio_GreaterThan100_NoDoubleDivision(t *testing.T) {
	t.Parallel()
	base := &config.ReversionConfig{
		Safety: config.SafetyConfig{
			MaxImpactRatio: 250, // 250%
		},
	}
	acc := &config.AccountReversionConfig{
		MarginUSD: 500,
	}

	merged := config.MergeReversionConfig(base, acc)
	// Must be 2.5 (250 / 100), NOT 0.025 (double divided by 100)
	assert.InDelta(t, 2.5, merged.Safety.MaxImpactRatio, 1e-9)

	// Additional merge must not divide again
	mergedAgain := config.MergeReversionConfig(merged, acc)
	assert.InDelta(t, 2.5, mergedAgain.Safety.MaxImpactRatio, 1e-9)
}

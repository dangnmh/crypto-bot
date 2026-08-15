package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/config"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────────────────────────────
// Helper: creates a temp funding.json and loads it with the given system config.
const defaultObfuscatorJSON = `{"enabled": false, "pollInterval": "1m", "lookbackWindow": "24h"}`

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
	val, ok := testReversionDefaults[sc]
	return val, ok
}

func loadWith(t *testing.T, sysCfg *config.SystemConfig, fundingJSON string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(fundingJSON), 0o600))

	defaults, ok := getTestDefaults(sysCfg)
	if !ok {
		defaults = testDefaults{RawFundingReversionConfig: config.RawFundingReversionConfig{Enabled: true}}
	}

	mockRev := struct {
		config.RawFundingReversionConfig
		Safety   config.SafetyConfig            `json:"safety"`
		Scanners config.ScannersConfig          `json:"scanners"`
		Notifier config.ReversionNotifierConfig `json:"notifier"`
		Sync     config.SyncConfig              `json:"sync"`
	}{
		RawFundingReversionConfig: defaults.RawFundingReversionConfig,
		Safety:                    defaults.Safety,
		Scanners: config.ScannersConfig{
			Configured: true,
		},
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), revData, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))

	cfg, err := config.Load(sysCfg, path, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
	require.NoError(t, err)
	return cfg
}

func loadWithError(t *testing.T, sysCfg *config.SystemConfig, fundingJSON string) error {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(fundingJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true, "scanners": {"configured": true}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	_, err := config.Load(sysCfg, path, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
	return err
}

func sysWithDefaults(defaults testDefaults) *config.SystemConfig {
	sc := &config.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			ExchangeConfig: sysconfig.ExchangeConfig{
				"mexc": sysconfig.APIConfig{
					Future: &sysconfig.RESTConfig{
						Enable:    true,
						BaseURL:   "https://mexc.test",
						WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://mexc.test"},
					},
					APIKey:    "mock-key",
					APISecret: "mock-secret",
				},
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

	require.Len(t, cfg.Symbols, 1)
	assert.Equal(t, "BTC_USDT", cfg.Symbols[0].Symbol)
	assert.Equal(t, float64(100), cfg.Symbols[0].MarginUSDT)
	assert.Equal(t, 20, cfg.Symbols[0].Leverage)
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true, "scanners": {"configured": true}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	_, err := config.Load(&config.SystemConfig{}, filepath.Join(dir, "nonexistent.json"), filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read config")
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true, "scanners": {"configured": true}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))
	_, err := config.Load(sysWithMexc(), path, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
	assert.Error(t, err)
}

func TestLoad_EmptySymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0o600))

	sysCfg := sysWithDefaults(testDefaults{
		RawFundingReversionConfig: config.RawFundingReversionConfig{
			Enabled: true,
			Default: config.ExchangeReversionConfig{
				MarginUSD: 100,
			},
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), revData, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blacklist.jsonc"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))

	cfg, err := config.Load(sysCfg, path, filepath.Join(dir, "blacklist.jsonc"), filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
	require.NoError(t, err)
	assert.Empty(t, cfg.Symbols)

	// Verify that NewSymbolConfig works correctly on this config with empty symbols
	symCfg, err := cfg.NewSymbolConfig("mexc", "BTC_USDT")
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
		RawFundingReversionConfig: config.RawFundingReversionConfig{
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
		},
		Safety: config.SafetyConfig{
			MaxPriceDiffPercent: 0.2,
			MaxLatency:          types.Duration(200 * time.Millisecond),
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100}]`)
	sc := cfg.Symbols[0]

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
		RawFundingReversionConfig: config.RawFundingReversionConfig{
			Default: config.ExchangeReversionConfig{
				Leverage:       10,
				MinFundingRate: 0.5,
			},
		},
		Safety: config.SafetyConfig{},
	})

	cfg := loadWith(t, sysCfg,
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 20, "minFundingRate": 1.0}]`)
	sc := cfg.Symbols[0]

	assert.Equal(t, 20, sc.Leverage, "per-symbol value should win")
	assert.InDelta(t, 0.01, sc.MinFundingRate, 1e-9, "per-symbol 1.0% -> 0.01")
}

func TestLoad_ValidTradeSide(t *testing.T) {
	t.Parallel()
	sysCfg := sysWithDefaults(testDefaults{
		RawFundingReversionConfig: config.RawFundingReversionConfig{
			Enabled:   true,
			TradeSide: " LONG ",
		},
	})
	cfg := loadWith(t, sysCfg, `[]`)
	assert.Equal(t, "long", cfg.Reversion.TradeSide)
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
	sc := cfg.Symbols[0]

	assert.InDelta(t, 0.003, sc.MinFundingRate, 1e-9, "0.3% -> 0.003")
	assert.InDelta(t, 0.8, sc.MaxPriceDiffPercent, 1e-9, "maxPriceDiffPercent remains percent for slippage math")
	assert.InDelta(t, 0.20, sc.FundingReversion.TakeProfitPct, 1e-9, "20% -> 0.20")
	assert.InDelta(t, 0.05, sc.FundingReversion.StopLossPct, 1e-9, "5% -> 0.05")
}

func TestLoad_DefaultTPSL_WhenZero(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithDefaults(testDefaults{RawFundingReversionConfig: config.RawFundingReversionConfig{Enabled: true}}),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 5}]`)
	sc := cfg.Symbols[0]

	assert.InDelta(t, 0.20, sc.FundingReversion.TakeProfitPct, 1e-9, "default TP 20% -> 0.20")
	assert.InDelta(t, 0.05, sc.FundingReversion.StopLossPct, 1e-9, "default SL 5% -> 0.05")
}

func TestLoad_AcceptsDecimalPercentRatiosAtConfigBoundary(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, sysWithMexc(),
		`[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 100, "leverage": 5,
		   "minFundingRate": 0.003,
		   "fundingReversion": {"enabled": true, "takeProfitPct": 0.03, "stopLossPct": 0.02}}]`)
	sc := cfg.Symbols[0]

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
			sc := cfg.Symbols[0]
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
		RawFundingReversionConfig: config.RawFundingReversionConfig{
			Default: config.ExchangeReversionConfig{
				Leverage:       10,
				MinFundingRate: 0.3,
			},
		},
		Safety: config.SafetyConfig{
			MaxPriceDiffPercent: 0.1,
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 50}]`)
	sc := cfg.Symbols[0]
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "obfuscator.jsonc"), []byte(defaultObfuscatorJSON), 0o600))

	cfg, err := config.Load(sysWithMexc(), fundingPath, blacklistPath, filepath.Join(dir, "reversion.jsonc"), filepath.Join(dir, "obfuscator.jsonc"))
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
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true}`), 0o600))

	obfuscatorPath := filepath.Join(dir, "obfuscator.jsonc")
	obfuscatorContent := `{
		"enabled": true,
		"pollInterval": "1m",
		"lookbackWindow": "24h",
		"exchanges": {
			"toobit_futures": {
				"enabled": true,
				"netPnLThresholdUSDT": 5.0,
				"volumeScalePct": 100.0,
				"minUSDT": 10.0,
				"maxUSDT": 500.0,
				"marginUSDT": 10.0,
				"takeProfitPct": 0.5,
				"stopLossPct": 0.5,
				"minHoldSec": 10,
				"maxHoldSec": 60,
				"maxActiveOrders": 1
			}
		}
	}`
	require.NoError(t, os.WriteFile(obfuscatorPath, []byte(obfuscatorContent), 0o600))

	cfg, err := config.Load(sysWithMexc(), fundingPath, blacklistPath, reversionPath, obfuscatorPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.Obfuscator)
	assert.True(t, cfg.Obfuscator.Enabled)
	assert.Equal(t, types.Duration(1*time.Minute), cfg.Obfuscator.PollInterval)
	assert.Equal(t, types.Duration(24*time.Hour), cfg.Obfuscator.LookbackWindow)
	assert.Contains(t, cfg.Obfuscator.Exchanges, "toobit_futures")
}

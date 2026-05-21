package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────────────────────────────
// Helper: creates a temp funding.json and loads it with the given system config.
// ──────────────────────────────────────────────────────────────────────.

func loadWith(t *testing.T, sysCfg *config.SystemConfig, fundingJSON string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(path, []byte(fundingJSON), 0o600))
	cfg, err := config.Load(sysCfg, path)
	require.NoError(t, err)
	return cfg
}

func sysWithDefaults(defaults config.TradingDefaults) *config.SystemConfig {
	raw, _ := json.Marshal(defaults)
	return &config.SystemConfig{TradingDefaults: json.RawMessage(raw)}
}

// ──────────────────────────────────────────────────────────────────────
// config.Load — error cases
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_ValidConfig(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, &config.SystemConfig{},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 20}]`)

	require.Len(t, cfg.Symbols, 1)
	assert.Equal(t, "BTC_USDT", cfg.Symbols[0].Symbol)
	assert.Equal(t, float64(100), cfg.Symbols[0].MarginUSDT)
	assert.Equal(t, 20, cfg.Symbols[0].Leverage)
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := config.Load(&config.SystemConfig{}, "/nonexistent/funding.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read funding config")
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))
	_, err := config.Load(&config.SystemConfig{}, path)
	assert.Error(t, err)
}

func TestLoad_EmptySymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0o600))
	_, err := config.Load(&config.SystemConfig{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoad_MissingSymbolName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "no_sym.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"marginUSDT": 100, "leverage": 20}]`), 0o600))
	_, err := config.Load(&config.SystemConfig{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symbol missing")
}

func TestLoad_InvalidMargin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_margin.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"symbol": "BTC_USDT", "marginUSDT": 0, "leverage": 20}]`), 0o600))
	_, err := config.Load(&config.SystemConfig{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marginUSDT")
}

func TestLoad_InvalidLeverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_lev.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 0}]`), 0o600))
	_, err := config.Load(&config.SystemConfig{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "leverage")
}

// ──────────────────────────────────────────────────────────────────────
// Defaults — verified through Load()
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_AppliesDefaults(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(config.TradingDefaults{
		MinFundingRate:      0.5,
		MaxPriceDiffPercent: 0.2,
		Leverage:            10,
		OpenType:            "ISOLATED",
		PositionMode:        "HEDGE",
		FundingReversion: domain.FundingReversionConfig{
			Enabled:       true,
			TakeProfitPct: 15,
			StopLossPct:   3,
			MaxLatency:    types.Duration(200 * time.Millisecond),
			BufferTime:    types.Duration(10 * time.Millisecond),
		},
		FundingTrap: domain.FundingTrapConfig{
			Enabled:           true,
			SizeRatio:         0.25,
			MaxNotionalUSDT:   250,
			DepthPct:          2.5,
			TakeProfitPct:     1.5,
			StopLossPct:       1.5,
			TrapAfterSettle:   types.Duration(50 * time.Millisecond),
			PostSettleTimeout: types.Duration(60 * time.Second),
		},
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "marginUSDT": 100}]`)
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
	assert.InDelta(t, 0.25, sc.FundingTrap.SizeRatio, 1e-9)
	assert.InDelta(t, 250, sc.FundingTrap.MaxNotionalUSDT, 1e-9)
	assert.Equal(t, types.Duration(50*time.Millisecond), sc.FundingTrap.TrapAfterSettle)
	assert.Equal(t, types.Duration(60*time.Second), sc.FundingTrap.PostSettleTimeout)
}

func TestLoad_DefaultsDoNotOverrideExisting(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(config.TradingDefaults{
		Leverage:       10,
		MinFundingRate: 0.5,
	})

	cfg := loadWith(t, sysCfg,
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 20, "minFundingRate": 1.0}]`)
	sc := cfg.Symbols[0]

	assert.Equal(t, 20, sc.Leverage, "per-symbol value should win")
	assert.InDelta(t, 0.01, sc.MinFundingRate, 1e-9, "per-symbol 1.0% -> 0.01")
}

// ──────────────────────────────────────────────────────────────────────
// Normalization — percentages to ratios, defaults for zero values
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_NormalizesPercentages(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, &config.SystemConfig{},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 5,
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

	cfg := loadWith(t, &config.SystemConfig{TradingDefaults: json.RawMessage(`{"fundingReversion":{"enabled":true}}`)},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 5}]`)
	sc := cfg.Symbols[0]

	assert.InDelta(t, 0.20, sc.FundingReversion.TakeProfitPct, 1e-9, "default TP 20% -> 0.20")
	assert.InDelta(t, 0.05, sc.FundingReversion.StopLossPct, 1e-9, "default SL 5% -> 0.05")
}

func TestLoad_AcceptsDecimalPercentRatiosAtConfigBoundary(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, &config.SystemConfig{},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 5,
		   "minFundingRate": 0.003,
		   "fundingReversion": {"enabled": true, "takeProfitPct": 0.03, "stopLossPct": 0.02},
		   "fundingTrap": {"enabled": true, "depthPct": 0.025, "takeProfitPct": 0.015, "stopLossPct": 0.015,
		     "trailing": {"enabled": true, "activationPct": 0, "callbackPct": 0.005}}}]`)
	sc := cfg.Symbols[0]

	assert.InDelta(t, 0.003, sc.MinFundingRate, 1e-9, "decimal funding threshold preserved")
	assert.InDelta(t, 0.03, sc.FundingReversion.TakeProfitPct, 1e-9, "decimal TP ratio preserved")
	assert.InDelta(t, 0.02, sc.FundingReversion.StopLossPct, 1e-9, "decimal SL ratio preserved")
	assert.InDelta(t, 0.025, sc.FundingTrap.DepthPct, 1e-9, "decimal trap depth preserved")
	assert.InDelta(t, 0.015, sc.FundingTrap.TakeProfitPct, 1e-9, "decimal trap TP preserved")
	assert.InDelta(t, 0.015, sc.FundingTrap.StopLossPct, 1e-9, "decimal trap SL preserved")
	assert.InDelta(t, 0.005, sc.FundingTrap.Trailing.CallbackPct, 1e-9, "decimal trap trailing callback preserved")
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
		{"ISOLATED/HEDGE", `[{"symbol":"S","marginUSDT":1,"leverage":1,"openType":"ISOLATED","positionMode":"HEDGE"}]`, 1, 1},
		{"CROSS/ONE_WAY", `[{"symbol":"S","marginUSDT":1,"leverage":1,"openType":"CROSS","positionMode":"ONE_WAY"}]`, 2, 2},
		{"empty defaults to ISOLATED/HEDGE", `[{"symbol":"S","marginUSDT":1,"leverage":1}]`, 1, 1},
		{"lowercase", `[{"symbol":"S","marginUSDT":1,"leverage":1,"openType":"isolated","positionMode":"hedge"}]`, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := loadWith(t, &config.SystemConfig{}, tt.json)
			sc := cfg.Symbols[0]
			assert.Equal(t, tt.wantOpen, sc.ParsedOpenType)
			assert.Equal(t, tt.wantPosition, sc.ParsedPositionMode)
		})
	}
}

// ──────────────────────────────────────────────────────────────────────
// Trap — defaults, normalization, IsHedgeTrapEnabled
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_TrapDefaults_WhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, &config.SystemConfig{},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 5,
		   "fundingTrap": {"enabled": true}}]`)
	sc := cfg.Symbols[0]

	assert.True(t, sc.IsHedgeTrapEnabled())
	assert.InDelta(t, 0.5, sc.FundingTrap.SizeRatio, 1e-9, "default trap size ratio")
	assert.InDelta(t, 0.05, sc.FundingTrap.DepthPct, 1e-9, "default 5% -> 0.05")
	assert.InDelta(t, 0.02, sc.FundingTrap.TakeProfitPct, 1e-9, "default 2% -> 0.02")
	assert.InDelta(t, 0.02, sc.FundingTrap.StopLossPct, 1e-9, "default 2% -> 0.02")
}

func TestIsHedgeTrapEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sc := &config.SymbolConfig{FundingTrap: domain.FundingTrapConfig{Enabled: tt.enabled}}
			assert.Equal(t, tt.want, sc.IsHedgeTrapEnabled())
		})
	}
}

func TestLoad_NormalizesTrailingPct(t *testing.T) {
	t.Parallel()

	cfg := loadWith(t, &config.SystemConfig{},
		`[{"symbol": "BTC_USDT", "marginUSDT": 100, "leverage": 5,
		   "fundingReversion": {"enabled": true},
		   "fundingTrap": {"enabled": true, "depthPct": 3, "takeProfitPct": 2, "stopLossPct": 2,
		            "trailing": {"enabled": true, "activationPct": 0, "callbackPct": 0.5}}}]`)
	sc := cfg.Symbols[0]

	assert.InDelta(t, 0.0, sc.FundingTrap.Trailing.ActivationPct, 1e-9, "0% -> 0.0")
	assert.InDelta(t, 0.005, sc.FundingTrap.Trailing.CallbackPct, 1e-9, "0.5% -> 0.005")
}

// ──────────────────────────────────────────────────────────────────────
// config.Load with TradingDefaults from system config
// ──────────────────────────────────────────────────────────────────────.

func TestLoad_WithTradingDefaults(t *testing.T) {
	t.Parallel()

	sysCfg := sysWithDefaults(config.TradingDefaults{
		Leverage:            10,
		MinFundingRate:      0.3,
		MaxPriceDiffPercent: 0.1,
	})

	cfg := loadWith(t, sysCfg, `[{"symbol": "BTC_USDT", "marginUSDT": 50}]`)
	sc := cfg.Symbols[0]
	assert.Equal(t, 10, sc.Leverage, "should inherit from defaults")
}

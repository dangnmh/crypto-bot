package domain_test

import (
	"testing"
	"time"

	"crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPennyJumperConfig_Validate(t *testing.T) {
	t.Parallel()

	validCfg := func() domain.PennyJumperConfig {
		cfg := domain.PennyJumperConfig{
			Exchanges:     []string{"mexc_futures", "kucoin_futures", "toobit_futures"},
			ExecutionMode: domain.ExecutionModePaper,
			Universe: domain.UniverseConfig{
				TopGainerLimit:   30,
				MinVolume24hUSDT: 100000.0,
				MaxCoinPrice:     10.0,
				TickerInterval:   types.Duration(15 * time.Minute),
			},
			OrderBookSync: domain.OrderBookSyncConfig{
				MaxBufferCapacity:  500,
				SnapshotTimeout:    types.Duration(5 * time.Second),
				CommitRecoverySize: 1000,
				Exchanges: map[string]domain.ExchangeSyncConfig{
					"mexc_futures": {
						Mode:           "INCREMENTAL",
						StrictSequence: true,
					},
					"kucoin_futures": {
						Mode:           "INCREMENTAL",
						StrictSequence: true,
					},
					"toobit_futures": {
						Mode:           "SNAPSHOT",
						StrictSequence: false,
					},
				},
			},
			WallDetector: domain.WallDetectorConfig{
				MinVolumeUSDT:      20000.0,
				MinLifespan:        types.Duration(5 * time.Second),
				MaxWallDistancePct: 1.0,
				MaxSpreadPct:       1.0,
			},
			WallJudge: domain.WallJudgeConfig{
				Mode:          "dual",
				MinTrustScore: 0.70,
				Timeout:       types.Duration(10 * time.Second),
				EvalCooldown:  types.Duration(5 * time.Second),
				Endpoint:      "http://localhost:8317",
				ApiKey:        "sk-test",
				ModelName:     "gemini-3.7-flash-high",
			},
		}
		cfg.ApplyDefaults()
		return cfg
	}

	t.Run("valid configuration passes validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		require.NoError(t, cfg.Validate())
		assert.Equal(t, []string{"mexc_futures", "kucoin_futures", "toobit_futures"}, cfg.GetExchanges())
	})

	t.Run("missing exchange in OrderBookSync fails validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		delete(cfg.OrderBookSync.Exchanges, "toobit_futures")
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing orderBookSync configuration for exchange: toobit_futures")
	})

	t.Run("invalid mode in ExchangeSyncConfig fails validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.OrderBookSync.Exchanges["toobit_futures"] = domain.ExchangeSyncConfig{
			Mode: "INVALID_MODE",
		}
		err := cfg.Validate()
		assert.Error(t, err)
	})

	t.Run("invalid executionMode fails validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.ExecutionMode = "invalid"
		err := cfg.Validate()
		assert.Error(t, err)
	})

	t.Run("missing or zero maxCoinPrice fails validation without defaults", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.Universe.MaxCoinPrice = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "maxCoinPrice")
	})

	t.Run("spot and futures multi-exchange passes validation", func(t *testing.T) {
		t.Parallel()
		cfg := domain.PennyJumperConfig{
			Exchanges: []string{
				"mexc_futures", "mexc_spot",
				"kucoin_futures", "kucoin_spot",
				"toobit_futures", "toobit_spot",
			},
			ExecutionMode: domain.ExecutionModePaper,
			Universe: domain.UniverseConfig{
				TopGainerLimit:   30,
				MinVolume24hUSDT: 100000.0,
				MaxCoinPrice:     10.0,
				TickerInterval:   types.Duration(15 * time.Minute),
			},
			OrderBookSync: domain.OrderBookSyncConfig{
				MaxBufferCapacity:  2000,
				SnapshotTimeout:    types.Duration(15 * time.Second),
				CommitRecoverySize: 1000,
				Exchanges: map[string]domain.ExchangeSyncConfig{
					"mexc_futures":   {Mode: "INCREMENTAL", StrictSequence: true},
					"mexc_spot":      {Mode: "INCREMENTAL", StrictSequence: false},
					"kucoin_futures": {Mode: "INCREMENTAL", StrictSequence: true},
					"kucoin_spot":    {Mode: "INCREMENTAL", StrictSequence: true},
					"toobit_futures": {Mode: "SNAPSHOT", StrictSequence: false},
					"toobit_spot":    {Mode: "SNAPSHOT", StrictSequence: false},
				},
			},
			WallDetector: domain.WallDetectorConfig{
				MinVolumeUSDT:      20000.0,
				MinLifespan:        types.Duration(5 * time.Second),
				MaxWallDistancePct: 1.0,
				MaxSpreadPct:       1.0,
			},
			WallJudge: domain.WallJudgeConfig{
				Mode:          "dual",
				MinTrustScore: 0.70,
				Timeout:       types.Duration(10 * time.Second),
				EvalCooldown:  types.Duration(5 * time.Second),
				Endpoint:      "http://localhost:8317",
				ApiKey:        "sk-test",
				ModelName:     "gemini-3.7-flash-high",
			},
		}
		cfg.ApplyDefaults()
		require.NoError(t, cfg.Validate())
		assert.Len(t, cfg.GetExchanges(), 6)
	})

	t.Run("invalid wallJudge mode fails validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "invalid_mode"
		err := cfg.Validate()
		assert.Error(t, err)
	})

	t.Run("missing modelName in wallJudge fails validation in model mode", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "model"
		cfg.WallJudge.ModelName = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AI_PROXY_MODEL is required")
	})

	t.Run("missing apiKey in wallJudge fails validation in dual mode", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "dual"
		cfg.WallJudge.ApiKey = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AI_PROXY_API_KEY is required")
	})

	t.Run("missing endpoint in wallJudge fails validation in dual mode", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "dual"
		cfg.WallJudge.Endpoint = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AI_PROXY_URL is required")
	})

	t.Run("valid wallJudge in local mode passes validation without AI credentials", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "local"
		cfg.WallJudge.Endpoint = ""
		cfg.WallJudge.ApiKey = ""
		cfg.WallJudge.ModelName = ""
		require.NoError(t, cfg.Validate())
	})

	t.Run("valid wallJudge in model mode passes validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "model"
		cfg.WallJudge.Endpoint = "http://localhost:11434"
		cfg.WallJudge.ApiKey = "sk-test"
		cfg.WallJudge.ModelName = "qwen2.5:3b"
		require.NoError(t, cfg.Validate())
	})

	t.Run("valid wallJudge in dual mode passes validation", func(t *testing.T) {
		t.Parallel()
		cfg := validCfg()
		cfg.WallJudge.Mode = "dual"
		require.NoError(t, cfg.Validate())
	})
}

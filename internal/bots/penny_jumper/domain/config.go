package domain

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
)

// ExecutionMode defines execution style (live or paper).
type ExecutionMode string

const (
	ExecutionModeLive  ExecutionMode = "live"
	ExecutionModePaper ExecutionMode = "paper"
)

// PennyJumperConfig encapsulates the complete configuration for the Penny Jumper bot.
type PennyJumperConfig struct {
	Exchanges     []string            `json:"exchanges" validate:"required,min=1,dive,required"`  // Target exchanges (e.g. ["toobit", "mexc"])
	ExecutionMode ExecutionMode       `json:"executionMode" validate:"required,oneof=live paper"` // "live" or "paper"
	Universe      UniverseConfig      `json:"universe" validate:"required"`
	OrderBookSync OrderBookSyncConfig `json:"orderBookSync" validate:"required"`
	WallDetector  WallDetectorConfig  `json:"wallDetector" validate:"required"`
	WallJudge     WallJudgeConfig     `json:"wallJudge"`
}

// GetExchanges returns the configured exchanges.
func (c *PennyJumperConfig) GetExchanges() []string {
	return c.Exchanges
}

// UniverseConfig configures dynamic symbol discovery.
type UniverseConfig struct {
	TopGainerLimit   int            `json:"topGainerLimit" validate:"gt=0"`        // Default 30
	MinVolume24hUSDT float64        `json:"minVolume24hUSDT" validate:"gte=0"`     // Filter out illiquid gainers
	MaxCoinPrice     float64        `json:"maxCoinPrice" validate:"required,gt=0"` // Filter out high-priced coins (e.g. BTC, ETH, SOL)
	TickerInterval   types.Duration `json:"tickerInterval" validate:"gt=0"`        // Refresh period (e.g. 15m)
}

// ExchangeSyncConfig configures synchronization mode and sequence checking for a specific exchange.
type ExchangeSyncConfig struct {
	Mode           string `json:"mode" validate:"required,oneof=SNAPSHOT INCREMENTAL"`
	StrictSequence bool   `json:"strictSequence"`
}

// OrderBookSyncConfig configures orderbook synchronization and sequence checking.
type OrderBookSyncConfig struct {
	MaxBufferCapacity  int                           `json:"maxBufferCapacity" validate:"gt=0"`
	SnapshotTimeout    types.Duration                `json:"snapshotTimeout" validate:"gt=0"`
	CommitRecoverySize int                           `json:"commitRecoverySize" validate:"gt=0"`
	Exchanges          map[string]ExchangeSyncConfig `json:"exchanges" validate:"required,dive"`
}

// WallDetectorConfig configures orderbook depth analysis and wall identification.
type WallDetectorConfig struct {
	MinVolumeUSDT      float64        `json:"minVolumeUSDT" validate:"gte=0"`     // Minimum wall notional in USDT (e.g. >= 20000)
	MinLifespan        types.Duration `json:"minLifespan" validate:"gte=0"`       // Minimum age before wall is qualified (e.g. 5s)
	MaxWallDistancePct float64        `json:"maxWallDistancePct" validate:"gt=0"` // Max % away from best bid/ask (e.g. <= 1.0%)
	MaxSpreadPct       float64        `json:"maxSpreadPct" validate:"gte=0"`      // Skip if spread > maxSpreadPct (e.g. <= 1.0%)
}

// Judge mode constants.
const (
	WallJudgeModeLocal = "local"
	WallJudgeModeModel = "model"
	WallJudgeModeDual  = "dual"
)

// WallJudgeConfig configures the wall evaluation model (local, model, or dual).
type WallJudgeConfig struct {
	Mode          string         `json:"mode" validate:"required,oneof=local model dual"`
	MinTrustScore float64        `json:"minTrustScore" validate:"gt=0,lte=1.0"`
	Timeout       types.Duration `json:"timeout" validate:"gt=0"`
	EvalCooldown  types.Duration `json:"evalCooldown" validate:"gt=0"`
	Endpoint      string         `json:"-"`
	ApiKey        string         `json:"-"`
	ModelName     string         `json:"-"`
}

// ApplyDefaults populates unset fields with sensible default parameters.
func (c *PennyJumperConfig) ApplyDefaults() {
	if c.ExecutionMode == "" {
		c.ExecutionMode = ExecutionModePaper
	}
	c.applyUniverseDefaults()
	c.applySyncDefaults()
	c.applyDetectorDefaults()
	c.applyJudgeDefaults()
}

func (c *PennyJumperConfig) applySyncDefaults() {
	if c.OrderBookSync.MaxBufferCapacity <= 0 {
		c.OrderBookSync.MaxBufferCapacity = 500
	}
	if c.OrderBookSync.SnapshotTimeout <= 0 {
		c.OrderBookSync.SnapshotTimeout = types.Duration(5 * time.Second)
	}
	if c.OrderBookSync.CommitRecoverySize <= 0 {
		c.OrderBookSync.CommitRecoverySize = 1000
	}
}

func (c *PennyJumperConfig) applyUniverseDefaults() {
	if c.Universe.TopGainerLimit <= 0 {
		c.Universe.TopGainerLimit = 30
	}
	if c.Universe.MaxCoinPrice <= 0 {
		c.Universe.MaxCoinPrice = 10.0
	}
	if c.Universe.TickerInterval <= 0 {
		c.Universe.TickerInterval = types.Duration(15 * time.Minute)
	}
}

func (c *PennyJumperConfig) applyDetectorDefaults() {
	if c.WallDetector.MaxWallDistancePct <= 0 {
		c.WallDetector.MaxWallDistancePct = 1.0
	}
	if c.WallDetector.MaxSpreadPct <= 0 {
		c.WallDetector.MaxSpreadPct = 1.0
	}
}

func (c *PennyJumperConfig) applyJudgeDefaults() {
	if c.WallJudge.Mode == "" {
		c.WallJudge.Mode = WallJudgeModeDual
	}
	if c.WallJudge.MinTrustScore <= 0 {
		c.WallJudge.MinTrustScore = 0.70
	}
	if c.WallJudge.Timeout <= 0 {
		c.WallJudge.Timeout = types.Duration(10 * time.Second)
	}
	if c.WallJudge.EvalCooldown <= 0 {
		c.WallJudge.EvalCooldown = types.Duration(5 * time.Second)
	}

	// Ingest from environment variables if present
	if envEndpoint := strings.TrimSpace(os.Getenv("AI_PROXY_URL")); envEndpoint != "" {
		c.WallJudge.Endpoint = envEndpoint
	}
	if envAPIKey := strings.TrimSpace(os.Getenv("AI_PROXY_API_KEY")); envAPIKey != "" {
		c.WallJudge.ApiKey = envAPIKey
	}
	if envModel := strings.TrimSpace(os.Getenv("AI_PROXY_MODEL")); envModel != "" {
		c.WallJudge.ModelName = envModel
	}
}

var _validate = func() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}()

// Validate checks for invalid configurations using go-playground/validator.
func (c *PennyJumperConfig) Validate() error {
	if err := _validate.Struct(c); err != nil {
		return fmt.Errorf("invalid penny jumper config: %w", err)
	}
	for _, exch := range c.Exchanges {
		if _, ok := c.OrderBookSync.Exchanges[exch]; !ok {
			return fmt.Errorf("missing orderBookSync configuration for exchange: %s", exch)
		}
	}
	if c.WallJudge.Mode == WallJudgeModeModel || c.WallJudge.Mode == WallJudgeModeDual {
		if strings.TrimSpace(c.WallJudge.Endpoint) == "" {
			return fmt.Errorf("AI_PROXY_URL is required when wallJudge mode is '%s' (set via env or Bitwarden)", c.WallJudge.Mode)
		}
		if strings.TrimSpace(c.WallJudge.ApiKey) == "" {
			return fmt.Errorf("AI_PROXY_API_KEY is required when wallJudge mode is '%s' (set via env or Bitwarden)", c.WallJudge.Mode)
		}
		if strings.TrimSpace(c.WallJudge.ModelName) == "" {
			return fmt.Errorf("AI_PROXY_MODEL is required when wallJudge mode is '%s' (set via env or Bitwarden)", c.WallJudge.Mode)
		}
	}
	return nil
}

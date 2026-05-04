package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/tailscale/hujson"
)

// Duration wraps time.Duration to provide JSON unmarshaling from strings like "30s", "5m".
type Duration time.Duration

// UnmarshalJSON parses a duration string or falls back to number.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}

// SystemConfig represents the global system settings loaded from system.json.
type SystemConfig struct {
	Safety          SafetyConfig    `json:"safety"`
	API             APIConfig       `json:"api"`
	Logging         LoggingConfig   `json:"logging"`
	TradingDefaults json.RawMessage `json:"tradingDefaults"` // Opaque to infrastructure, parsed by bots
	
	// Loaded from .env
	APIKey    string `json:"-"`
	APISecret string `json:"-"`
}

type SafetyConfig struct {
	MaxCapitalPctPerSymbol float64  `json:"maxCapitalPctPerSymbol"`
	MaxImpactRatio         float64  `json:"maxImpactRatio"`
	MaxLatency             Duration `json:"maxLatency"`
	BufferTime             Duration `json:"bufferTime"`

	// Reversion strategy: hold position after entry
	HoldDuration Duration `json:"holdDuration"` // max time to hold position (e.g. "30s")

	// Hedge Trapping configurations
	TrapAfterSettle Duration `json:"trapAfterSettle"` // Duration after settle to throw trap
}

// APIConfig holds MEXC API connection parameters.
type APIConfig struct {
	BaseURL              string   `json:"baseURL"`
	WSURL                string   `json:"wsURL"`
	TimeSyncInterval     Duration `json:"timeSyncInterval"`
	TickerSyncInterval   Duration `json:"tickerSyncInterval"`
	ContractSyncInterval Duration `json:"contractSyncInterval"`
	FundingSyncInterval  Duration `json:"fundingSyncInterval"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level    string `json:"level"`
	Ticker   bool   `json:"ticker"`
	Order    bool   `json:"order"`
	Position bool   `json:"position"`
	HTTP     bool   `json:"http"`
}

// Load reads system.json and returns the SystemConfig.
func Load(systemPath string) (*SystemConfig, error) {
	_ = godotenv.Load()

	sysData, err := os.ReadFile(systemPath)
	if err != nil {
		return nil, fmt.Errorf("read system config %s: %w", systemPath, err)
	}

	sysData, err = hujson.Standardize(sysData)
	if err != nil {
		return nil, fmt.Errorf("standardize system config json: %w", err)
	}

	var sysCfg SystemConfig
	if err := json.Unmarshal(sysData, &sysCfg); err != nil {
		return nil, fmt.Errorf("parse system config: %w", err)
	}

	sysCfg.APIKey = os.Getenv("MEXC_API_KEY")
	sysCfg.APISecret = os.Getenv("MEXC_API_SECRET")

	if err := sysCfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &sysCfg, nil
}

func (c *SystemConfig) validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("MEXC_API_KEY is required (set in .env or environment)")
	}
	if c.APISecret == "" {
		return fmt.Errorf("MEXC_API_SECRET is required (set in .env or environment)")
	}

	if c.API.BaseURL == "" {
		return fmt.Errorf("api.baseURL is required")
	}
	if c.API.WSURL == "" {
		return fmt.Errorf("api.wsURL is required")
	}
	if c.API.TimeSyncInterval <= 0 {
		c.API.TimeSyncInterval = Duration(30 * time.Second) // default to 30s
	}
	if c.API.TickerSyncInterval <= 0 {
		c.API.TickerSyncInterval = Duration(30 * time.Second) // default to 30s
	}
	if c.API.ContractSyncInterval <= 0 {
		c.API.ContractSyncInterval = Duration(300 * time.Second) // default to 5min
	}
	if c.API.FundingSyncInterval <= 0 {
		c.API.FundingSyncInterval = Duration(30 * time.Second) // default to 30s
	}
	if c.Safety.BufferTime <= 0 {
		c.Safety.BufferTime = Duration(10 * time.Millisecond) // default 10ms
	}
	if c.Safety.HoldDuration <= 0 {
		c.Safety.HoldDuration = Duration(30 * time.Second) // default 30s
	}
	if c.Safety.TrapAfterSettle <= 0 {
		c.Safety.TrapAfterSettle = Duration(10 * time.Millisecond) // default 10ms
	}

	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}

	// Normalize Global System percentages
	c.Safety.MaxCapitalPctPerSymbol /= 100
	c.Safety.MaxImpactRatio /= 100

	return nil
}

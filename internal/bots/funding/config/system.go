package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"

	"github.com/tailscale/hujson"
)

const (
	fundingReversionDefaultsFile = "reversion.jsonc"
	fundingTrapDefaultsFile      = "trap.jsonc"
)

// SystemConfig represents the system configuration for the Funding Reversion bot.
type SystemConfig struct {
	sysconfig.SystemConfig
	Safety          SafetyConfig    `json:"safety"`
	Sync            SyncConfig      `json:"sync"`
	TradingDefaults json.RawMessage `json:"tradingDefaults"`
	Notifier        NotifierConfig  `json:"notifier"`
}

type NotifierConfig struct {
	Enabled bool   `json:"enabled"`
	ChatID  string `json:"chatId"`
	Level   string `json:"level"`
}

type SyncConfig struct {
	sysconfig.SyncConfig
	// Funding Sync Interval
	FundingSync types.Duration `json:"funding"`
}

// SafetyConfig holds safety metrics specific to funding reversion.
type SafetyConfig struct {
	MinVol24USD    float64 `json:"minVol24USD"`
	MaxImpactRatio float64 `json:"maxImpactRatio"`
}

// LoadSystemConfig loads the system configuration from the given path.
func LoadSystemConfig(systemPath string) (*SystemConfig, error) {
	sysCfg, err := pkgconfig.Load[SystemConfig](systemPath)
	if err != nil {
		return nil, fmt.Errorf("load funding reversion system config: %w", err)
	}

	if err := sysCfg.loadStrategyDefaults(systemPath); err != nil {
		return nil, err
	}

	// Propagate unmarshaled sync to the base struct
	sysCfg.SystemConfig.Sync = sysCfg.Sync.SyncConfig

	if err := sysconfig.InitializeBase(&sysCfg.SystemConfig); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	// Propagate defaults back
	sysCfg.Sync.SyncConfig = sysCfg.SystemConfig.Sync

	if err := sysCfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return sysCfg, nil
}

func (c *SystemConfig) loadStrategyDefaults(systemPath string) error {
	configDir := filepath.Dir(systemPath)
	strategyFiles := []struct {
		key  string
		name string
	}{
		{key: "fundingReversion", name: fundingReversionDefaultsFile},
		{key: "fundingTrap", name: fundingTrapDefaultsFile},
	}

	for _, strategyFile := range strategyFiles {
		data, err := readOptionalJSONC(filepath.Join(configDir, strategyFile.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load %s defaults: %w", strategyFile.key, err)
		}
		if err := mergeTradingDefault(&c.TradingDefaults, strategyFile.key, data); err != nil {
			return fmt.Errorf("merge %s defaults: %w", strategyFile.key, err)
		}
	}

	return nil
}

func readOptionalJSONC(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data, err = hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("standardize config json %s: %w", path, err)
	}

	return json.RawMessage(data), nil
}

func mergeTradingDefault(defaults *json.RawMessage, key string, value json.RawMessage) error {
	var valueObject map[string]json.RawMessage
	if err := json.Unmarshal(value, &valueObject); err != nil {
		return fmt.Errorf("parse strategy config: %w", err)
	}
	if valueObject == nil {
		return fmt.Errorf("strategy config must be a JSON object")
	}

	defaultsObject := make(map[string]json.RawMessage)
	if len(*defaults) > 0 {
		if err := json.Unmarshal(*defaults, &defaultsObject); err != nil {
			return fmt.Errorf("parse trading defaults: %w", err)
		}
	}
	defaultsObject[key] = value

	merged, err := json.Marshal(defaultsObject)
	if err != nil {
		return fmt.Errorf("encode trading defaults: %w", err)
	}
	*defaults = merged

	return nil
}

func (c *SystemConfig) validate() error {
	// Apply bot specific safety defaults
	if c.Sync.FundingSync <= 0 {
		c.Sync.FundingSync = types.Duration(10 * 1e9) // 10s
	}

	// Normalize Global System percentages.
	c.Safety.MaxImpactRatio /= 100

	return nil
}

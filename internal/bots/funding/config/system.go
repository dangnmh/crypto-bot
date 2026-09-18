package config

import (
	"fmt"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"
)

// SystemConfig represents the system configuration for the Funding Reversion bot.
type SystemConfig struct {
	sysconfig.SystemConfig
}

type NotifierConfig struct {
	Enabled bool   `json:"enabled"`
	ChatID  string `json:"chatId"`
}

// SafetyConfig holds safety metrics specific to funding reversion.
type SafetyConfig struct {
	MaxImpactRatio      float64        `json:"maxImpactRatio"`
	MaxLatency          types.Duration `json:"maxLatency"`
	MaxPriceDiffPercent float64        `json:"maxPriceDiffPercent"`
	MaxSymbolUSDTPrice  float64        `json:"maxSymbolUSDTPrice"`
	normalized          bool
}

// LoadSystemConfig loads the system configuration from the given path.
func LoadSystemConfig(systemPath, exchangePath string) (*SystemConfig, error) {
	sysRaw, err := LoadAndValidate[sysconfig.SystemConfig](systemPath)
	if err != nil {
		return nil, fmt.Errorf("load system config: %w", err)
	}

	exchRaw, err := pkgconfig.Load[sysconfig.ExchangeConfig](exchangePath)
	if err != nil {
		return nil, fmt.Errorf("load exchange config: %w", err)
	}

	sysRaw.ExchangeConfig = *exchRaw

	if err := sysconfig.InitializeBase(sysRaw); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	sysCfg := &SystemConfig{*sysRaw}
	if err := sysCfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return sysCfg, nil
}

func (c *SystemConfig) validate() error {
	if c.NotiConfig.Enabled && c.NotiConfig.TelegramChatID == "" {
		return fmt.Errorf("notifier is enabled but chatId is missing (set TELEGRAM_CHAT_ID in environment or .env)")
	}

	return nil
}

package config

import (
	"fmt"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"
)

type ScannersConfig struct {
	Configured bool            `json:"configured"`
	Schedule   map[string]bool `json:"schedule"`
}

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
	MinVol24USD         float64        `json:"minVol24USD"`
	MaxImpactRatio      float64        `json:"maxImpactRatio"`
	MaxLatency          types.Duration `json:"maxLatency"`
	MinFundingRate      float64        `json:"minFundingRate"`
	MaxPriceDiffPercent float64        `json:"maxPriceDiffPercent"`
	MaxSymbolUSDTPrice  float64        `json:"maxSymbolUSDTPrice"`
}

// LoadSystemConfig loads the system configuration from the given path.
func LoadSystemConfig(systemPath string) (*SystemConfig, error) {
	sysCfg, err := pkgconfig.Load[SystemConfig](systemPath)
	if err != nil {
		return nil, fmt.Errorf("load funding reversion system config: %w", err)
	}

	if err := sysconfig.InitializeBase(&sysCfg.SystemConfig); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	if err := sysCfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return sysCfg, nil
}

func (c *SystemConfig) validate() error {
	if c.NotiConfig.Enabled && c.NotiConfig.TelegramChatID == "" {
		return fmt.Errorf("notifier is enabled but chatId is missing (set TELEGRAM_CHAT_ID in .env or Bitwarden)")
	}

	return nil
}

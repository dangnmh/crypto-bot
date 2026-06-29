// Package toolconfig provides configuration loading with Bitwarden fallback for CLI tools.
package toolconfig

import (
	"fmt"
	"path/filepath"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
)

// Load loads the system config and applies standard credentials injections and defaults.
func Load(configPath string) (*sysconfig.SystemConfig, error) {
	if configPath == "" {
		configPath = "configs/funding/local/main.jsonc"
	}
	cfg, err := pkgconfig.Load[sysconfig.SystemConfig](configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	exchPath := filepath.Join(filepath.Dir(configPath), "exchange.jsonc")
	exchCfg, err := pkgconfig.Load[sysconfig.SystemConfig](exchPath)
	if err != nil {
		return nil, fmt.Errorf("load exchange config: %w", err)
	}
	cfg.ExchangeConfig = exchCfg.ExchangeConfig

	// Use standard main-app config initialization, which dynamically loads env
	// variables, handles Bitwarden fallback for all exchanges, and performs validations.
	if err := sysconfig.InitializeBase(cfg); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	return cfg, nil
}

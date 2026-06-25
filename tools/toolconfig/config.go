// Package toolconfig provides configuration loading with Bitwarden fallback for CLI tools.
package toolconfig

import (
	"fmt"
	"os"
	"path/filepath"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
)

// Load loads the system config and applies Bitwarden fallback if env vars are empty.
func Load(configPath string) (*sysconfig.SystemConfig, error) {
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

	if err := applyBitwardenFallback(cfg); err != nil {
		return nil, err
	}

	if err := validateCredentials(cfg); err != nil {
		return nil, err
	}

	if err := validateEndpoints(cfg); err != nil {
		return nil, err
	}

	applySystemDefaults(cfg)

	return cfg, nil
}

func applyBitwardenFallback(cfg *sysconfig.SystemConfig) error {
	if cfg.ExchangeConfig == nil {
		cfg.ExchangeConfig = make(sysconfig.ExchangeConfig)
	}
	mexcCfg := cfg.ExchangeConfig["mexc"]
	if mexcCfg.APIKey != "" && mexcCfg.APISecret != "" {
		return nil
	}
	if !hasBitwardenConfig() {
		return nil
	}
	credentials, err := loadFromBitwarden()
	if err != nil {
		return fmt.Errorf("bitwarden fallback failed: %w", err)
	}
	if mexcCfg.APIKey == "" {
		mexcCfg.APIKey = credentials.APIKey
	}
	if mexcCfg.APISecret == "" {
		mexcCfg.APISecret = credentials.APISecret
	}
	cfg.ExchangeConfig["mexc"] = mexcCfg
	return nil
}

func validateCredentials(cfg *sysconfig.SystemConfig) error {
	if cfg.ExchangeConfig == nil || cfg.ExchangeConfig["mexc"].APIKey == "" {
		return fmt.Errorf("MEXC_API_KEY is required (set in .env, environment, or Bitwarden)")
	}
	if cfg.ExchangeConfig["mexc"].APISecret == "" {
		return fmt.Errorf("MEXC_API_SECRET is required (set in .env, environment, or Bitwarden)")
	}
	return nil
}

func validateEndpoints(cfg *sysconfig.SystemConfig) error {
	if cfg.ExchangeConfig == nil || cfg.ExchangeConfig["mexc"].Future.BaseURL == "" {
		return fmt.Errorf("api.future.baseURL is required")
	}
	if cfg.ExchangeConfig["mexc"].WebSocket.WSURL == "" {
		return fmt.Errorf("api.websocket.wsURL is required")
	}
	return nil
}

func applySystemDefaults(cfg *sysconfig.SystemConfig) {
	if cfg.ExchangeConfig == nil {
		cfg.ExchangeConfig = make(sysconfig.ExchangeConfig)
	}
	mexcCfg := cfg.ExchangeConfig["mexc"]
	if mexcCfg.WebSocket.MaxPairsPerWSConn <= 0 {
		mexcCfg.WebSocket.MaxPairsPerWSConn = 30
	}
	cfg.ExchangeConfig["mexc"] = mexcCfg
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
}

// bitwardenCredentials holds API credentials from Bitwarden.
type bitwardenCredentials struct {
	APIKey    string
	APISecret string
}

// hasBitwardenConfig checks if Bitwarden environment variables are set.
func hasBitwardenConfig() bool {
	return os.Getenv("BITWARDEN_ACCESS_TOKEN") != "" &&
		os.Getenv("BITWARDEN_ORGANIZATION_ID") != "" &&
		os.Getenv("BITWARDEN_PROJECT_NAME") != ""
}

// loadFromBitwarden retrieves MEXC credentials from Bitwarden Secrets Manager.
func loadFromBitwarden() (*bitwardenCredentials, error) {
	loader, err := sysconfig.NewBitwardenLoader()
	if err != nil {
		return nil, err
	}

	apiKey, err := loader.GetSecret("MEXC_API_KEY")
	if err != nil {
		return nil, fmt.Errorf("failed to get MEXC_API_KEY from Bitwarden: %w", err)
	}

	apiSecret, err := loader.GetSecret("MEXC_API_SECRET")
	if err != nil {
		return nil, fmt.Errorf("failed to get MEXC_API_SECRET from Bitwarden: %w", err)
	}

	return &bitwardenCredentials{
		APIKey:    apiKey,
		APISecret: apiSecret,
	}, nil
}

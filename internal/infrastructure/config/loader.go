package config

import (
	"fmt"
	"os"
	"strings"

	"crypto-bot/pkg/types"

	"github.com/joho/godotenv"
)

// InitializeBase loads environment variables, injects credentials,
// and applies universal default values to the core SystemConfig.
// This function should be called by any bot immediately after parsing its JSON configuration.
func InitializeBase(c *SystemConfig) error {
	_ = godotenv.Load()

	c.APIKey = os.Getenv("MEXC_API_KEY")
	c.APISecret = os.Getenv("MEXC_API_SECRET")

	if err := applyBitwardenFallback(c); err != nil {
		return err
	}

	if err := validateCredentials(c); err != nil {
		return err
	}

	if err := validateEndpoints(c); err != nil {
		return err
	}

	applySystemDefaults(c)

	return nil
}

func applyBitwardenFallback(c *SystemConfig) error {
	if c.APIKey != "" && c.APISecret != "" {
		return nil
	}
	if !hasBitwardenConfig() {
		return nil
	}
	credentials, err := loadFromBitwarden()
	if err != nil {
		return fmt.Errorf("bitwarden fallback failed: %w", err)
	}
	if c.APIKey == "" {
		c.APIKey = credentials.APIKey
	}
	if c.APISecret == "" {
		c.APISecret = credentials.APISecret
	}
	return nil
}

func validateCredentials(c *SystemConfig) error {
	if c.APIKey == "" {
		return fmt.Errorf("MEXC_API_KEY is required (set in .env, environment, or Bitwarden)")
	}
	if c.APISecret == "" {
		return fmt.Errorf("MEXC_API_SECRET is required (set in .env, environment, or Bitwarden)")
	}
	return nil
}

func validateEndpoints(c *SystemConfig) error {
	if c.API.Future.BaseURL == "" {
		return fmt.Errorf("api.future.baseURL is required")
	}
	if c.API.WebSocket.WSURL == "" {
		return fmt.Errorf("api.websocket.wsURL is required")
	}
	return nil
}

func applySystemDefaults(c *SystemConfig) {
	if c.Sync.Time <= 0 {
		c.Sync.Time = types.Duration(30 * 1e9) // 30s
	}
	if c.Sync.Ticker <= 0 {
		c.Sync.Ticker = types.Duration(30 * 1e9) // 30s
	}
	if c.Sync.Contract <= 0 {
		c.Sync.Contract = types.Duration(300 * 1e9) // 5min
	}
	if c.API.WebSocket.MaxPairsPerWSConn <= 0 {
		c.API.WebSocket.MaxPairsPerWSConn = 30 // default MEXC limit
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
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
	loader, err := NewBitwardenLoader()
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

	// Trim whitespace from credentials
	apiKey = strings.TrimSpace(apiKey)
	apiSecret = strings.TrimSpace(apiSecret)

	return &bitwardenCredentials{
		APIKey:    apiKey,
		APISecret: apiSecret,
	}, nil
}

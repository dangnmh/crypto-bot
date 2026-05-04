package config

import (
	"fmt"
	"os"

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

	if c.APIKey == "" {
		return fmt.Errorf("MEXC_API_KEY is required (set in .env or environment)")
	}
	if c.APISecret == "" {
		return fmt.Errorf("MEXC_API_SECRET is required (set in .env or environment)")
	}

	if c.API.Future.BaseURL == "" {
		return fmt.Errorf("api.future.baseURL is required")
	}
	if c.API.WebSocket.WSURL == "" {
		return fmt.Errorf("api.websocket.wsURL is required")
	}

	// Apply generic defaults
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

	return nil
}

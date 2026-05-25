package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"crypto-bot/pkg/types"

	"github.com/bitwarden/sdk-go/v2"
	"github.com/joho/godotenv"
)

// InitializeBase loads environment variables, injects credentials,
// and applies universal default values to the core SystemConfig.
// This function should be called by any bot immediately after parsing its JSON configuration.
func InitializeBase(c *SystemConfig) error {
	_ = godotenv.Load()

	c.ExchangeConfig.Mexc.APIKey = os.Getenv("MEXC_API_KEY")
	c.ExchangeConfig.Mexc.APISecret = os.Getenv("MEXC_API_SECRET")
	c.ExchangeConfig.Gate.APIKey = os.Getenv("GATE_API_KEY")
	c.ExchangeConfig.Gate.APISecret = os.Getenv("GATE_API_SECRET")

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
	mexcSet := c.ExchangeConfig.Mexc.APIKey != "" && c.ExchangeConfig.Mexc.APISecret != ""
	gateSet := c.ExchangeConfig.Gate.APIKey != "" && c.ExchangeConfig.Gate.APISecret != ""
	notiSet := c.NotiConfig.TelegramChatID != "" && c.NotiConfig.TelegramBotToken != ""

	mexcEnabled := c.ExchangeConfig.Mexc.Future.BaseURL != ""
	gateEnabled := c.ExchangeConfig.Gate.Future.BaseURL != ""

	if (!mexcEnabled || mexcSet) && (!gateEnabled || gateSet) && notiSet {
		return nil
	}

	creds, err := LoadFromBitwarden()
	if err != nil {
		// Bitwarden not configured or failed - non-fatal if env vars are set
		if !hasBitwardenConfig() {
			return nil
		}
		return fmt.Errorf("bitwarden fallback failed: %w", err)
	}

	// Only fill missing values (keep env vars priority)
	if mexcEnabled && c.ExchangeConfig.Mexc.APIKey == "" {
		c.ExchangeConfig.Mexc.APIKey = creds.APIKey
	}
	if mexcEnabled && c.ExchangeConfig.Mexc.APISecret == "" {
		c.ExchangeConfig.Mexc.APISecret = creds.APISecret
	}
	if gateEnabled && c.ExchangeConfig.Gate.APIKey == "" {
		c.ExchangeConfig.Gate.APIKey = creds.GateKey
	}
	if gateEnabled && c.ExchangeConfig.Gate.APISecret == "" {
		c.ExchangeConfig.Gate.APISecret = creds.GateSecret
	}
	if c.NotiConfig.TelegramChatID == "" {
		c.NotiConfig.TelegramChatID = creds.TelegramChatID
	}
	if c.NotiConfig.TelegramBotToken == "" {
		c.NotiConfig.TelegramBotToken = creds.TelegramBotToken
	}

	return nil
}

// LoadFromBitwarden retrieves MEXC and Gate credentials and Telegram Chat ID from Bitwarden Secrets Manager.
func LoadFromBitwarden() (*bitwardenCredentials, error) {
	if !hasBitwardenConfig() {
		return nil, fmt.Errorf("bitwarden configuration not found (BITWARDEN_ACCESS_TOKEN, BITWARDEN_ORGANIZATION_ID, BITWARDEN_PROJECT_NAME required)")
	}
	loader, err := newBitwardenLoader()
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

	gateKey, _ := loader.GetSecret("GATE_API_KEY")
	gateSecret, _ := loader.GetSecret("GATE_API_SECRET")

	telegramChatID, err := loader.GetSecret("TELEGRAM_CHAT_ID")
	if err != nil {
		slog.Error("failed to get TELEGRAM_CHAT_ID from Bitwarden", slog.Any("error", err))
	}

	telegramBotToken, err := loader.GetSecret("TELEGRAM_BOT_TOKEN")
	if err != nil {
		slog.Error("failed to get TELEGRAM_BOT_TOKEN from Bitwarden", slog.Any("error", err))
	}

	// Trim whitespace from credentials
	apiKey = strings.TrimSpace(apiKey)
	apiSecret = strings.TrimSpace(apiSecret)
	gateKey = strings.TrimSpace(gateKey)
	gateSecret = strings.TrimSpace(gateSecret)
	telegramChatID = strings.TrimSpace(telegramChatID)
	telegramBotToken = strings.TrimSpace(telegramBotToken)

	return &bitwardenCredentials{
		APIKey:           apiKey,
		APISecret:        apiSecret,
		GateKey:          gateKey,
		GateSecret:       gateSecret,
		TelegramChatID:   telegramChatID,
		TelegramBotToken: telegramBotToken,
	}, nil
}

// hasBitwardenConfig checks if Bitwarden environment variables are set.
func hasBitwardenConfig() bool {
	return os.Getenv("BITWARDEN_ACCESS_TOKEN") != "" &&
		os.Getenv("BITWARDEN_ORGANIZATION_ID") != "" &&
		os.Getenv("BITWARDEN_PROJECT_NAME") != ""
}

// newBitwardenLoader creates a new Bitwarden secrets loader.
func newBitwardenLoader() (*BitwardenLoader, error) {
	accessToken := os.Getenv("BITWARDEN_ACCESS_TOKEN")
	organizationID := os.Getenv("BITWARDEN_ORGANIZATION_ID")
	projectName := os.Getenv("BITWARDEN_PROJECT_NAME")

	accessToken = strings.TrimSpace(accessToken)
	organizationID = strings.TrimSpace(organizationID)
	projectName = strings.TrimSpace(projectName)

	client, err := sdk.NewBitwardenClient(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bitwarden client: %w", err)
	}

	if err := client.AccessTokenLogin(accessToken, nil); err != nil {
		return nil, fmt.Errorf("failed to login to Bitwarden: %w", err)
	}

	return &BitwardenLoader{
		client:         client,
		accessToken:    accessToken,
		organizationID: organizationID,
		projectName:    projectName,
		secretCache:    make(map[string]string),
	}, nil
}

func validateCredentials(c *SystemConfig) error {
	if c.ExchangeConfig.Mexc.Future.BaseURL != "" {
		if c.ExchangeConfig.Mexc.APIKey == "" {
			return fmt.Errorf("MEXC_API_KEY is required (set in .env, environment, or Bitwarden)")
		}
		if c.ExchangeConfig.Mexc.APISecret == "" {
			return fmt.Errorf("MEXC_API_SECRET is required (set in .env, environment, or Bitwarden)")
		}
	}
	if c.ExchangeConfig.Gate.Future.BaseURL != "" {
		if c.ExchangeConfig.Gate.APIKey == "" {
			return fmt.Errorf("GATE_API_KEY is required (set in .env, environment, or Bitwarden)")
		}
		if c.ExchangeConfig.Gate.APISecret == "" {
			return fmt.Errorf("GATE_API_SECRET is required (set in .env, environment, or Bitwarden)")
		}
	}
	return nil
}

func validateEndpoints(c *SystemConfig) error {
	mexcEnabled := c.ExchangeConfig.Mexc.Future.BaseURL != ""
	gateEnabled := c.ExchangeConfig.Gate.Future.BaseURL != ""

	if !mexcEnabled && !gateEnabled {
		return fmt.Errorf("api.future.baseURL is required for at least one active exchange")
	}

	if mexcEnabled {
		if c.ExchangeConfig.Mexc.WebSocket.WSURL == "" {
			return fmt.Errorf("mexc api.websocket.wsURL is required when mexc is enabled")
		}
	}
	if gateEnabled {
		if c.ExchangeConfig.Gate.WebSocket.WSURL == "" {
			return fmt.Errorf("gate api.websocket.wsURL is required when gate is enabled")
		}
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
	if c.ExchangeConfig.Mexc.Future.BaseURL != "" && c.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn <= 0 {
		c.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn = 30 // default MEXC limit
	}
	if c.ExchangeConfig.Gate.Future.BaseURL != "" && c.ExchangeConfig.Gate.WebSocket.MaxPairsPerWSConn <= 0 {
		c.ExchangeConfig.Gate.WebSocket.MaxPairsPerWSConn = 30 // default Gate limit
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
}

// bitwardenCredentials holds API credentials from Bitwarden.
type bitwardenCredentials struct {
	APIKey           string
	APISecret        string
	GateKey          string
	GateSecret       string
	TelegramChatID   string
	TelegramBotToken string
}

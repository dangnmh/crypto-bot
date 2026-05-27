package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"crypto-bot/pkg/types"

	"github.com/bitwarden/sdk-go/v2"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

type bitwardenSecretLoader interface {
	GetSecret(secretKey string) (string, error)
}

var newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
	return newBitwardenLoader()
}

// InitializeBase loads environment variables, injects credentials,
// and applies universal default values to the core SystemConfig.
// This function should be called by any bot immediately after parsing its JSON configuration.
func InitializeBase(c *SystemConfig) error {
	_ = godotenv.Load()

	c.ExchangeConfig.Mexc.APIKey = os.Getenv("MEXC_API_KEY")
	c.ExchangeConfig.Mexc.APISecret = os.Getenv("MEXC_API_SECRET")
	c.ExchangeConfig.Gate.APIKey = os.Getenv("GATE_API_KEY")
	c.ExchangeConfig.Gate.APISecret = os.Getenv("GATE_API_SECRET")
	c.ExchangeConfig.Okx.APIKey = os.Getenv("OKX_API_KEY")
	c.ExchangeConfig.Okx.APISecret = os.Getenv("OKX_API_SECRET")

	if err := applyBitwardenFallback(c); err != nil {
		return err
	}

	applySystemDefaults(c)

	// Ensure at least one active exchange is enabled
	if !c.ExchangeConfig.Mexc.Enable && !c.ExchangeConfig.Gate.Enable && !c.ExchangeConfig.Okx.Enable && !c.ExchangeConfig.Binance.Enable {
		return fmt.Errorf("at least one active exchange must be enabled")
	}

	validate := validator.New()
	_ = validate.RegisterValidation("api_config", ValidateAPIConfigField)
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("system config validation failed: %w", err)
	}

	return nil
}

func applyBitwardenFallback(c *SystemConfig) error {
	if bitwardenFallbackNotNeeded(c) {
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
	if c.ExchangeConfig.Mexc.Enable && c.ExchangeConfig.Mexc.APIKey == "" {
		c.ExchangeConfig.Mexc.APIKey = creds.APIKey
	}
	if c.ExchangeConfig.Mexc.Enable && c.ExchangeConfig.Mexc.APISecret == "" {
		c.ExchangeConfig.Mexc.APISecret = creds.APISecret
	}
	if c.ExchangeConfig.Gate.Enable && c.ExchangeConfig.Gate.APIKey == "" {
		c.ExchangeConfig.Gate.APIKey = creds.GateKey
	}
	if c.ExchangeConfig.Gate.Enable && c.ExchangeConfig.Gate.APISecret == "" {
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

func bitwardenFallbackNotNeeded(c *SystemConfig) bool {
	return exchangeCredentialsComplete(c.ExchangeConfig.Mexc) &&
		exchangeCredentialsComplete(c.ExchangeConfig.Gate) &&
		notificationCredentialsComplete(c.NotiConfig)
}

func exchangeCredentialsComplete(c APIConfig) bool {
	if !c.Enable {
		return true
	}
	return c.APIKey != "" && c.APISecret != ""
}

func notificationCredentialsComplete(c NotiConfig) bool {
	return c.TelegramChatID != "" && c.TelegramBotToken != ""
}

// LoadFromBitwarden retrieves MEXC and Gate credentials and Telegram Chat ID from Bitwarden Secrets Manager.
func LoadFromBitwarden() (*bitwardenCredentials, error) {
	if !hasBitwardenConfig() {
		return nil, fmt.Errorf("bitwarden configuration not found (BITWARDEN_ACCESS_TOKEN, BITWARDEN_ORGANIZATION_ID, BITWARDEN_PROJECT_NAME required)")
	}
	loader, err := newBitwardenSecretLoader()
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

// ValidateAPIConfigField validates an APIConfig struct field using the api_config tag.
func ValidateAPIConfigField(fl validator.FieldLevel) bool {
	cfg, ok := fl.Field().Interface().(APIConfig)
	if !ok {
		return false
	}
	if !cfg.Enable {
		return true
	}
	if cfg.Future.BaseURL == "" {
		return false
	}
	if _, err := url.ParseRequestURI(cfg.Future.BaseURL); err != nil {
		return false
	}
	if cfg.WebSocket.WSURL == "" {
		return false
	}
	if _, err := url.ParseRequestURI(cfg.WebSocket.WSURL); err != nil {
		return false
	}
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return false
	}
	return true
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
	if c.ExchangeConfig.Okx.Future.BaseURL != "" && c.ExchangeConfig.Okx.WebSocket.MaxPairsPerWSConn <= 0 {
		c.ExchangeConfig.Okx.WebSocket.MaxPairsPerWSConn = 30 // default OKX limit
	}
	if c.ExchangeConfig.Binance.Future.BaseURL != "" && c.ExchangeConfig.Binance.WebSocket.MaxPairsPerWSConn <= 0 {
		c.ExchangeConfig.Binance.WebSocket.MaxPairsPerWSConn = 30 // default Binance limit
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

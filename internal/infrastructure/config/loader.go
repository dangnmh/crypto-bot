package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/bitwarden/sdk-go/v2"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/samber/lo"
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

	c.ExchangeConfig.Mexc.APIKey = strings.TrimSpace(os.Getenv("MEXC_API_KEY"))
	c.ExchangeConfig.Mexc.APISecret = strings.TrimSpace(os.Getenv("MEXC_API_SECRET"))
	c.ExchangeConfig.Gate.APIKey = strings.TrimSpace(os.Getenv("GATE_API_KEY"))
	c.ExchangeConfig.Gate.APISecret = strings.TrimSpace(os.Getenv("GATE_API_SECRET"))
	c.ExchangeConfig.Okx.APIKey = strings.TrimSpace(os.Getenv("OKX_API_KEY"))
	c.ExchangeConfig.Okx.APISecret = strings.TrimSpace(os.Getenv("OKX_API_SECRET"))
	c.ExchangeConfig.Okx.APIPassphrase = strings.TrimSpace(os.Getenv("OKX_API_PASSPHRASE"))

	c.ExchangeConfig.Bybit.APIKey = strings.TrimSpace(os.Getenv("BYBIT_API_KEY"))
	c.ExchangeConfig.Bybit.APISecret = strings.TrimSpace(os.Getenv("BYBIT_API_SECRET"))
	c.ExchangeConfig.Binance.APIKey = strings.TrimSpace(os.Getenv("BINANCE_API_KEY"))
	c.ExchangeConfig.Binance.APISecret = strings.TrimSpace(os.Getenv("BINANCE_API_SECRET"))
	c.ExchangeConfig.Bitget.APIKey = strings.TrimSpace(os.Getenv("BITGET_API_KEY"))
	c.ExchangeConfig.Bitget.APISecret = strings.TrimSpace(os.Getenv("BITGET_API_SECRET"))
	c.ExchangeConfig.Kucoin.APIKey = strings.TrimSpace(os.Getenv("KUCOIN_API_KEY"))
	c.ExchangeConfig.Kucoin.APISecret = strings.TrimSpace(os.Getenv("KUCOIN_API_SECRET"))
	c.ExchangeConfig.Kucoin.APIPassphrase = strings.TrimSpace(os.Getenv("KUCOIN_API_PASSPHRASE"))
	c.ExchangeConfig.Bingx.APIKey = strings.TrimSpace(os.Getenv("BINGX_API_KEY"))
	c.ExchangeConfig.Bingx.APISecret = strings.TrimSpace(os.Getenv("BINGX_API_SECRET"))
	c.ExchangeConfig.Deepcoin.APIKey = strings.TrimSpace(os.Getenv("DEEPCOIN_API_KEY"))
	c.ExchangeConfig.Deepcoin.APISecret = strings.TrimSpace(os.Getenv("DEEPCOIN_API_SECRET"))
	c.ExchangeConfig.Deepcoin.APIPassphrase = strings.TrimSpace(os.Getenv("DEEPCOIN_API_PASSPHRASE"))
	c.ExchangeConfig.Toobit.APIKey = strings.TrimSpace(os.Getenv("TOOBIT_API_KEY"))
	c.ExchangeConfig.Toobit.APISecret = strings.TrimSpace(os.Getenv("TOOBIT_API_SECRET"))
	c.NotiConfig.TelegramChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	c.NotiConfig.TelegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err := applyBitwardenFallback(c); err != nil {
		return err
	}

	applySystemDefaults(c)

	// Ensure at least one active exchange is enabled
	if !lo.Contains([]bool{
		c.ExchangeConfig.Mexc.Enable,
		c.ExchangeConfig.Gate.Enable,
		c.ExchangeConfig.Okx.Enable,
		c.ExchangeConfig.Binance.Enable,
		c.ExchangeConfig.Bybit.Enable,
		c.ExchangeConfig.Kucoin.Enable,
		c.ExchangeConfig.Bingx.Enable,
		c.ExchangeConfig.Hyperliquid.Enable,
		c.ExchangeConfig.Bitget.Enable,
		c.ExchangeConfig.Deepcoin.Enable,
		c.ExchangeConfig.Toobit.Enable,
	}, true) {
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
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Mexc, creds.MEXCAPIKey, creds.MEXCAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Gate, creds.GateAPIKey, creds.GateAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Bybit, creds.BybitAPIKey, creds.BybitAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Binance, creds.BinanceAPIKey, creds.BinanceAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Bitget, creds.BitgetAPIKey, creds.BitgetAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Kucoin, creds.KucoinAPIKey, creds.KucoinAPISecret, creds.KucoinPassphrase)
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Bingx, creds.BingxAPIKey, creds.BingxAPISecret, "")
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Okx, creds.OkxAPIKey, creds.OkxAPISecret, creds.OkxAPIPassphrase)
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Deepcoin, creds.DeepcoinAPIKey, creds.DeepcoinAPISecret, creds.DeepcoinAPIPassphrase)
	fallbackExchangeAPIConfig(&c.ExchangeConfig.Toobit, creds.ToobitAPIKey, creds.ToobitAPISecret, "")

	if c.NotiConfig.TelegramChatID == "" {
		c.NotiConfig.TelegramChatID = creds.TelegramChatID
	}
	if c.NotiConfig.TelegramBotToken == "" {
		c.NotiConfig.TelegramBotToken = creds.TelegramBotToken
	}

	return nil
}

func fallbackExchangeAPIConfig(cfg *APIConfig, key, secret, passphrase string) {
	if cfg.Enable && cfg.APIKey == "" {
		cfg.APIKey = key
	}
	if cfg.Enable && cfg.APISecret == "" {
		cfg.APISecret = secret
	}
	if cfg.Enable && cfg.APIPassphrase == "" && passphrase != "" {
		cfg.APIPassphrase = passphrase
	}
}

const kucoinName = "Kucoin"
const okxName = "Okx"
const deepcoinName = "Deepcoin"

func bitwardenFallbackNotNeeded(c *SystemConfig) bool {
	return exchangeCredentialsComplete("Mexc", c.ExchangeConfig.Mexc) &&
		exchangeCredentialsComplete("Gate", c.ExchangeConfig.Gate) &&
		exchangeCredentialsComplete("Bybit", c.ExchangeConfig.Bybit) &&
		exchangeCredentialsComplete("Binance", c.ExchangeConfig.Binance) &&
		exchangeCredentialsComplete("Bitget", c.ExchangeConfig.Bitget) &&
		exchangeCredentialsComplete("Bingx", c.ExchangeConfig.Bingx) &&
		exchangeCredentialsComplete(deepcoinName, c.ExchangeConfig.Deepcoin) &&
		exchangeCredentialsComplete(okxName, c.ExchangeConfig.Okx) &&
		exchangeCredentialsComplete(kucoinName, c.ExchangeConfig.Kucoin) &&
		exchangeCredentialsComplete("Toobit", c.ExchangeConfig.Toobit) &&
		notificationCredentialsComplete(c.NotiConfig)
}

func exchangeCredentialsComplete(name string, c APIConfig) bool {
	if !c.Enable {
		return true
	}
	if (name == kucoinName || name == okxName || name == deepcoinName) && c.APIPassphrase == "" {
		return false
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

	bybitKey, _ := loader.GetSecret("BYBIT_API_KEY")
	bybitSecret, _ := loader.GetSecret("BYBIT_API_SECRET")

	binanceKey, _ := loader.GetSecret("BINANCE_API_KEY")
	binanceSecret, _ := loader.GetSecret("BINANCE_API_SECRET")

	bitgetKey, _ := loader.GetSecret("BITGET_API_KEY")
	bitgetSecret, _ := loader.GetSecret("BITGET_API_SECRET")

	kucoinKey, _ := loader.GetSecret("KUCOIN_API_KEY")
	kucoinSecret, _ := loader.GetSecret("KUCOIN_API_SECRET")
	kucoinPassphrase, _ := loader.GetSecret("KUCOIN_API_PASSPHRASE")
	bingxKey, _ := loader.GetSecret("BINGX_API_KEY")
	bingxSecret, _ := loader.GetSecret("BINGX_API_SECRET")

	okxKey, _ := loader.GetSecret("OKX_API_KEY")
	okxSecret, _ := loader.GetSecret("OKX_API_SECRET")
	okxPassphrase, _ := loader.GetSecret("OKX_API_PASSPHRASE")
	deepcoinKey, _ := loader.GetSecret("DEEPCOIN_API_KEY")
	deepcoinSecret, _ := loader.GetSecret("DEEPCOIN_API_SECRET")
	deepcoinPassphrase, _ := loader.GetSecret("DEEPCOIN_API_PASSPHRASE")
	toobitKey, _ := loader.GetSecret("TOOBIT_API_KEY")
	toobitSecret, _ := loader.GetSecret("TOOBIT_API_SECRET")

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
	bybitKey = strings.TrimSpace(bybitKey)
	bybitSecret = strings.TrimSpace(bybitSecret)
	binanceKey = strings.TrimSpace(binanceKey)
	binanceSecret = strings.TrimSpace(binanceSecret)
	bitgetKey = strings.TrimSpace(bitgetKey)
	bitgetSecret = strings.TrimSpace(bitgetSecret)
	kucoinKey = strings.TrimSpace(kucoinKey)
	kucoinSecret = strings.TrimSpace(kucoinSecret)
	kucoinPassphrase = strings.TrimSpace(kucoinPassphrase)
	bingxKey = strings.TrimSpace(bingxKey)
	bingxSecret = strings.TrimSpace(bingxSecret)
	okxKey = strings.TrimSpace(okxKey)
	okxSecret = strings.TrimSpace(okxSecret)
	okxPassphrase = strings.TrimSpace(okxPassphrase)
	deepcoinKey = strings.TrimSpace(deepcoinKey)
	deepcoinSecret = strings.TrimSpace(deepcoinSecret)
	deepcoinPassphrase = strings.TrimSpace(deepcoinPassphrase)
	toobitKey = strings.TrimSpace(toobitKey)
	toobitSecret = strings.TrimSpace(toobitSecret)
	telegramChatID = strings.TrimSpace(telegramChatID)
	telegramBotToken = strings.TrimSpace(telegramBotToken)

	return &bitwardenCredentials{
		MEXCAPIKey:            apiKey,
		MEXCAPISecret:         apiSecret,
		GateAPIKey:            gateKey,
		GateAPISecret:         gateSecret,
		BybitAPIKey:           bybitKey,
		BybitAPISecret:        bybitSecret,
		BinanceAPIKey:         binanceKey,
		BinanceAPISecret:      binanceSecret,
		BitgetAPIKey:          bitgetKey,
		BitgetAPISecret:       bitgetSecret,
		KucoinAPIKey:          kucoinKey,
		KucoinAPISecret:       kucoinSecret,
		KucoinPassphrase:      kucoinPassphrase,
		BingxAPIKey:           bingxKey,
		BingxAPISecret:        bingxSecret,
		OkxAPIKey:             okxKey,
		OkxAPISecret:          okxSecret,
		OkxAPIPassphrase:      okxPassphrase,
		DeepcoinAPIKey:        deepcoinKey,
		DeepcoinAPISecret:     deepcoinSecret,
		DeepcoinAPIPassphrase: deepcoinPassphrase,
		ToobitAPIKey:          toobitKey,
		ToobitAPISecret:       toobitSecret,
		TelegramChatID:        telegramChatID,
		TelegramBotToken:      telegramBotToken,
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
	if fl.StructFieldName() == "Bybit" && !IsSupportedBybitAccountType(cfg.AccountType) {
		return false
	}
	if (fl.StructFieldName() == "Kucoin" || fl.StructFieldName() == okxName || fl.StructFieldName() == "Deepcoin") && cfg.APIPassphrase == "" {
		return false
	}
	if !isValidURL(cfg.Future.BaseURL) {
		return false
	}
	if !isValidURL(cfg.WebSocket.PublicEndpoint()) || !isValidURL(cfg.WebSocket.PrivateEndpoint()) {
		return false
	}
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return false
	}
	return true
}

func isValidURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	_, err := url.ParseRequestURI(rawURL)
	return err == nil
}

func applySystemDefaults(c *SystemConfig) {
	applyExchangeWSDefaults(&c.ExchangeConfig.Mexc)
	applyExchangeWSDefaults(&c.ExchangeConfig.Gate)
	applyExchangeWSDefaults(&c.ExchangeConfig.Bybit)
	applyExchangeWSDefaults(&c.ExchangeConfig.Binance)
	applyExchangeWSDefaults(&c.ExchangeConfig.Okx)
	applyExchangeWSDefaults(&c.ExchangeConfig.Hyperliquid)
	applyExchangeWSDefaults(&c.ExchangeConfig.Bitget)
	applyExchangeWSDefaults(&c.ExchangeConfig.Kucoin)
	applyExchangeWSDefaults(&c.ExchangeConfig.Bingx)
	applyExchangeWSDefaults(&c.ExchangeConfig.Deepcoin)
	applyExchangeWSDefaults(&c.ExchangeConfig.Toobit)
	if c.Env == "" {
		c.Env = "dev"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.APIServer.Port == 0 {
		c.APIServer.Port = 3100
	}
	if c.APIServer.Host == "" {
		c.APIServer.Host = "0.0.0.0"
	}
}

func applyExchangeWSDefaults(cfg *APIConfig) {
	if cfg.Future.BaseURL != "" && cfg.WebSocket.MaxPairsPerWSConn <= 0 {
		cfg.WebSocket.MaxPairsPerWSConn = 30
	}
}

// bitwardenCredentials holds API credentials from Bitwarden.
type bitwardenCredentials struct {
	MEXCAPIKey            string
	MEXCAPISecret         string
	GateAPIKey            string
	GateAPISecret         string
	BybitAPIKey           string
	BybitAPISecret        string
	BinanceAPIKey         string
	BinanceAPISecret      string
	BitgetAPIKey          string
	BitgetAPISecret       string
	KucoinAPIKey          string
	KucoinAPISecret       string
	KucoinPassphrase      string
	BingxAPIKey           string
	BingxAPISecret        string
	OkxAPIKey             string
	OkxAPISecret          string
	OkxAPIPassphrase      string
	DeepcoinAPIKey        string
	DeepcoinAPISecret     string
	DeepcoinAPIPassphrase string
	ToobitAPIKey          string
	ToobitAPISecret       string
	TelegramChatID        string
	TelegramBotToken      string
}

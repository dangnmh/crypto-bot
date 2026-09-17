package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	pkgconfig "crypto-bot/pkg/config"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

// InitializeBase loads environment variables, injects credentials,
// and applies universal default values to the core SystemConfig.
// This function should be called by any bot immediately after parsing its JSON configuration.
func InitializeBase(c *SystemConfig) error {
	_ = godotenv.Load()

	if c.ExchangeConfig == nil {
		c.ExchangeConfig = make(ExchangeConfig)
	}

	c.NotiConfig.TelegramChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	c.NotiConfig.TelegramCriticalChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CRITICAL_CHAT_ID"))
	c.NotiConfig.TelegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))

	applySystemDefaults(c)

	// Ensure at least one active exchange is enabled
	if !c.HasEnabledExchange() {
		return fmt.Errorf("at least one active exchange must be enabled")
	}

	validate := validator.New()
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("system config validation failed: %w", err)
	}

	// Validate the exchange configuration map
	if err := ValidateExchangeConfig(c.ExchangeConfig); err != nil {
		return fmt.Errorf("exchange config validation failed: %w", err)
	}

	return nil
}

// ValidateAPIConfigField is a stub for the legacy api_config tag validator.
func ValidateAPIConfigField(fl validator.FieldLevel) bool {
	return true
}

// ValidateExchangeConfig checks the validity of each enabled exchange configuration in the map.
func ValidateExchangeConfig(m ExchangeConfig) error {
	for name := range m {
		ep := m[name]
		if !IsSupportedExchange(name) {
			return fmt.Errorf("unsupported exchange configured: %s", name)
		}
		if !ep.IsEnabled() {
			continue
		}
		spec, _ := GetExchangeSpec(name)
		if err := validateSingleExchangeConfig(name, ep, spec); err != nil {
			return err
		}
	}
	return nil
}

func validateSingleExchangeConfig(name string, ep EndpointConfig, spec ExchangeSpec) error {
	if spec.Validate != nil {
		if err := spec.Validate(ep); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return validateEndpoint(name, ep)
}

func validateEndpoint(name string, ep EndpointConfig) error {
	if ep.TradeMode == "" {
		ep.TradeMode = DefaultTradeMode
	}
	if ep.TradeURL == "" && ep.WebSocket.TradeURL != "" {
		ep.TradeURL = ep.WebSocket.TradeURL
	}
	if ep.WebSocket.TradeURL == "" && ep.TradeURL != "" {
		ep.WebSocket.TradeURL = ep.TradeURL
	}

	validate := validator.New()
	if err := validate.Struct(ep); err != nil {
		return fmt.Errorf("%s: endpoint validation failed: %w", name, err)
	}
	if !isValidURL(ep.BaseURL) {
		return fmt.Errorf("%s: invalid base URL: %s", name, ep.BaseURL)
	}
	if !isValidURL(ep.WebSocket.PublicEndpoint()) {
		return fmt.Errorf("%s: invalid websocket endpoint URL", name)
	}
	if ep.WebSocket.PrivateEndpoint() != "" && !isValidURL(ep.WebSocket.PrivateEndpoint()) {
		return fmt.Errorf("%s: invalid websocket endpoint URL", name)
	}
	if ep.TradeMode == TradeModeWS && !isValidURL(ep.TradeEndpoint()) {
		return fmt.Errorf("%s: invalid trade websocket URL: %s", name, ep.TradeEndpoint())
	}
	return nil
}

func isValidURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	_, err := url.ParseRequestURI(rawURL)
	return err == nil
}

func applySystemDefaults(c *SystemConfig) {
	for name := range c.ExchangeConfig {
		ep := c.ExchangeConfig[name]
		applyEndpointWSDefaults(&ep)
		c.ExchangeConfig[name] = ep
	}
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

func applyEndpointWSDefaults(ep *EndpointConfig) {
	if ep.TradeMode == "" {
		ep.TradeMode = DefaultTradeMode
	}
	if ep.TradeURL == "" && ep.WebSocket.TradeURL != "" {
		ep.TradeURL = ep.WebSocket.TradeURL
	}
	if ep.WebSocket.TradeURL == "" && ep.TradeURL != "" {
		ep.WebSocket.TradeURL = ep.TradeURL
	}
	if ep.BaseURL != "" && ep.WebSocket.MaxPairsPerWSConn <= 0 {
		ep.WebSocket.MaxPairsPerWSConn = 30
	}
}

// LoadAccountCredentials resolves and injects environment variables into the given AccountConfig.
func LoadAccountCredentials(account *AccountConfig) error {
	if account == nil {
		return fmt.Errorf("account config is nil")
	}
	if !account.Enabled {
		return nil
	}

	if account.Env.APIKey == "" {
		return fmt.Errorf("account %q: missing env.apiKey mapping", account.ID)
	}
	if account.Env.APISecret == "" {
		return fmt.Errorf("account %q: missing env.apiSecret mapping", account.ID)
	}

	apiKey := strings.TrimSpace(os.Getenv(account.Env.APIKey))
	if apiKey == "" {
		return fmt.Errorf("account %q: environment variable %q is not set or empty", account.ID, account.Env.APIKey)
	}
	apiSecret := strings.TrimSpace(os.Getenv(account.Env.APISecret))
	if apiSecret == "" {
		return fmt.Errorf("account %q: environment variable %q is not set or empty", account.ID, account.Env.APISecret)
	}

	account.APIKey = apiKey
	account.APISecret = apiSecret

	if account.Env.Passphrase != "" {
		account.APIPassphrase = strings.TrimSpace(os.Getenv(account.Env.Passphrase))
	}

	spec, ok := GetExchangeSpec(account.Exchange)
	if ok && spec.RequiresPassphrase && account.APIPassphrase == "" {
		return fmt.Errorf("account %q: exchange %q requires passphrase, but passphrase is empty (checked env %q)", account.ID, account.Exchange, account.Env.Passphrase)
	}

	return nil
}

// LoadAccountsManifest loads an accounts manifest from a JSON/JSONC file and resolves credentials for all enabled accounts.
func LoadAccountsManifest(path string) (*AccountsManifest, error) {
	manifest, err := pkgconfig.Load[AccountsManifest](path)
	if err != nil {
		return nil, fmt.Errorf("load accounts manifest %s: %w", path, err)
	}

	validate := validator.New()
	if err := validate.Struct(manifest); err != nil {
		return nil, fmt.Errorf("validate accounts manifest: %w", err)
	}

	seenIDs := make(map[string]bool)
	for i := range manifest.Accounts {
		acc := &manifest.Accounts[i]
		if seenIDs[acc.ID] {
			return nil, fmt.Errorf("duplicate account ID %q in accounts manifest", acc.ID)
		}
		seenIDs[acc.ID] = true

		if err := LoadAccountCredentials(acc); err != nil {
			return nil, err
		}
	}

	return manifest, nil
}

package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

type bitwardenSecretLoader interface {
	GetSecret(secretKey string) (string, error)
}

var newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
	return NewBitwardenLoader()
}

// InitializeBase loads environment variables, injects credentials,
// and applies universal default values to the core SystemConfig.
// This function should be called by any bot immediately after parsing its JSON configuration.
func InitializeBase(c *SystemConfig) error {
	_ = godotenv.Load()

	if c.ExchangeConfig == nil {
		c.ExchangeConfig = make(ExchangeConfig)
	}
	// Pre-populate missing supported exchanges to ensure they exist
	for _, exch := range SupportedExchanges {
		if _, ok := c.ExchangeConfig[exch]; !ok {
			c.ExchangeConfig[exch] = APIConfig{}
		}
	}

	// Load credentials dynamically from environment variables
	for _, exch := range SupportedExchanges {
		apiCfg := c.ExchangeConfig[exch]
		upperExch := strings.ToUpper(exch)

		if key := strings.TrimSpace(os.Getenv(upperExch + "_API_KEY")); key != "" {
			apiCfg.APIKey = key
		}
		if secret := strings.TrimSpace(os.Getenv(upperExch + "_API_SECRET")); secret != "" {
			apiCfg.APISecret = secret
		}
		if passphrase := strings.TrimSpace(os.Getenv(upperExch + "_API_PASSPHRASE")); passphrase != "" {
			apiCfg.APIPassphrase = passphrase
		}
		c.ExchangeConfig[exch] = apiCfg
	}

	c.NotiConfig.TelegramChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	c.NotiConfig.TelegramCriticalChatID = strings.TrimSpace(os.Getenv("TELEGRAM_CRITICAL_CHAT_ID"))
	c.NotiConfig.TelegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))

	if err := applyBitwardenFallback(c); err != nil {
		return err
	}

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

// HasEnabledExchange checks if at least one exchange configuration is enabled.
func (c *SystemConfig) HasEnabledExchange() bool {
	for name := range c.ExchangeConfig {
		if c.ExchangeConfig[name].IsEnabled() {
			return true
		}
	}
	return false
}

func fallbackExchangeSecrets(c *SystemConfig, loader bitwardenSecretLoader) {
	for _, exch := range SupportedExchanges {
		apiCfg := c.ExchangeConfig[exch]
		if apiCfg.IsEnabled() {
			fallbackSingleExchangeSecret(exch, &apiCfg, loader)
			c.ExchangeConfig[exch] = apiCfg
		}
	}
}

func fallbackSingleExchangeSecret(name string, apiCfg *APIConfig, loader bitwardenSecretLoader) {
	upperName := strings.ToUpper(name)
	if apiCfg.APIKey == "" {
		if val, err := loader.GetSecret(upperName + "_API_KEY"); err == nil && val != "" {
			apiCfg.APIKey = strings.TrimSpace(val)
		}
	}
	if apiCfg.APISecret == "" {
		if val, err := loader.GetSecret(upperName + "_API_SECRET"); err == nil && val != "" {
			apiCfg.APISecret = strings.TrimSpace(val)
		}
	}
	spec := ExchangeSpecs[name]
	if spec.RequiresPassphrase && apiCfg.APIPassphrase == "" {
		if val, err := loader.GetSecret(upperName + "_API_PASSPHRASE"); err == nil && val != "" {
			apiCfg.APIPassphrase = strings.TrimSpace(val)
		}
	}
}

func applyBitwardenFallback(c *SystemConfig) error {
	if bitwardenFallbackNotNeeded(c) {
		return nil
	}

	if !hasBitwardenConfig() {
		return nil
	}

	loader, err := newBitwardenSecretLoader()
	if err != nil {
		return fmt.Errorf("bitwarden fallback failed: %w", err)
	}

	fallbackExchangeSecrets(c, loader)

	if c.NotiConfig.TelegramChatID == "" {
		if val, err := loader.GetSecret("TELEGRAM_CHAT_ID"); err == nil && val != "" {
			c.NotiConfig.TelegramChatID = strings.TrimSpace(val)
		}
	}
	if c.NotiConfig.TelegramCriticalChatID == "" {
		if val, err := loader.GetSecret("TELEGRAM_CRITICAL_CHAT_ID"); err == nil && val != "" {
			c.NotiConfig.TelegramCriticalChatID = strings.TrimSpace(val)
		}
	}
	if c.NotiConfig.TelegramBotToken == "" {
		if val, err := loader.GetSecret("TELEGRAM_BOT_TOKEN"); err == nil && val != "" {
			c.NotiConfig.TelegramBotToken = strings.TrimSpace(val)
		}
	}

	return nil
}

func bitwardenFallbackNotNeeded(c *SystemConfig) bool {
	for name := range c.ExchangeConfig {
		if !exchangeCredentialsComplete(name, c.ExchangeConfig[name]) {
			return false
		}
	}
	return notificationCredentialsComplete(c.NotiConfig)
}

func exchangeCredentialsComplete(name string, c APIConfig) bool {
	if !c.IsEnabled() {
		return true
	}
	spec := ExchangeSpecs[name]
	if spec.RequiresPassphrase && c.APIPassphrase == "" {
		return false
	}
	return c.APIKey != "" && c.APISecret != ""
}

func notificationCredentialsComplete(c NotiConfig) bool {
	return c.TelegramChatID != "" && c.TelegramBotToken != ""
}

// hasBitwardenConfig checks if Bitwarden environment variables are set.
func hasBitwardenConfig() bool {
	return os.Getenv("BITWARDEN_ACCESS_TOKEN") != "" &&
		os.Getenv("BITWARDEN_ORGANIZATION_ID") != "" &&
		os.Getenv("BITWARDEN_PROJECT_NAME") != ""
}

// ValidateAPIConfigField is a stub for the legacy api_config tag validator.
func ValidateAPIConfigField(fl validator.FieldLevel) bool {
	return true
}

// ValidateExchangeConfig checks the validity of each enabled exchange configuration in the map.
func ValidateExchangeConfig(m ExchangeConfig) error {
	for name := range m {
		cfg := m[name]
		spec, supported := ExchangeSpecs[name]
		if !supported {
			return fmt.Errorf("unsupported exchange configured: %s", name)
		}
		if !cfg.IsEnabled() {
			continue
		}
		if err := validateSingleExchangeConfig(name, cfg, spec); err != nil {
			return err
		}
	}
	return nil
}

func validateSingleExchangeConfig(name string, cfg APIConfig, spec ExchangeSpec) error {
	if spec.RequiresPassphrase && cfg.APIPassphrase == "" {
		return fmt.Errorf("%s: API passphrase is required", name)
	}
	if spec.Validate != nil {
		if err := spec.Validate(cfg); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	futureEP := cfg.GetFutureEndpoint()
	spotEP := cfg.GetSpotEndpoint()

	if cfg.Future != nil && (cfg.Future.Enable || cfg.Spot == nil) {
		if err := validateEndpoint(name, futureEP); err != nil {
			return err
		}
	}
	if cfg.Spot != nil && (cfg.Spot.Enable || cfg.Future == nil) {
		if err := validateEndpoint(name, spotEP); err != nil {
			return err
		}
	}
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return fmt.Errorf("%s: API key and secret are required", name)
	}
	return nil
}

func validateEndpoint(name string, ep EndpointConfig) error {
	if !isValidURL(ep.BaseURL) {
		return fmt.Errorf("%s: invalid base URL: %s", name, ep.BaseURL)
	}
	if !isValidURL(ep.WebSocket.PublicEndpoint()) {
		return fmt.Errorf("%s: invalid websocket endpoint URL", name)
	}
	if ep.WebSocket.PrivateEndpoint() != "" && !isValidURL(ep.WebSocket.PrivateEndpoint()) {
		return fmt.Errorf("%s: invalid websocket endpoint URL", name)
	}
	return nil
}

func IsSupportedExchange(name string) bool {
	if slices.Contains(SupportedExchanges, name) {
		return true
	}
	baseName := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(name), "_spot"), "_futures")
	return slices.Contains(SupportedExchanges, baseName)
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
		apiCfg := c.ExchangeConfig[name]
		applyExchangeWSDefaults(&apiCfg)
		c.ExchangeConfig[name] = apiCfg
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

func applyExchangeWSDefaults(cfg *APIConfig) {
	if cfg.Future != nil && cfg.Future.BaseURL != "" && cfg.Future.WebSocket.MaxPairsPerWSConn <= 0 {
		cfg.Future.WebSocket.MaxPairsPerWSConn = 30
	}
	if cfg.Spot != nil && cfg.Spot.BaseURL != "" && cfg.Spot.WebSocket.MaxPairsPerWSConn <= 0 {
		cfg.Spot.WebSocket.MaxPairsPerWSConn = 30
	}
}

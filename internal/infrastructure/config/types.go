package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"crypto-bot/pkg/types"
)

// SyncConfig holds intervals for various background synchronization tasks.
type SyncConfig struct {
	Ticker   types.Duration `json:"ticker"`
	Time     types.Duration `json:"time"`
	Contract types.Duration `json:"contract"`
}

type WebSocketConfig struct {
	PublicURL         string `json:"publicURL"`
	MarketURL         string `json:"marketURL,omitempty"`
	PrivateURL        string `json:"privateURL"`
	TradeURL          string `json:"tradeURL,omitempty"`
	MaxPairsPerWSConn int    `json:"maxPairsPerWSConn"`
}

func (c WebSocketConfig) PublicEndpoint() string {
	return c.PublicURL
}

func (c WebSocketConfig) MarketEndpoint() string {
	if c.MarketURL != "" {
		return c.MarketURL
	}
	return c.PublicURL
}

func (c WebSocketConfig) PrivateEndpoint() string {
	return c.PrivateURL
}

func (c WebSocketConfig) TradeEndpoint() string {
	return c.TradeURL
}

const (
	TradeModeHTTP    = "http"
	TradeModeWS      = "ws"
	DefaultTradeMode = TradeModeHTTP
)

// EndpointConfig defines connection parameters for an exchange endpoint.
type EndpointConfig struct {
	Enable      bool            `json:"enable"`
	BaseURL     string          `json:"baseURL"`
	AccountType string          `json:"accountType,omitempty"`
	TradeMode   string          `json:"tradeMode,omitempty" validate:"omitempty,oneof=http ws"`
	TradeURL    string          `json:"tradeURL,omitempty" validate:"required_if=TradeMode ws"`
	WebSocket   WebSocketConfig `json:"websocket"`

	// Resolved credentials populated at runtime from environment variables or account configs
	APIKey        string `json:"-"`
	APISecret     string `json:"-"`
	APIPassphrase string `json:"-"`
}

func (e EndpointConfig) IsEnabled() bool {
	return e.Enable
}

func (e EndpointConfig) GetTradeMode() string {
	if e.TradeMode == "" {
		return DefaultTradeMode
	}
	return e.TradeMode
}

func (e EndpointConfig) TradeEndpoint() string {
	if e.TradeURL != "" {
		return e.TradeURL
	}
	return e.WebSocket.TradeURL
}

func (e EndpointConfig) GetSpotEndpoint() EndpointConfig {
	return e
}

func (e EndpointConfig) GetFutureEndpoint() EndpointConfig {
	return e
}

func (e *EndpointConfig) UnmarshalJSON(data []byte) error {
	type Alias EndpointConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if e.TradeMode == "" {
		e.TradeMode = DefaultTradeMode
	}
	if e.TradeURL == "" && e.WebSocket.TradeURL != "" {
		e.TradeURL = e.WebSocket.TradeURL
	}
	if e.WebSocket.TradeURL == "" && e.TradeURL != "" {
		e.WebSocket.TradeURL = e.TradeURL
	}
	return nil
}

// APIConfig is an alias for EndpointConfig representing exact-name endpoint configs.
type APIConfig = EndpointConfig
type RESTConfig = EndpointConfig

const (
	BybitAccountTypeStandard = "standard"
	BybitAccountTypeUnified  = "unified"
)

func NormalizeBybitAccountType(raw string) string {
	accountType := strings.ToLower(strings.TrimSpace(raw))
	if accountType == "" {
		return BybitAccountTypeStandard
	}
	return accountType
}

func IsSupportedBybitAccountType(raw string) bool {
	switch NormalizeBybitAccountType(raw) {
	case BybitAccountTypeStandard, BybitAccountTypeUnified:
		return true
	default:
		return false
	}
}

type LogWSConfig struct {
	Ticker   bool `json:"ticker"`
	Order    bool `json:"order"`
	Position bool `json:"position"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level string      `json:"level"`
	HTTP  bool        `json:"http"`
	WS    LogWSConfig `json:"ws"`
}

type APIServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// SystemConfig contains universally required configuration for any bot connecting to the exchange.
type SystemConfig struct {
	Env            string          `json:"env"`
	Logging        LoggingConfig   `json:"logging"`
	DryRun         bool            `json:"dryRun"`
	ExchangeConfig ExchangeConfig  `json:"-"`
	NotiConfig     NotiConfig      `json:"notifier"`
	APIServer      APIServerConfig `json:"api_server"`
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

const (
	MexcName        = "mexc"
	GateName        = "gate"
	BybitName       = "bybit"
	BinanceName     = "binance"
	OkxName         = "okx"
	HyperliquidName = "hyperliquid"
	BitgetName      = "bitget"
	KucoinName      = "kucoin"
	BingxName       = "bingx"
	DeepcoinName    = "deepcoin"
	ToobitName      = "toobit"
	BitmartName     = "bitmart"
	WeexName        = "weex"
	BitunixName     = "bitunix"
	XtName          = "xt"
	OrangexName     = "orangex"
	AsterName       = "aster"
	PionexName      = "pionex"
	HotcoinName     = "hotcoin"
)

type ExchangeSpec struct {
	RequiresPassphrase bool
	Validate           func(cfg EndpointConfig) error
}

var ExchangeSpecs = map[string]ExchangeSpec{
	MexcName:        {},
	GateName:        {},
	BinanceName:     {},
	HyperliquidName: {},
	BitgetName:      {RequiresPassphrase: true},
	BingxName:       {},
	ToobitName:      {},
	BitmartName:     {RequiresPassphrase: true},
	KucoinName:      {RequiresPassphrase: true},
	OkxName:         {RequiresPassphrase: true},
	DeepcoinName:    {RequiresPassphrase: true},
	WeexName:        {RequiresPassphrase: true},
	BitunixName:     {},
	XtName:          {},
	OrangexName:     {},
	AsterName:       {RequiresPassphrase: true},
	PionexName:      {},
	HotcoinName:     {},
	BybitName: {
		Validate: func(cfg EndpointConfig) error {
			if !IsSupportedBybitAccountType(cfg.AccountType) {
				return fmt.Errorf("unsupported account type: %s", cfg.AccountType)
			}
			return nil
		},
	},
}

// NormalizeExchangeName extracts the base exchange identifier (e.g. "mexc_futures" -> "mexc").
func NormalizeExchangeName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(strings.TrimSuffix(s, "_futures"), "_spot")
	return s
}

// GetExchangeSpec returns the ExchangeSpec for an exact or base exchange name.
func GetExchangeSpec(name string) (ExchangeSpec, bool) {
	spec, ok := ExchangeSpecs[NormalizeExchangeName(name)]
	return spec, ok
}

// SupportedExchanges contains the list of all supported base exchange identifiers.
var SupportedExchanges []string

func init() {
	SupportedExchanges = make([]string, 0, len(ExchangeSpecs))
	for k := range ExchangeSpecs {
		SupportedExchanges = append(SupportedExchanges, k)
	}
	slices.Sort(SupportedExchanges)
}

// IsSupportedExchange checks if a name corresponds to a supported exchange (exact or base name).
func IsSupportedExchange(name string) bool {
	_, ok := GetExchangeSpec(name)
	return ok
}

// ExchangeConfig maps exact exchange names (e.g. "mexc_futures", "bybit_futures") directly to EndpointConfig.
type ExchangeConfig map[string]EndpointConfig

type NotiConfig struct {
	Enabled                bool   `json:"enable"`
	TelegramChatID         string `json:"-"`
	TelegramCriticalChatID string `json:"-"`
	TelegramBotToken       string `json:"-"`
}

// AccountEnvMapping maps an account to the specific environment variable names providing its credentials.
type AccountEnvMapping struct {
	APIKey     string `json:"apiKey" validate:"required"`
	APISecret  string `json:"apiSecret" validate:"required"`
	Passphrase string `json:"passphrase,omitempty"`
}

// AccountScannersConfig defines scanners toggles for an account.
type AccountScannersConfig struct {
	Configured bool `json:"configured"`
	Schedule   bool `json:"schedule"`
}

// AccountConfig defines configuration for an individual exchange account.
type AccountConfig struct {
	ID          string                `json:"id" validate:"required"`
	Exchange    string                `json:"exchange" validate:"required"`
	Enabled     bool                  `json:"enabled"`
	OutboundIP  string                `json:"outboundIP,omitempty"`
	ProxyURL    string                `json:"proxyURL,omitempty"`
	AccountType string                `json:"accountType,omitempty"`
	Env         AccountEnvMapping     `json:"env"`
	Configs     map[string]string     `json:"configs,omitempty"`
	Scanners    AccountScannersConfig `json:"scanners"`

	// Resolved credentials populated at runtime from environment variables
	APIKey        string `json:"-"`
	APISecret     string `json:"-"`
	APIPassphrase string `json:"-"`
}

// AccountsManifest represents the root structure of accounts.jsonc.
type AccountsManifest struct {
	Accounts []AccountConfig `json:"accounts" validate:"dive"`
}

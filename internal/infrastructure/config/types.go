package config

import (
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
	WSURL             string `json:"wsURL"`
	PublicURL         string `json:"publicURL"`
	MarketURL         string `json:"marketURL"`
	PrivateURL        string `json:"privateURL"`
	MaxPairsPerWSConn int    `json:"maxPairsPerWSConn"`
}

func (c WebSocketConfig) PublicEndpoint() string {
	if c.PublicURL != "" {
		return c.PublicURL
	}
	return c.WSURL
}

func (c WebSocketConfig) MarketEndpoint() string {
	if c.MarketURL != "" {
		return c.MarketURL
	}
	return c.PublicEndpoint()
}

func (c WebSocketConfig) PrivateEndpoint() string {
	if c.PrivateURL != "" {
		return c.PrivateURL
	}
	return c.WSURL
}

type RESTConfig struct {
	BaseURL string `json:"baseURL"`
}

// APIConfig holds API connection parameters.
type APIConfig struct {
	Enable        bool            `json:"enable"`
	Future        RESTConfig      `json:"future"`
	WebSocket     WebSocketConfig `json:"websocket"`
	APIKey        string          `json:"-"`
	APISecret     string          `json:"-"`
	APIPassphrase string          `json:"-"`
	AccountType   string          `json:"accountType,omitempty"`
}

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
	Logging        LoggingConfig   `json:"logging"`
	DryRun         bool            `json:"dryRun"`
	ExchangeConfig ExchangeConfig  `json:"exchange"`
	NotiConfig     NotiConfig      `json:"notifier"`
	APIServer      APIServerConfig `json:"api_server"`
}

type ExchangeConfig struct {
	Mexc        APIConfig `json:"mexc" validate:"api_config"`
	Gate        APIConfig `json:"gate" validate:"api_config"`
	Bybit       APIConfig `json:"bybit" validate:"api_config"`
	Binance     APIConfig `json:"binance" validate:"api_config"`
	Okx         APIConfig `json:"okx" validate:"api_config"`
	Hyperliquid APIConfig `json:"hyperliquid" validate:"api_config"`
	Bitget      APIConfig `json:"bitget" validate:"api_config"`
	Kucoin      APIConfig `json:"kucoin" validate:"api_config"`
	Bingx       APIConfig `json:"bingx" validate:"api_config"`
}

type NotiConfig struct {
	Enabled          bool   `json:"enable"`
	TelegramChatID   string `json:"-"`
	TelegramBotToken string `json:"-"`
}

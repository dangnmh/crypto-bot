package config

import (
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
	Env            string          `json:"env"`
	Logging        LoggingConfig   `json:"logging"`
	DryRun         bool            `json:"dryRun"`
	ExchangeConfig ExchangeConfig  `json:"exchange"`
	NotiConfig     NotiConfig      `json:"notifier"`
	APIServer      APIServerConfig `json:"api_server"`
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
)

type ExchangeSpec struct {
	RequiresPassphrase bool
	Validate           func(cfg APIConfig) error
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
	BybitName: {
		Validate: func(cfg APIConfig) error {
			if !IsSupportedBybitAccountType(cfg.AccountType) {
				return fmt.Errorf("unsupported account type: %s", cfg.AccountType)
			}
			return nil
		},
	},
}

// SupportedExchanges contains the list of all supported exchange identifiers.
var SupportedExchanges []string

func init() {
	SupportedExchanges = make([]string, 0, len(ExchangeSpecs))
	for k := range ExchangeSpecs {
		SupportedExchanges = append(SupportedExchanges, k)
	}
	slices.Sort(SupportedExchanges)
}

// ExchangeConfig maps exchange names to their API configurations.
type ExchangeConfig map[string]APIConfig

type NotiConfig struct {
	Enabled          bool   `json:"enable"`
	TelegramChatID   string `json:"-"`
	TelegramBotToken string `json:"-"`
}

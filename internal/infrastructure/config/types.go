package config

import "crypto-bot/pkg/types"

// SyncConfig holds intervals for various background synchronization tasks.
type SyncConfig struct {
	Ticker   types.Duration `json:"ticker"`
	Time     types.Duration `json:"time"`
	Contract types.Duration `json:"contract"`
}

type WebSocketConfig struct {
	WSURL             string `json:"wsURL"`
	MaxPairsPerWSConn int    `json:"maxPairsPerWSConn"`
}

type RESTConfig struct {
	BaseURL string `json:"baseURL"`
}

// APIConfig holds API connection parameters.
type APIConfig struct {
	Future    RESTConfig      `json:"future"`
	WebSocket WebSocketConfig `json:"websocket"`
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

// SystemConfig contains universally required configuration for any bot connecting to the exchange.
type SystemConfig struct {
	API         APIConfig     `json:"api"`
	Logging     LoggingConfig `json:"logging"`
	Sync        SyncConfig    `json:"sync"`
	DryRun      bool          `json:"dryRun"`
	MetricsPort int           `json:"metricsPort"`
	APIKey      string        `json:"-"`
	APISecret   string        `json:"-"`
}

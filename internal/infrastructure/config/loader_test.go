package config_test

import (
	"os"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitializeBase_Success(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com"},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "test-key", cfg.ExchangeConfig.Mexc.APIKey)
	assert.Equal(t, "test-secret", cfg.ExchangeConfig.Mexc.APISecret)
}

func TestInitializeBase_Defaults(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com"},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	// Verify defaults are applied.
	assert.Greater(t, cfg.Sync.Time, types.Duration(0))
	assert.Greater(t, cfg.Sync.Ticker, types.Duration(0))
	assert.Greater(t, cfg.Sync.Contract, types.Duration(0))
	assert.Equal(t, 30, cfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestInitializeBase_NoOverrideExistingDefaults(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	customTime := types.Duration(60 * 1e9) // 60s
	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 50},
			},
		},
		Sync:    config.SyncConfig{Time: customTime},
		Logging: config.LoggingConfig{Level: "debug"},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, customTime, cfg.Sync.Time)
	assert.Equal(t, 50, cfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestInitializeBase_MissingAPIKey(t *testing.T) {
	_ = os.Unsetenv("MEXC_API_KEY")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com"},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_MissingAPISecret(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	_ = os.Unsetenv("MEXC_API_SECRET")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com"},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_MissingBaseURL(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: ""},
				WebSocket: config.WebSocketConfig{WSURL: "wss://ws.example.com"},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_MissingWSURL(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Mexc: config.APIConfig{
				Enable:    true,
				Future:    config.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: config.WebSocketConfig{WSURL: ""},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_SeparatePublicPrivateWSURLs(t *testing.T) {
	t.Setenv("BYBIT_API_KEY", "key")
	t.Setenv("BYBIT_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			Bybit: config.APIConfig{
				Enable: true,
				Future: config.RESTConfig{BaseURL: "https://api.bybit.com"},
				WebSocket: config.WebSocketConfig{
					PublicURL:  "wss://stream.bybit.com/v5/public/linear",
					PrivateURL: "wss://stream.bybit.com/v5/private",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "wss://stream.bybit.com/v5/public/linear", cfg.ExchangeConfig.Bybit.WebSocket.PublicEndpoint())
	assert.Equal(t, "wss://stream.bybit.com/v5/private", cfg.ExchangeConfig.Bybit.WebSocket.PrivateEndpoint())
	assert.Equal(t, 30, cfg.ExchangeConfig.Bybit.WebSocket.MaxPairsPerWSConn)
}

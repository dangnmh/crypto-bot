package config_test

import (
	"encoding/json"
	"os"
	"testing"

	"crypto-bot/internal/infrastructure/config"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitializeBase_Success(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
					},
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "test-key", cfg.ExchangeConfig["mexc"].APIKey)
	assert.Equal(t, "test-secret", cfg.ExchangeConfig["mexc"].APISecret)
}

func TestInitializeBase_Defaults(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
					},
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	// Verify defaults are applied.
	assert.Equal(t, 30, cfg.ExchangeConfig["mexc"].GetFutureEndpoint().WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestInitializeBase_NoOverrideExistingDefaults(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:         "wss://ws.example.com",
						PrivateURL:        "wss://ws.example.com",
						MaxPairsPerWSConn: 50,
					},
				},
			},
		},
		Logging: config.LoggingConfig{Level: "debug"},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, 50, cfg.ExchangeConfig["mexc"].GetFutureEndpoint().WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestInitializeBase_MissingAPIKey(t *testing.T) {
	_ = os.Unsetenv("MEXC_API_KEY")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
					},
				},
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
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
					},
				},
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
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
					},
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_MissingPublicURL(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.example.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "",
						PrivateURL: "wss://ws.example.com",
					},
				},
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
			"bybit": config.APIConfig{
				Future: &config.RESTConfig{
					Enable:  true,
					BaseURL: "https://api.bybit.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://stream.bybit.com/v5/public/linear",
						PrivateURL: "wss://stream.bybit.com/v5/private",
					},
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "wss://stream.bybit.com/v5/public/linear", cfg.ExchangeConfig["bybit"].GetFutureEndpoint().WebSocket.PublicEndpoint())
	assert.Equal(t, "wss://stream.bybit.com/v5/private", cfg.ExchangeConfig["bybit"].GetFutureEndpoint().WebSocket.PrivateEndpoint())
	assert.Equal(t, 30, cfg.ExchangeConfig["bybit"].GetFutureEndpoint().WebSocket.MaxPairsPerWSConn)
}

func TestEndpointConfig_TradeModeAndTradeURL(t *testing.T) {
	t.Parallel()

	ep := config.EndpointConfig{
		TradeMode: "ws",
		WebSocket: config.WebSocketConfig{
			TradeURL: "wss://stream.bybit.com/v5/trade",
		},
	}
	assert.Equal(t, "ws", ep.GetTradeMode())
	assert.Equal(t, "wss://stream.bybit.com/v5/trade", ep.WebSocket.TradeEndpoint())
	assert.Equal(t, "wss://stream.bybit.com/v5/trade", ep.TradeEndpoint())
}

func TestEndpointConfig_ValidationTags(t *testing.T) {
	t.Parallel()

	v := validator.New()

	tests := []struct {
		name      string
		ep        config.EndpointConfig
		wantError bool
		errTag    string
	}{
		{
			name: "http mode with empty tradeURL is valid",
			ep: config.EndpointConfig{
				TradeMode: "http",
				TradeURL:  "",
			},
			wantError: false,
		},
		{
			name: "ws mode with valid tradeURL is valid",
			ep: config.EndpointConfig{
				TradeMode: "ws",
				TradeURL:  "wss://ws-fapi.binance.com/ws-fapi/v1",
			},
			wantError: false,
		},
		{
			name: "ws mode with empty tradeURL fails required_if validation",
			ep: config.EndpointConfig{
				TradeMode: "ws",
				TradeURL:  "",
			},
			wantError: true,
			errTag:    "required_if",
		},
		{
			name: "invalid tradeMode fails oneof validation",
			ep: config.EndpointConfig{
				TradeMode: "ftp",
				TradeURL:  "",
			},
			wantError: true,
			errTag:    "oneof",
		},
		{
			name: "grpc tradeMode fails oneof validation even with tradeURL",
			ep: config.EndpointConfig{
				TradeMode: "grpc",
				TradeURL:  "wss://stream.bybit.com/v5/trade",
			},
			wantError: true,
			errTag:    "oneof",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.Struct(tt.ep)
			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errTag)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEndpointConfig_DefaultHTTPAndJSONSync(t *testing.T) {
	t.Parallel()

	// 1. Default HTTP when tradeMode is omitted
	var epDefault config.EndpointConfig
	err := json.Unmarshal([]byte(`{"enable": true, "baseURL": "https://api.binance.com"}`), &epDefault)
	require.NoError(t, err)
	assert.Equal(t, "http", epDefault.TradeMode)
	assert.Equal(t, "http", epDefault.GetTradeMode())

	// 2. Sync websocket.tradeURL to ep.TradeURL
	var epSyncWS config.EndpointConfig
	rawWS := `{"enable": true, "tradeMode": "ws", "websocket": {"tradeURL": "wss://ws-fapi.binance.com/ws-fapi/v1"}}`
	err = json.Unmarshal([]byte(rawWS), &epSyncWS)
	require.NoError(t, err)
	assert.Equal(t, "ws", epSyncWS.TradeMode)
	assert.Equal(t, "wss://ws-fapi.binance.com/ws-fapi/v1", epSyncWS.TradeURL)
	assert.Equal(t, "wss://ws-fapi.binance.com/ws-fapi/v1", epSyncWS.WebSocket.TradeURL)
	assert.Equal(t, "wss://ws-fapi.binance.com/ws-fapi/v1", epSyncWS.TradeEndpoint())

	// 3. Sync ep.tradeURL to ep.WebSocket.TradeURL
	var epSyncDirect config.EndpointConfig
	rawDirect := `{"enable": true, "tradeMode": "ws", "tradeURL": "wss://stream.bybit.com/v5/trade"}`
	err = json.Unmarshal([]byte(rawDirect), &epSyncDirect)
	require.NoError(t, err)
	assert.Equal(t, "ws", epSyncDirect.TradeMode)
	assert.Equal(t, "wss://stream.bybit.com/v5/trade", epSyncDirect.TradeURL)
	assert.Equal(t, "wss://stream.bybit.com/v5/trade", epSyncDirect.WebSocket.TradeURL)
	assert.Equal(t, "wss://stream.bybit.com/v5/trade", epSyncDirect.TradeEndpoint())

	// 4. Empty struct GetTradeMode returns "http"
	emptyEP := config.EndpointConfig{}
	assert.Equal(t, "http", emptyEP.GetTradeMode())
}

func TestValidateExchangeConfig_TradeModeBybitAndBinance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exchange  string
		tradeMode string
		tradeURL  string
		wantError bool
	}{
		{
			name:      "bybit ws mode with tradeURL is valid",
			exchange:  "bybit",
			tradeMode: "ws",
			tradeURL:  "wss://stream.bybit.com/v5/trade",
			wantError: false,
		},
		{
			name:      "bybit ws mode without tradeURL fails validation",
			exchange:  "bybit",
			tradeMode: "ws",
			tradeURL:  "",
			wantError: true,
		},
		{
			name:      "bybit http mode without tradeURL is valid",
			exchange:  "bybit",
			tradeMode: "http",
			tradeURL:  "",
			wantError: false,
		},
		{
			name:      "binance ws mode with tradeURL is valid",
			exchange:  "binance",
			tradeMode: "ws",
			tradeURL:  "wss://ws-fapi.binance.com/ws-fapi/v1",
			wantError: false,
		},
		{
			name:      "binance ws mode without tradeURL fails validation",
			exchange:  "binance",
			tradeMode: "ws",
			tradeURL:  "",
			wantError: true,
		},
		{
			name:      "binance http mode without tradeURL is valid",
			exchange:  "binance",
			tradeMode: "http",
			tradeURL:  "",
			wantError: false,
		},
		{
			name:      "binance with invalid tradeMode fails validation",
			exchange:  "binance",
			tradeMode: "invalid_mode",
			tradeURL:  "wss://ws-fapi.binance.com/ws-fapi/v1",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ExchangeConfig{
				tt.exchange: config.APIConfig{
					APIKey:    "test-key",
					APISecret: "test-secret",
					Future: &config.RESTConfig{
						Enable:    true,
						BaseURL:   "https://api.example.com",
						TradeMode: tt.tradeMode,
						WebSocket: config.WebSocketConfig{
							PublicURL:  "wss://ws.example.com",
							PrivateURL: "wss://ws.example.com",
							TradeURL:   tt.tradeURL,
						},
					},
				},
			}

			err := config.ValidateExchangeConfig(cfg)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

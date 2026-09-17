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
	t.Parallel()

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "https://api.example.com",
				WebSocket: config.WebSocketConfig{
					PublicURL:  "wss://ws.example.com",
					PrivateURL: "wss://ws.example.com",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "dev", cfg.Env)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, 3100, cfg.APIServer.Port)
}

func TestInitializeBase_Defaults(t *testing.T) {
	t.Parallel()

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "https://api.example.com",
				WebSocket: config.WebSocketConfig{
					PublicURL:  "wss://ws.example.com",
					PrivateURL: "wss://ws.example.com",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	// Verify defaults are applied.
	assert.Equal(t, 30, cfg.ExchangeConfig["mexc_futures"].WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestInitializeBase_NoOverrideExistingDefaults(t *testing.T) {
	t.Parallel()

	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "https://api.example.com",
				WebSocket: config.WebSocketConfig{
					PublicURL:         "wss://ws.example.com",
					PrivateURL:        "wss://ws.example.com",
					MaxPairsPerWSConn: 50,
				},
			},
		},
		Logging: config.LoggingConfig{Level: "debug"},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, 50, cfg.ExchangeConfig["mexc_futures"].WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestInitializeBase_MissingBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "",
				WebSocket: config.WebSocketConfig{
					PublicURL:  "wss://ws.example.com",
					PrivateURL: "wss://ws.example.com",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_MissingPublicURL(t *testing.T) {
	t.Parallel()
	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "https://api.example.com",
				WebSocket: config.WebSocketConfig{
					PublicURL:  "",
					PrivateURL: "wss://ws.example.com",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	assert.Error(t, err)
}

func TestInitializeBase_SeparatePublicPrivateWSURLs(t *testing.T) {
	t.Parallel()
	cfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"bybit_futures": config.EndpointConfig{
				Enable:  true,
				BaseURL: "https://api.bybit.com",
				WebSocket: config.WebSocketConfig{
					PublicURL:  "wss://stream.bybit.com/v5/public/linear",
					PrivateURL: "wss://stream.bybit.com/v5/private",
				},
			},
		},
	}

	err := config.InitializeBase(cfg)
	require.NoError(t, err)

	assert.Equal(t, "wss://stream.bybit.com/v5/public/linear", cfg.ExchangeConfig["bybit_futures"].WebSocket.PublicEndpoint())
	assert.Equal(t, "wss://stream.bybit.com/v5/private", cfg.ExchangeConfig["bybit_futures"].WebSocket.PrivateEndpoint())
	assert.Equal(t, 30, cfg.ExchangeConfig["bybit_futures"].WebSocket.MaxPairsPerWSConn)
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
			exchange:  "bybit_futures",
			tradeMode: "ws",
			tradeURL:  "wss://stream.bybit.com/v5/trade",
			wantError: false,
		},
		{
			name:      "bybit ws mode without tradeURL fails validation",
			exchange:  "bybit_futures",
			tradeMode: "ws",
			tradeURL:  "",
			wantError: true,
		},
		{
			name:      "bybit http mode without tradeURL is valid",
			exchange:  "bybit_futures",
			tradeMode: "http",
			tradeURL:  "",
			wantError: false,
		},
		{
			name:      "binance ws mode with tradeURL is valid",
			exchange:  "binance_futures",
			tradeMode: "ws",
			tradeURL:  "wss://ws-fapi.binance.com/ws-fapi/v1",
			wantError: false,
		},
		{
			name:      "binance ws mode without tradeURL fails validation",
			exchange:  "binance_futures",
			tradeMode: "ws",
			tradeURL:  "",
			wantError: true,
		},
		{
			name:      "binance http mode without tradeURL is valid",
			exchange:  "binance_futures",
			tradeMode: "http",
			tradeURL:  "",
			wantError: false,
		},
		{
			name:      "binance with invalid tradeMode fails validation",
			exchange:  "binance_futures",
			tradeMode: "invalid_mode",
			tradeURL:  "wss://ws-fapi.binance.com/ws-fapi/v1",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ExchangeConfig{
				tt.exchange: config.EndpointConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					TradeMode: tt.tradeMode,
					TradeURL:  tt.tradeURL,
					WebSocket: config.WebSocketConfig{
						PublicURL:  "wss://ws.example.com",
						PrivateURL: "wss://ws.example.com",
						TradeURL:   tt.tradeURL,
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

//nolint:paralleltest,gosec // mock test keys mutate process environment with t.Setenv
func TestLoadAccountCredentials(t *testing.T) {
	t.Setenv("ENV_VAR_MOCK_1", "mexc-key-123")
	t.Setenv("ENV_VAR_MOCK_2", "mexc-secret-456")
	t.Setenv("ENV_VAR_MOCK_3", "okx-pass-789")

	t.Run("successful credential load without passphrase", func(t *testing.T) {
		acc := &config.AccountConfig{
			ID:       "mexc_main",
			Exchange: "mexc_futures",
			Enabled:  true,
			Env: config.AccountEnvMapping{
				APIKey:    "ENV_VAR_MOCK_1",
				APISecret: "ENV_VAR_MOCK_2",
			},
		}
		err := config.LoadAccountCredentials(acc)
		require.NoError(t, err)
		assert.Equal(t, "mexc-key-123", acc.APIKey)
		assert.Equal(t, "mexc-secret-456", acc.APISecret)
	})

	t.Run("successful credential load with passphrase", func(t *testing.T) {
		acc := &config.AccountConfig{
			ID:       "okx_sub1",
			Exchange: "okx_futures",
			Enabled:  true,
			Env: config.AccountEnvMapping{
				APIKey:     "ENV_VAR_MOCK_1",
				APISecret:  "ENV_VAR_MOCK_2",
				Passphrase: "ENV_VAR_MOCK_3",
			},
		}
		err := config.LoadAccountCredentials(acc)
		require.NoError(t, err)
		assert.Equal(t, "okx-pass-789", acc.APIPassphrase)
	})

	t.Run("fails when passphrase missing for exchange requiring it", func(t *testing.T) {
		acc := &config.AccountConfig{
			ID:       "okx_sub2",
			Exchange: "okx_futures",
			Enabled:  true,
			Env: config.AccountEnvMapping{
				APIKey:    "ENV_VAR_MOCK_1",
				APISecret: "ENV_VAR_MOCK_2",
			},
		}
		err := config.LoadAccountCredentials(acc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires passphrase")
	})

	t.Run("fails when mapped env variable is unset", func(t *testing.T) {
		acc := &config.AccountConfig{
			ID:       "mexc_missing",
			Exchange: "mexc_futures",
			Enabled:  true,
			Env: config.AccountEnvMapping{
				APIKey:    "NON_EXISTENT_KEY",
				APISecret: "ENV_VAR_MOCK_2",
			},
		}
		err := config.LoadAccountCredentials(acc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "NON_EXISTENT_KEY")
	})

	t.Run("skips disabled account", func(t *testing.T) {
		acc := &config.AccountConfig{
			ID:       "mexc_disabled",
			Exchange: "mexc_futures",
			Enabled:  false,
			Env: config.AccountEnvMapping{
				APIKey:    "NON_EXISTENT_KEY",
				APISecret: "NON_EXISTENT_SECRET",
			},
		}
		err := config.LoadAccountCredentials(acc)
		require.NoError(t, err)
		assert.Empty(t, acc.APIKey)
	})
}

func TestLoadAccountsManifest(t *testing.T) {
	t.Setenv("ENV_ACC1_KEY", "key1")
	t.Setenv("ENV_ACC1_SECRET", "secret1")
	t.Setenv("ENV_ACC2_KEY", "key2")
	t.Setenv("ENV_ACC2_SECRET", "secret2")

	dir := t.TempDir()
	manifestPath := dir + "/accounts.jsonc"
	content := `{
		"accounts": [
			{
				"id": "acc_01",
				"exchange": "mexc_futures",
				"enabled": true,
				"outboundIP": "172.16.0.10",
				"env": {
					"apiKey": "ENV_ACC1_KEY",
					"apiSecret": "ENV_ACC1_SECRET"
				}
			},
			{
				"id": "acc_02",
				"exchange": "bybit_futures",
				"enabled": true,
				"outboundIP": "172.16.0.11",
				"env": {
					"apiKey": "ENV_ACC2_KEY",
					"apiSecret": "ENV_ACC2_SECRET"
				}
			}
		]
	}`
	require.NoError(t, os.WriteFile(manifestPath, []byte(content), 0o600))

	manifest, err := config.LoadAccountsManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, manifest.Accounts, 2)
	assert.Equal(t, "acc_01", manifest.Accounts[0].ID)
	assert.Equal(t, "key1", manifest.Accounts[0].APIKey)
	assert.Equal(t, "172.16.0.10", manifest.Accounts[0].OutboundIP)
	assert.Equal(t, "acc_02", manifest.Accounts[1].ID)
	assert.Equal(t, "key2", manifest.Accounts[1].APIKey)
	assert.Equal(t, "172.16.0.11", manifest.Accounts[1].OutboundIP)
}

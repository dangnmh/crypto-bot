package config

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSecretLoader struct {
	values map[string]string
	errs   map[string]error
}

func (f fakeSecretLoader) GetSecret(key string) (string, error) {
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	return f.values[key], nil
}

func TestInternalCredentialCompletenessHelpers(t *testing.T) {
	t.Parallel()

	disabled := APIConfig{}
	complete := APIConfig{
		Future:    &RESTConfig{Enable: true, BaseURL: "https://api.example.com"},
		APIKey:    "key",
		APISecret: "secret",
	}
	completeKucoin := APIConfig{
		Future:        &RESTConfig{Enable: true, BaseURL: "https://api.example.com"},
		APIKey:        "key",
		APISecret:     "secret",
		APIPassphrase: "pass",
	}
	missingKey := APIConfig{
		Future:    &RESTConfig{Enable: true, BaseURL: "https://api.example.com"},
		APISecret: "secret",
	}

	completeBingx := APIConfig{
		Future:    &RESTConfig{Enable: true, BaseURL: "https://api.example.com"},
		APIKey:    "key",
		APISecret: "secret",
	}

	assert.True(t, exchangeCredentialsComplete("mexc", disabled))
	assert.True(t, exchangeCredentialsComplete("mexc", complete))
	assert.False(t, exchangeCredentialsComplete("mexc", missingKey))
	assert.True(t, exchangeCredentialsComplete("bingx", completeBingx))
	assert.True(t, exchangeCredentialsComplete("kucoin", completeKucoin))
	assert.False(t, exchangeCredentialsComplete("kucoin", complete))
	completeOkx := APIConfig{
		Future:        &RESTConfig{Enable: true, BaseURL: "https://api.example.com"},
		APIKey:        "key",
		APISecret:     "secret",
		APIPassphrase: "pass",
	}
	assert.True(t, exchangeCredentialsComplete("okx", completeOkx))
	assert.False(t, exchangeCredentialsComplete("okx", complete))
	assert.True(t, notificationCredentialsComplete(NotiConfig{
		TelegramChatID:   "123",
		TelegramBotToken: "token",
	}))
	assert.False(t, notificationCredentialsComplete(NotiConfig{TelegramChatID: "123"}))
	assert.True(t, bitwardenFallbackNotNeeded(&SystemConfig{
		ExchangeConfig: ExchangeConfig{"mexc": complete, "gate": disabled, "bybit": disabled, "kucoin": completeKucoin},
		NotiConfig:     NotiConfig{TelegramChatID: "123", TelegramBotToken: "token"},
	}))
}

func TestInternalValidateCredentialsAndEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *SystemConfig
		wantErr string
	}{
		{
			name:    "missing all endpoints",
			cfg:     &SystemConfig{},
			wantErr: "at least one active exchange must be enabled",
		},
		{
			name: "mexc missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"mexc": APIConfig{
				Future:    &RESTConfig{Enable: true, BaseURL: "https://mexc.example", WebSocket: WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"}},
				APISecret: "secret",
			}}},
			wantErr: "mexc: API key and secret are required",
		},
		{
			name: "mexc missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"mexc": APIConfig{
				Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example", WebSocket: WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"}},
				APIKey: "key",
			}}},
			wantErr: "mexc: API key and secret are required",
		},
		{
			name: "gate missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"gate": APIConfig{
				Future:    &RESTConfig{Enable: true, BaseURL: "https://gate.example", WebSocket: WebSocketConfig{PublicURL: "wss://gate.example", PrivateURL: "wss://gate.example"}},
				APISecret: "secret",
			}}},
			wantErr: "gate: API key and secret are required",
		},
		{
			name: "gate missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"gate": APIConfig{
				Future: &RESTConfig{Enable: true, BaseURL: "https://gate.example", WebSocket: WebSocketConfig{PublicURL: "wss://gate.example", PrivateURL: "wss://gate.example"}},
				APIKey: "key",
			}}},
			wantErr: "gate: API key and secret are required",
		},
		{
			name: "gate missing websocket url",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"gate": APIConfig{
				Future:    &RESTConfig{Enable: true, BaseURL: "https://gate.example"},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "gate: invalid websocket endpoint URL",
		},
		{
			name: "kucoin missing passphrase",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"kucoin": APIConfig{
				Future:    &RESTConfig{Enable: true, BaseURL: "https://kucoin.example", WebSocket: WebSocketConfig{PublicURL: "wss://kucoin.example", PrivateURL: "wss://kucoin.example"}},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "kucoin: API passphrase is required",
		},
		{
			name: "okx missing passphrase",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"okx": APIConfig{
				Future:    &RESTConfig{Enable: true, BaseURL: "https://okx.example", WebSocket: WebSocketConfig{PublicURL: "wss://okx.example", PrivateURL: "wss://okx.example"}},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "okx: API passphrase is required",
		},
		{
			name: "kucoin missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"kucoin": APIConfig{
				Future:        &RESTConfig{Enable: true, BaseURL: "https://kucoin.example", WebSocket: WebSocketConfig{PublicURL: "wss://kucoin.example", PrivateURL: "wss://kucoin.example"}},
				APISecret:     "secret",
				APIPassphrase: "pass",
			}}},
			wantErr: "kucoin: API key and secret are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Check missing all active exchanges logic
			if tt.cfg.ExchangeConfig == nil || (!tt.cfg.ExchangeConfig["mexc"].IsEnabled() && !tt.cfg.ExchangeConfig["gate"].IsEnabled() && !tt.cfg.ExchangeConfig["okx"].IsEnabled() && !tt.cfg.ExchangeConfig["binance"].IsEnabled() && !tt.cfg.ExchangeConfig["bitget"].IsEnabled() && !tt.cfg.ExchangeConfig["kucoin"].IsEnabled()) {
				err := fmt.Errorf("at least one active exchange must be enabled")
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			err := InitializeBase(tt.cfg)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateExchangeConfig_AccountType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		accountType string
		wantErr     bool
	}{
		{name: "empty defaults to standard"},
		{name: "standard", accountType: BybitAccountTypeStandard},
		{name: "unified", accountType: BybitAccountTypeUnified},
		{name: "trimmed uppercase unified", accountType: " UNIFIED "},
		{name: "unsupported", accountType: "classic", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := ExchangeConfig{
				"bybit": APIConfig{
					Future:      &RESTConfig{Enable: true, BaseURL: "https://bybit.example", WebSocket: WebSocketConfig{PublicURL: "wss://bybit-public.example", PrivateURL: "wss://bybit-private.example"}},
					APIKey:      "key",
					APISecret:   "secret",
					AccountType: tt.accountType,
				},
			}

			err := ValidateExchangeConfig(cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported account type")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateExchangeConfig_AccountTypeOnlyAppliesToBybit(t *testing.T) {
	t.Parallel()

	cfg := ExchangeConfig{
		"mexc": APIConfig{
			Future:      &RESTConfig{Enable: true, BaseURL: "https://mexc.example", WebSocket: WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"}},
			APIKey:      "key",
			APISecret:   "secret",
			AccountType: "ignored",
		},
	}

	require.NoError(t, ValidateExchangeConfig(cfg))
}

func TestValidateExchangeConfig_DisabledFutureEndpointNotValidatedWhenSpotEnabled(t *testing.T) {
	t.Parallel()

	cfg := ExchangeConfig{
		"mexc": APIConfig{
			Spot: &RESTConfig{
				Enable:  true,
				BaseURL: "https://api.mexc.com",
				WebSocket: WebSocketConfig{
					PublicURL:  "wss://wbs-api.mexc.com/ws",
					PrivateURL: "wss://wbs-api.mexc.com/ws",
				},
			},
			Future: &RESTConfig{
				Enable:  false,
				BaseURL: "",
			},
			APIKey:    "key",
			APISecret: "secret",
		},
	}

	require.NoError(t, ValidateExchangeConfig(cfg))
}

func TestInternalApplySystemDefaultsForBothExchanges(t *testing.T) {
	t.Parallel()

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc": APIConfig{
				Future: &RESTConfig{BaseURL: "https://mexc.example", WebSocket: WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"}},
			},
			"gate": APIConfig{
				Future: &RESTConfig{BaseURL: "https://gate.example", WebSocket: WebSocketConfig{PublicURL: "wss://gate.example", PrivateURL: "wss://gate.example"}},
			},
		},
	}

	applySystemDefaults(cfg)

	assert.Equal(t, 30, cfg.ExchangeConfig["mexc"].GetFutureEndpoint().WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, 30, cfg.ExchangeConfig["gate"].GetFutureEndpoint().WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestInternalApplyBitwardenFallbackRequiresConfig(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "")
	t.Setenv("BITWARDEN_PROJECT_NAME", "")

	assert.False(t, HasBitwardenConfig())
	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example"}},
		},
	}
	err := applyBitwardenFallback(cfg)
	require.NoError(t, err) // Non-fatal when not configured
}

func TestInternalApplyBitwardenFallbackTrimsAndToleratesOptionalSecrets(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "project")

	orig := newBitwardenSecretLoader
	t.Cleanup(func() { newBitwardenSecretLoader = orig })
	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return fakeSecretLoader{
			values: map[string]string{ //nolint:gosec // Mock credentials in tests are not real secrets
				"MEXC_API_KEY":          " mexc-key ",
				"MEXC_API_SECRET":       " mexc-secret ",
				"GATE_API_KEY":          " gate-key ",
				"GATE_API_SECRET":       " gate-secret ",
				"BYBIT_API_KEY":         " bybit-key ",
				"BYBIT_API_SECRET":      " bybit-secret ",
				"BITGET_API_KEY":        " bitget-key ",
				"BITGET_API_SECRET":     " bitget-secret ",
				"KUCOIN_API_KEY":        " kucoin-key ",
				"KUCOIN_API_SECRET":     " kucoin-secret ",
				"KUCOIN_API_PASSPHRASE": " kucoin-passphrase ",
				"BINGX_API_KEY":         " bingx-key ",
				"BINGX_API_SECRET":      " bingx-secret ",
				"OKX_API_KEY":           " okx-key ",
				"OKX_API_SECRET":        " okx-secret ",
				"OKX_API_PASSPHRASE":    " okx-passphrase ",
				"TELEGRAM_CHAT_ID":      " 123 ",
				"TELEGRAM_BOT_TOKEN":    " token ",
			},
		}, nil
	}

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc":   APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example"}},
			"gate":   APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://gate.example"}},
			"bybit":  APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bybit.example"}},
			"bitget": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bitget.example"}},
			"kucoin": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://kucoin.example"}},
			"bingx":  APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bingx.example"}},
			"okx":    APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://okx.example"}},
		},
	}

	err := applyBitwardenFallback(cfg)
	require.NoError(t, err)
	assert.Equal(t, "mexc-key", cfg.ExchangeConfig["mexc"].APIKey)
	assert.Equal(t, "mexc-secret", cfg.ExchangeConfig["mexc"].APISecret)
	assert.Equal(t, "gate-key", cfg.ExchangeConfig["gate"].APIKey)
	assert.Equal(t, "gate-secret", cfg.ExchangeConfig["gate"].APISecret)
	assert.Equal(t, "bybit-key", cfg.ExchangeConfig["bybit"].APIKey)
	assert.Equal(t, "bybit-secret", cfg.ExchangeConfig["bybit"].APISecret)
	assert.Equal(t, "bitget-key", cfg.ExchangeConfig["bitget"].APIKey)
	assert.Equal(t, "bitget-secret", cfg.ExchangeConfig["bitget"].APISecret)
	assert.Equal(t, "bingx-key", cfg.ExchangeConfig["bingx"].APIKey)
	assert.Equal(t, "bingx-secret", cfg.ExchangeConfig["bingx"].APISecret)
	assert.Equal(t, "kucoin-key", cfg.ExchangeConfig["kucoin"].APIKey)
	assert.Equal(t, "kucoin-secret", cfg.ExchangeConfig["kucoin"].APISecret)
	assert.Equal(t, "kucoin-passphrase", cfg.ExchangeConfig["kucoin"].APIPassphrase)
	assert.Equal(t, "okx-key", cfg.ExchangeConfig["okx"].APIKey)
	assert.Equal(t, "okx-secret", cfg.ExchangeConfig["okx"].APISecret)
	assert.Equal(t, "okx-passphrase", cfg.ExchangeConfig["okx"].APIPassphrase)

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return nil, errors.New("login failed")
	}
	err = applyBitwardenFallback(&SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example"}},
		},
	})
	require.ErrorContains(t, err, "login failed")
}

func TestInternalApplyBitwardenFallbackFillsMissingFields(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "project")

	orig := newBitwardenSecretLoader
	t.Cleanup(func() { newBitwardenSecretLoader = orig })
	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return fakeSecretLoader{values: map[string]string{ //nolint:gosec // Mock credentials in tests are not real secrets
			"MEXC_API_KEY":              "mexc-key",
			"MEXC_API_SECRET":           "mexc-secret",
			"GATE_API_KEY":              "gate-key",
			"GATE_API_SECRET":           "gate-secret",
			"BYBIT_API_KEY":             "bybit-key",
			"BYBIT_API_SECRET":          "bybit-secret",
			"BITGET_API_KEY":            "bitget-key",
			"BITGET_API_SECRET":         "bitget-secret",
			"KUCOIN_API_KEY":            "kucoin-key",
			"KUCOIN_API_SECRET":         "kucoin-secret",
			"KUCOIN_API_PASSPHRASE":     "kucoin-passphrase",
			"BINGX_API_KEY":             "bingx-key",
			"BINGX_API_SECRET":          "bingx-secret",
			"OKX_API_KEY":               "okx-key",
			"OKX_API_SECRET":            "okx-secret",
			"OKX_API_PASSPHRASE":        "okx-passphrase",
			"TELEGRAM_CHAT_ID":          "123",
			"TELEGRAM_CRITICAL_CHAT_ID": "456",
			"TELEGRAM_BOT_TOKEN":        "token",
		}}, nil
	}

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc":   APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example"}},
			"gate":   APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://gate.example"}},
			"bybit":  APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bybit.example"}},
			"bitget": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bitget.example"}},
			"kucoin": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://kucoin.example"}},
			"bingx":  APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://bingx.example"}},
			"okx":    APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://okx.example"}},
		},
	}
	require.NoError(t, applyBitwardenFallback(cfg))

	assert.Equal(t, "mexc-key", cfg.ExchangeConfig["mexc"].APIKey)
	assert.Equal(t, "mexc-secret", cfg.ExchangeConfig["mexc"].APISecret)
	assert.Equal(t, "gate-key", cfg.ExchangeConfig["gate"].APIKey)
	assert.Equal(t, "gate-secret", cfg.ExchangeConfig["gate"].APISecret)
	assert.Equal(t, "bybit-key", cfg.ExchangeConfig["bybit"].APIKey)
	assert.Equal(t, "bybit-secret", cfg.ExchangeConfig["bybit"].APISecret)
	assert.Equal(t, "bitget-key", cfg.ExchangeConfig["bitget"].APIKey)
	assert.Equal(t, "bitget-secret", cfg.ExchangeConfig["bitget"].APISecret)
	assert.Equal(t, "kucoin-key", cfg.ExchangeConfig["kucoin"].APIKey)
	assert.Equal(t, "kucoin-secret", cfg.ExchangeConfig["kucoin"].APISecret)
	assert.Equal(t, "kucoin-passphrase", cfg.ExchangeConfig["kucoin"].APIPassphrase)
	assert.Equal(t, "bingx-key", cfg.ExchangeConfig["bingx"].APIKey)
	assert.Equal(t, "bingx-secret", cfg.ExchangeConfig["bingx"].APISecret)
	assert.Equal(t, "okx-key", cfg.ExchangeConfig["okx"].APIKey)
	assert.Equal(t, "okx-secret", cfg.ExchangeConfig["okx"].APISecret)
	assert.Equal(t, "okx-passphrase", cfg.ExchangeConfig["okx"].APIPassphrase)
	assert.Equal(t, "123", cfg.NotiConfig.TelegramChatID)
	assert.Equal(t, "456", cfg.NotiConfig.TelegramCriticalChatID)
	assert.Equal(t, "token", cfg.NotiConfig.TelegramBotToken)

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return nil, errors.New("boom")
	}
	require.ErrorContains(t, applyBitwardenFallback(&SystemConfig{
		ExchangeConfig: ExchangeConfig{"mexc": APIConfig{Future: &RESTConfig{Enable: true, BaseURL: "https://mexc.example"}}},
	}), "bitwarden fallback failed")
}

func TestInitializeBase_TelegramCriticalChatID(t *testing.T) {
	t.Setenv("TELEGRAM_CHAT_ID", "12345")
	t.Setenv("TELEGRAM_CRITICAL_CHAT_ID", "67890")
	t.Setenv("TELEGRAM_BOT_TOKEN", "mock-token")
	t.Setenv("MEXC_API_KEY", "key")
	t.Setenv("MEXC_API_SECRET", "secret")

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			MexcName: APIConfig{
				Future: &RESTConfig{
					Enable:  true,
					BaseURL: "https://api.mexc.com",
					WebSocket: WebSocketConfig{
						PublicURL:  "wss://wbs-api.mexc.com/ws",
						PrivateURL: "wss://wbs-api.mexc.com/ws",
					},
				},
			},
		},
	}
	err := InitializeBase(cfg)
	require.NoError(t, err)
	assert.Equal(t, "12345", cfg.NotiConfig.TelegramChatID)
	assert.Equal(t, "67890", cfg.NotiConfig.TelegramCriticalChatID)
	assert.Equal(t, "mock-token", cfg.NotiConfig.TelegramBotToken)
}

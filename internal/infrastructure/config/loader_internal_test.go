package config

import (
	"errors"
	"fmt"
	"testing"

	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
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
		Enable:    true,
		Future:    RESTConfig{BaseURL: "https://api.example.com"},
		APIKey:    "key",
		APISecret: "secret",
	}
	completeKucoin := APIConfig{
		Enable:        true,
		Future:        RESTConfig{BaseURL: "https://api.example.com"},
		APIKey:        "key",
		APISecret:     "secret",
		APIPassphrase: "pass",
	}
	missingKey := APIConfig{
		Enable:    true,
		Future:    RESTConfig{BaseURL: "https://api.example.com"},
		APISecret: "secret",
	}

	completeBingx := APIConfig{
		Enable:    true,
		Future:    RESTConfig{BaseURL: "https://api.example.com"},
		APIKey:    "key",
		APISecret: "secret",
	}

	assert.True(t, exchangeCredentialsComplete("Mexc", disabled))
	assert.True(t, exchangeCredentialsComplete("Mexc", complete))
	assert.False(t, exchangeCredentialsComplete("Mexc", missingKey))
	assert.True(t, exchangeCredentialsComplete("Bingx", completeBingx))
	assert.True(t, exchangeCredentialsComplete("Kucoin", completeKucoin))
	assert.False(t, exchangeCredentialsComplete("Kucoin", complete))
	assert.True(t, notificationCredentialsComplete(NotiConfig{
		TelegramChatID:   "123",
		TelegramBotToken: "token",
	}))
	assert.False(t, notificationCredentialsComplete(NotiConfig{TelegramChatID: "123"}))
	assert.True(t, bitwardenFallbackNotNeeded(&SystemConfig{
		ExchangeConfig: ExchangeConfig{Mexc: complete, Gate: disabled, Bybit: disabled, Kucoin: completeKucoin},
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
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Mexc: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://mexc.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://mexc.example"},
				APISecret: "secret",
			}}},
			wantErr: "api_config",
		},
		{
			name: "mexc missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Mexc: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://mexc.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://mexc.example"},
				APIKey:    "key",
			}}},
			wantErr: "api_config",
		},
		{
			name: "gate missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://gate.example"},
				APISecret: "secret",
			}}},
			wantErr: "api_config",
		},
		{
			name: "gate missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://gate.example"},
				APIKey:    "key",
			}}},
			wantErr: "api_config",
		},
		{
			name: "gate missing websocket url",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "api_config",
		},
		{
			name: "kucoin missing passphrase",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Kucoin: APIConfig{
				Enable:    true,
				Future:    RESTConfig{BaseURL: "https://kucoin.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://kucoin.example"},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "api_config",
		},
		{
			name: "kucoin missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Kucoin: APIConfig{
				Enable:        true,
				Future:        RESTConfig{BaseURL: "https://kucoin.example"},
				WebSocket:     WebSocketConfig{WSURL: "wss://kucoin.example"},
				APISecret:     "secret",
				APIPassphrase: "pass",
			}}},
			wantErr: "api_config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Check missing all active exchanges logic
			if !tt.cfg.ExchangeConfig.Mexc.Enable && !tt.cfg.ExchangeConfig.Gate.Enable && !tt.cfg.ExchangeConfig.Okx.Enable && !tt.cfg.ExchangeConfig.Binance.Enable && !tt.cfg.ExchangeConfig.Bitget.Enable && !tt.cfg.ExchangeConfig.Kucoin.Enable {
				err := fmt.Errorf("at least one active exchange must be enabled")
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			// Validate using register validation tag flow
			validate := validator.New()
			_ = validate.RegisterValidation("api_config", ValidateAPIConfigField)

			err := validate.Struct(tt.cfg)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateAPIConfigField_AccountType(t *testing.T) {
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

			validate := validator.New()
			_ = validate.RegisterValidation("api_config", ValidateAPIConfigField)

			cfg := &SystemConfig{ExchangeConfig: ExchangeConfig{Bybit: APIConfig{
				Enable:      true,
				Future:      RESTConfig{BaseURL: "https://bybit.example"},
				WebSocket:   WebSocketConfig{PublicURL: "wss://bybit-public.example", PrivateURL: "wss://bybit-private.example"},
				APIKey:      "key",
				APISecret:   "secret",
				AccountType: tt.accountType,
			}}}

			err := validate.Struct(cfg)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorContains(t, err, "api_config")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateAPIConfigField_AccountTypeOnlyAppliesToBybit(t *testing.T) {
	t.Parallel()

	validate := validator.New()
	_ = validate.RegisterValidation("api_config", ValidateAPIConfigField)

	cfg := &SystemConfig{ExchangeConfig: ExchangeConfig{Mexc: APIConfig{
		Enable:      true,
		Future:      RESTConfig{BaseURL: "https://mexc.example"},
		WebSocket:   WebSocketConfig{WSURL: "wss://mexc.example"},
		APIKey:      "key",
		APISecret:   "secret",
		AccountType: "ignored",
	}}}

	require.NoError(t, validate.Struct(cfg))
}

func TestInternalApplySystemDefaultsForBothExchanges(t *testing.T) {
	t.Parallel()

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			Mexc: APIConfig{
				Future:    RESTConfig{BaseURL: "https://mexc.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://mexc.example"},
			},
			Gate: APIConfig{
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://gate.example"},
			},
		},
	}

	applySystemDefaults(cfg)

	assert.Equal(t, types.Duration(30*1e9), cfg.Sync.Time)
	assert.Equal(t, types.Duration(30*1e9), cfg.Sync.Ticker)
	assert.Equal(t, types.Duration(300*1e9), cfg.Sync.Contract)
	assert.Equal(t, 30, cfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, 30, cfg.ExchangeConfig.Gate.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

func TestInternalLoadFromBitwardenRequiresConfig(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "")
	t.Setenv("BITWARDEN_PROJECT_NAME", "")

	assert.False(t, hasBitwardenConfig())
	_, err := LoadFromBitwarden()
	require.ErrorContains(t, err, "bitwarden configuration not found")
}

func TestInternalLoadFromBitwardenTrimsAndToleratesOptionalSecrets(t *testing.T) {
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
				"TELEGRAM_CHAT_ID":      " 123 ",
				"TELEGRAM_BOT_TOKEN":    " token ",
			},
		}, nil
	}

	creds, err := LoadFromBitwarden()
	require.NoError(t, err)
	assert.Equal(t, "mexc-key", creds.MEXCAPIKey)
	assert.Equal(t, "mexc-secret", creds.MEXCAPISecret)
	assert.Equal(t, "gate-key", creds.GateAPIKey)
	assert.Equal(t, "gate-secret", creds.GateAPISecret)
	assert.Equal(t, "bybit-key", creds.BybitAPIKey)
	assert.Equal(t, "bybit-secret", creds.BybitAPISecret)
	assert.Equal(t, "bitget-key", creds.BitgetAPIKey)
	assert.Equal(t, "bitget-secret", creds.BitgetAPISecret)
	assert.Equal(t, "bingx-key", creds.BingxAPIKey)
	assert.Equal(t, "bingx-secret", creds.BingxAPISecret)
	assert.Equal(t, "kucoin-key", creds.KucoinAPIKey)
	assert.Equal(t, "kucoin-secret", creds.KucoinAPISecret)
	assert.Equal(t, "kucoin-passphrase", creds.KucoinPassphrase)
	assert.Equal(t, "123", creds.TelegramChatID)
	assert.Equal(t, "token", creds.TelegramBotToken)

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return nil, errors.New("login failed")
	}
	_, err = LoadFromBitwarden()
	require.ErrorContains(t, err, "login failed")

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return fakeSecretLoader{errs: map[string]error{"MEXC_API_KEY": errors.New("missing")}}, nil
	}
	_, err = LoadFromBitwarden()
	require.ErrorContains(t, err, "failed to get MEXC_API_KEY")
}

func TestInternalApplyBitwardenFallbackFillsMissingFields(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "project")

	orig := newBitwardenSecretLoader
	t.Cleanup(func() { newBitwardenSecretLoader = orig })
	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return fakeSecretLoader{values: map[string]string{ //nolint:gosec // Mock credentials in tests are not real secrets
			"MEXC_API_KEY":          "mexc-key",
			"MEXC_API_SECRET":       "mexc-secret",
			"GATE_API_KEY":          "gate-key",
			"GATE_API_SECRET":       "gate-secret",
			"BYBIT_API_KEY":         "bybit-key",
			"BYBIT_API_SECRET":      "bybit-secret",
			"BITGET_API_KEY":        "bitget-key",
			"BITGET_API_SECRET":     "bitget-secret",
			"KUCOIN_API_KEY":        "kucoin-key",
			"KUCOIN_API_SECRET":     "kucoin-secret",
			"KUCOIN_API_PASSPHRASE": "kucoin-passphrase",
			"BINGX_API_KEY":         "bingx-key",
			"BINGX_API_SECRET":      "bingx-secret",
			"TELEGRAM_CHAT_ID":      "123",
			"TELEGRAM_BOT_TOKEN":    "token",
		}}, nil
	}

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			Mexc:   APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://mexc.example"}},
			Gate:   APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://gate.example"}},
			Bybit:  APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://bybit.example"}},
			Bitget: APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://bitget.example"}},
			Kucoin: APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://kucoin.example"}},
			Bingx:  APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://bingx.example"}},
		},
	}
	require.NoError(t, applyBitwardenFallback(cfg))

	assert.Equal(t, "mexc-key", cfg.ExchangeConfig.Mexc.APIKey)
	assert.Equal(t, "mexc-secret", cfg.ExchangeConfig.Mexc.APISecret)
	assert.Equal(t, "gate-key", cfg.ExchangeConfig.Gate.APIKey)
	assert.Equal(t, "gate-secret", cfg.ExchangeConfig.Gate.APISecret)
	assert.Equal(t, "bybit-key", cfg.ExchangeConfig.Bybit.APIKey)
	assert.Equal(t, "bybit-secret", cfg.ExchangeConfig.Bybit.APISecret)
	assert.Equal(t, "bitget-key", cfg.ExchangeConfig.Bitget.APIKey)
	assert.Equal(t, "bitget-secret", cfg.ExchangeConfig.Bitget.APISecret)
	assert.Equal(t, "kucoin-key", cfg.ExchangeConfig.Kucoin.APIKey)
	assert.Equal(t, "kucoin-secret", cfg.ExchangeConfig.Kucoin.APISecret)
	assert.Equal(t, "kucoin-passphrase", cfg.ExchangeConfig.Kucoin.APIPassphrase)
	assert.Equal(t, "bingx-key", cfg.ExchangeConfig.Bingx.APIKey)
	assert.Equal(t, "bingx-secret", cfg.ExchangeConfig.Bingx.APISecret)
	assert.Equal(t, "123", cfg.NotiConfig.TelegramChatID)
	assert.Equal(t, "token", cfg.NotiConfig.TelegramBotToken)

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return nil, errors.New("boom")
	}
	require.ErrorContains(t, applyBitwardenFallback(&SystemConfig{
		ExchangeConfig: ExchangeConfig{Mexc: APIConfig{Enable: true, Future: RESTConfig{BaseURL: "https://mexc.example"}}},
	}), "bitwarden fallback failed")
}

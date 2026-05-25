package config

import (
	"errors"
	"testing"

	"crypto-bot/pkg/types"

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
		Future:    RESTConfig{BaseURL: "https://api.example.com"},
		APIKey:    "key",
		APISecret: "secret",
	}
	missingKey := APIConfig{
		Future:    RESTConfig{BaseURL: "https://api.example.com"},
		APISecret: "secret",
	}

	assert.True(t, exchangeCredentialsComplete(disabled))
	assert.True(t, exchangeCredentialsComplete(complete))
	assert.False(t, exchangeCredentialsComplete(missingKey))
	assert.True(t, notificationCredentialsComplete(NotiConfig{
		TelegramChatID:   "123",
		TelegramBotToken: "token",
	}))
	assert.False(t, notificationCredentialsComplete(NotiConfig{TelegramChatID: "123"}))
	assert.True(t, bitwardenFallbackNotNeeded(&SystemConfig{
		ExchangeConfig: ExchangeConfig{Mexc: complete, Gate: disabled},
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
			wantErr: "api.future.baseURL",
		},
		{
			name: "mexc missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Mexc: APIConfig{
				Future:    RESTConfig{BaseURL: "https://mexc.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://mexc.example"},
				APISecret: "secret",
			}}},
			wantErr: "MEXC_API_KEY",
		},
		{
			name: "mexc missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Mexc: APIConfig{
				Future:    RESTConfig{BaseURL: "https://mexc.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://mexc.example"},
				APIKey:    "key",
			}}},
			wantErr: "MEXC_API_SECRET",
		},
		{
			name: "gate missing key",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://gate.example"},
				APISecret: "secret",
			}}},
			wantErr: "GATE_API_KEY",
		},
		{
			name: "gate missing secret",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				WebSocket: WebSocketConfig{WSURL: "wss://gate.example"},
				APIKey:    "key",
			}}},
			wantErr: "GATE_API_SECRET",
		},
		{
			name: "gate missing websocket url",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{Gate: APIConfig{
				Future:    RESTConfig{BaseURL: "https://gate.example"},
				APIKey:    "key",
				APISecret: "secret",
			}}},
			wantErr: "gate api.websocket.wsURL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			credentialErr := validateCredentials(tt.cfg)
			endpointErr := validateEndpoints(tt.cfg)
			if credentialErr != nil {
				require.ErrorContains(t, credentialErr, tt.wantErr)
				return
			}
			require.ErrorContains(t, endpointErr, tt.wantErr)
		})
	}
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
			values: map[string]string{
				"MEXC_API_KEY":       " mexc-key ",
				"MEXC_API_SECRET":    " mexc-secret ",
				"GATE_API_KEY":       " gate-key ",
				"GATE_API_SECRET":    " gate-secret ",
				"TELEGRAM_CHAT_ID":   " 123 ",
				"TELEGRAM_BOT_TOKEN": " token ",
			},
		}, nil
	}

	creds, err := LoadFromBitwarden()
	require.NoError(t, err)
	assert.Equal(t, "mexc-key", creds.APIKey)
	assert.Equal(t, "mexc-secret", creds.APISecret)
	assert.Equal(t, "gate-key", creds.GateKey)
	assert.Equal(t, "gate-secret", creds.GateSecret)
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
		return fakeSecretLoader{values: map[string]string{
			"MEXC_API_KEY":       "mexc-key",
			"MEXC_API_SECRET":    "mexc-secret",
			"GATE_API_KEY":       "gate-key",
			"GATE_API_SECRET":    "gate-secret",
			"TELEGRAM_CHAT_ID":   "123",
			"TELEGRAM_BOT_TOKEN": "token",
		}}, nil
	}

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			Mexc: APIConfig{Future: RESTConfig{BaseURL: "https://mexc.example"}},
			Gate: APIConfig{Future: RESTConfig{BaseURL: "https://gate.example"}},
		},
	}
	require.NoError(t, applyBitwardenFallback(cfg))

	assert.Equal(t, "mexc-key", cfg.ExchangeConfig.Mexc.APIKey)
	assert.Equal(t, "mexc-secret", cfg.ExchangeConfig.Mexc.APISecret)
	assert.Equal(t, "gate-key", cfg.ExchangeConfig.Gate.APIKey)
	assert.Equal(t, "gate-secret", cfg.ExchangeConfig.Gate.APISecret)
	assert.Equal(t, "123", cfg.NotiConfig.TelegramChatID)
	assert.Equal(t, "token", cfg.NotiConfig.TelegramBotToken)

	newBitwardenSecretLoader = func() (bitwardenSecretLoader, error) {
		return nil, errors.New("boom")
	}
	require.ErrorContains(t, applyBitwardenFallback(&SystemConfig{
		ExchangeConfig: ExchangeConfig{Mexc: APIConfig{Future: RESTConfig{BaseURL: "https://mexc.example"}}},
	}), "bitwarden fallback failed")
}

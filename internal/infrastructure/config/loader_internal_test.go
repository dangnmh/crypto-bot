package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name: "gate missing websocket url",
			cfg: &SystemConfig{ExchangeConfig: ExchangeConfig{"gate_futures": EndpointConfig{
				Enable:  true,
				BaseURL: "https://gate.example",
			}}},
			wantErr: "gate_futures: invalid websocket endpoint URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if len(tt.cfg.ExchangeConfig) == 0 {
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
				"bybit_futures": EndpointConfig{
					Enable:      true,
					BaseURL:     "https://bybit.example",
					WebSocket:   WebSocketConfig{PublicURL: "wss://bybit-public.example", PrivateURL: "wss://bybit-private.example"},
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
		"mexc_futures": EndpointConfig{
			Enable:      true,
			BaseURL:     "https://mexc.example",
			WebSocket:   WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"},
			AccountType: "ignored",
		},
	}

	require.NoError(t, ValidateExchangeConfig(cfg))
}

func TestInternalApplySystemDefaultsForBothExchanges(t *testing.T) {
	t.Parallel()

	cfg := &SystemConfig{
		ExchangeConfig: ExchangeConfig{
			"mexc_futures": EndpointConfig{BaseURL: "https://mexc.example", WebSocket: WebSocketConfig{PublicURL: "wss://mexc.example", PrivateURL: "wss://mexc.example"}},
			"gate_futures": EndpointConfig{BaseURL: "https://gate.example", WebSocket: WebSocketConfig{PublicURL: "wss://gate.example", PrivateURL: "wss://gate.example"}},
		},
	}

	applySystemDefaults(cfg)

	assert.Equal(t, 30, cfg.ExchangeConfig["mexc_futures"].WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, 30, cfg.ExchangeConfig["gate_futures"].WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "info", cfg.Logging.Level)
}

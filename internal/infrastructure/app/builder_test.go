package app_test

import (
	"context"
	"net/http"
	"testing"

	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineBuilder_NilConfigReturnsError(t *testing.T) {
	t.Parallel()
	_, err := app.NewEngineBuilder().
		Build()
	assert.Error(t, err, "expected error for nil config")
}

func TestEngineBuilder_WithOptionalDependenciesBuilds(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc_futures": sysconfig.EndpointConfig{
				Enable:    true,
				BaseURL:   "https://api.example.com",
				APIKey:    "key",
				APISecret: "secret",
				WebSocket: sysconfig.WebSocketConfig{PublicURL: "wss://ws.example.com", PrivateURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
			},
		},
	}

	e, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		WithHTTPClient(&http.Client{}).
		WithLogger(testLogger()).
		Build()
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Contains(t, e.Providers, "mexc_futures")
	require.NoError(t, e.Shutdown(context.Background()))
}

func TestEngineBuilder_MissingAPIBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc_futures": sysconfig.EndpointConfig{
				Enable:    true,
				BaseURL:   "",
				APIKey:    "key",
				APISecret: "secret",
				WebSocket: sysconfig.WebSocketConfig{PublicURL: "wss://ws.example.com", PrivateURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
			},
		},
	}

	_, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		Build()

	assert.Error(t, err, "expected error for missing BaseURL")
}

func TestEngineBuilder_MissingWSURL(t *testing.T) {
	t.Parallel()
	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc_futures": sysconfig.EndpointConfig{
				Enable:    true,
				BaseURL:   "https://api.example.com",
				APIKey:    "key",
				APISecret: "secret",
				WebSocket: sysconfig.WebSocketConfig{PublicURL: "", PrivateURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
			},
		},
	}

	_, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		Build()

	assert.Error(t, err, "expected error for missing WSURL")
}

func TestEngineBuilder_InvalidMaxPairs(t *testing.T) {
	t.Parallel()
	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc_futures": sysconfig.EndpointConfig{
				Enable:    true,
				BaseURL:   "https://api.example.com",
				APIKey:    "key",
				APISecret: "secret",
				WebSocket: sysconfig.WebSocketConfig{PublicURL: "wss://ws.example.com", PrivateURL: "wss://ws.example.com", MaxPairsPerWSConn: 0},
			},
		},
	}

	e, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		WithLogger(testLogger()).
		Build()
	require.NoError(t, err)
	require.NotNil(t, e)
	require.NoError(t, e.Shutdown(context.Background()))
}

// ── app.StoreRegistry — Chain building ───────────────────────────────────.

func TestStoreRegistry_ChainBuild(t *testing.T) {
	t.Parallel()
	r := app.NewStoreRegistry(testLogger()).WithFunding().WithKline()

	require.NotNil(t, r.Ticker)
	require.NotNil(t, r.Contract)
	require.NotNil(t, r.Funding)
	require.NotNil(t, r.Kline)
}

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

// ── EngineBuilder validation tests ───────────────────────────────────.

func TestEngineBuilder_MissingConfig(t *testing.T) {
	t.Parallel()
	_, err := app.NewEngineBuilder().
		Build()
	assert.Error(t, err, "expected error for nil config")
}

func TestEngineBuilder_WithOptionalDependenciesBuilds(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
				},
				APIKey:    "key",
				APISecret: "secret",
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
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
				},
				APIKey:    "key",
				APISecret: "secret",
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
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "", MaxPairsPerWSConn: 10},
				},
				APIKey:    "key",
				APISecret: "secret",
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
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 0},
				},
				APIKey:    "key",
				APISecret: "secret",
			},
		},
	}

	_, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		Build()

	assert.Error(t, err, "expected error for MaxPairsPerWSConn=0")
}

func TestEngineBuilder_MissingAPIKey(t *testing.T) {
	t.Parallel()
	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
				},
				APIKey:    "",
				APISecret: "secret",
			},
		},
	}

	_, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		Build()

	assert.Error(t, err, "expected error for missing APIKey")
}

func TestEngineBuilder_MissingAPISecret(t *testing.T) {
	t.Parallel()
	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			"mexc": sysconfig.APIConfig{
				Future: &sysconfig.RESTConfig{
					Enable:    true,
					BaseURL:   "https://api.example.com",
					WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
				},
				APIKey:    "key",
				APISecret: "",
			},
		},
	}

	_, err := app.NewEngineBuilder().
		WithSystemConfig(cfg).
		Build()

	assert.Error(t, err, "expected error for missing APISecret")
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

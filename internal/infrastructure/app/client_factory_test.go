package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"io"
	"strings"

	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllExchangesFetchKlinesSupport(t *testing.T) {
	t.Parallel()
	exchanges := []string{
		"mexc", "gate", "bybit", "okx", "kucoin", "binance", "hyperliquid", "bitget",
		"bingx", "zoomex", "deepcoin", "gemini", "toobit", "weex", "batonex", "bitmart",
		"coinw", "krakenfutures", "bitunix", "xt", "htx", "lbank", "mandala", "orangex",
		"pionex", "poloniex", "deribit", "delta", "coinex", "bitfinex", "whitebit", "dydx",
		"aster", "backpack", "aevo", "apex", "lighter", "tradexyz", "grvt", "pacifica",
		"extended", "jupiter", "avantis", "btse", "bitmex", "hashkey", "hibt", "hitbtc",
		"hotcoin", "cryptocom", "woox", "phemex", "blofin", "digifinex", "bydfi", "ju",
		"sunx", "fameex", "fmfw", "coinbase", "trubit",
	}

	supported := map[string]bool{
		"binance":       true,
		"bybit":         true,
		"gate":          true,
		"mexc":          true,
		"okx":           true,
		"hyperliquid":   true,
		"apex":          true,
		"aster":         true,
		"backpack":      true,
		"batonex":       true,
		"bingx":         true,
		"kucoin":        true,
		"bitget":        true,
		"zoomex":        true,
		"deepcoin":      true,
		"toobit":        true,
		"bitmart":       true,
		"orangex":       true,
		"bitunix":       true,
		"xt":            true,
		"pionex":        true,
		"hotcoin":       true,
		"weex":          true,
		"blofin":        true,
		"whitebit":      true,
		"bydfi":         true,
		"ju":            true,
		"fameex":        true,
		"lbank":         true,
		"krakenfutures": true,
		"lighter":       true,
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	logCfg := config.LoggingConfig{}

	for _, name := range exchanges {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := app.BuildPublicClient(context.Background(), name, httpClient, slog.Default(), logCfg)
			assert.NoError(t, err)

			kProv, ok := c.(exchange.KlineProvider)
			assert.True(t, ok, "client %s must implement KlineProvider", name)

			// Call FetchKlines with short timeout to fail fast if online requests block
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			_, err = kProv.FetchKlines(ctx, "BTC-USDT", exchange.Interval1m, time.Now().Add(-10*time.Minute), time.Now())
			if supported[name] {
				// Supported clients might return context/network/symbol errors, but never "does not support FetchKlines"
				if err != nil {
					assert.NotContains(t, err.Error(), "does not support FetchKlines")
				}
			} else {
				// Unsupported clients must return the unsupported error message
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "does not support FetchKlines")
			}
		})
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBuildPublicClient_CoinbaseBaseURL(t *testing.T) {
	t.Parallel()

	var requestedURL string
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("[]")),
			}, nil
		}),
	}

	c, err := app.BuildPublicClient(context.Background(), "coinbase", httpClient, slog.Default(), config.LoggingConfig{})
	require.NoError(t, err)

	type fundingSymbolGetter interface {
		GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error)
	}

	getter, ok := c.(fundingSymbolGetter)
	require.True(t, ok)

	_, err = getter.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, "https://api.international.coinbase.com/api/v1/instruments", requestedURL)
}

func TestBuildPublicClient_SunXBaseURL(t *testing.T) {
	t.Parallel()

	var requestedURL string
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			var responseBody string
			if strings.Contains(req.URL.Path, "batch_funding_rate") {
				responseBody = `{"status":"ok","data":[]}`
			} else {
				responseBody = `{"status":"ok","ticks":[]}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		}),
	}

	c, err := app.BuildPublicClient(context.Background(), "sunx", httpClient, slog.Default(), config.LoggingConfig{})
	require.NoError(t, err)

	type fundingSymbolGetter interface {
		GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error)
	}

	getter, ok := c.(fundingSymbolGetter)
	require.True(t, ok)

	_, err = getter.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(requestedURL, "https://api.sunx.io"), "expected URL starting with https://api.sunx.io, got: %s", requestedURL)
}

func TestBuildPublicClient_TrubitBaseURL(t *testing.T) {
	t.Parallel()

	var requestedURL string
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok","result":[]}`)),
			}, nil
		}),
	}

	c, err := app.BuildPublicClient(context.Background(), "trubit", httpClient, slog.Default(), config.LoggingConfig{})
	require.NoError(t, err)

	type fundingSymbolGetter interface {
		GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error)
	}

	getter, ok := c.(fundingSymbolGetter)
	require.True(t, ok)

	_, err = getter.GetPotentialFundingSymbols(context.Background(), 0, 0, nil, nil)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(requestedURL, "https://api-futures.trubit.com"), "expected URL starting with https://api-futures.trubit.com, got: %s", requestedURL)
}

func TestDefaultProviderFactories_SpotFuturesEnabled(t *testing.T) {
	t.Parallel()

	sysCfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Spot:   &config.RESTConfig{Enable: true, BaseURL: "https://api.mexc.com"},
				Future: &config.RESTConfig{Enable: false, BaseURL: "https://contract.mexc.com"},
			},
			"toobit": config.APIConfig{
				Spot:   &config.RESTConfig{Enable: false, BaseURL: "https://api.toobit.com"},
				Future: &config.RESTConfig{Enable: true, BaseURL: "https://api.toobit.com"},
			},
		},
	}

	factories := app.DefaultProviderFactories()
	factoryMap := make(map[string]app.ProviderFactory)
	for _, f := range factories {
		factoryMap[f.Name()] = f
	}

	require.Contains(t, factoryMap, "mexc_spot")
	require.Contains(t, factoryMap, "mexc_futures")
	require.Contains(t, factoryMap, "toobit_spot")
	require.Contains(t, factoryMap, "toobit_futures")

	assert.True(t, factoryMap["mexc_spot"].Enabled(sysCfg))
	assert.False(t, factoryMap["mexc_futures"].Enabled(sysCfg))
	assert.False(t, factoryMap["toobit_spot"].Enabled(sysCfg))
	assert.True(t, factoryMap["toobit_futures"].Enabled(sysCfg))
}

func TestDefaultProviderFactories_BothSpotAndFuturesEnabled(t *testing.T) {
	t.Parallel()

	sysCfg := &config.SystemConfig{
		ExchangeConfig: config.ExchangeConfig{
			"mexc": config.APIConfig{
				Spot: &config.EndpointConfig{
					Enable:  true,
					BaseURL: "https://api.mexc.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:         "wss://wbs-api.mexc.com/ws",
						PrivateURL:        "wss://wbs-api.mexc.com/ws",
						MaxPairsPerWSConn: 30,
					},
				},
				Future: &config.EndpointConfig{
					Enable:  true,
					BaseURL: "https://contract.mexc.com",
					WebSocket: config.WebSocketConfig{
						PublicURL:         "wss://contract.mexc.com/edge",
						PrivateURL:        "wss://contract.mexc.com/edge",
						MaxPairsPerWSConn: 30,
					},
				},
			},
		},
	}

	factories := app.DefaultProviderFactories()
	factoryMap := make(map[string]app.ProviderFactory)
	for _, f := range factories {
		factoryMap[f.Name()] = f
	}

	assert.NotContains(t, factoryMap, "mexc")
	require.Contains(t, factoryMap, "mexc_spot")
	require.Contains(t, factoryMap, "mexc_futures")

	assert.True(t, factoryMap["mexc_spot"].Enabled(sysCfg))
	assert.True(t, factoryMap["mexc_futures"].Enabled(sysCfg))

	cfg := app.ProviderFactoryConfig{
		SystemConfig: sysCfg,
		HTTPClient:   &http.Client{},
		Logger:       slog.Default(),
		Bus:          nil,
	}

	spotProv, err := factoryMap["mexc_spot"].Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, spotProv)
	assert.Equal(t, "mexc_spot", spotProv.Name)

	futuresProv, err := factoryMap["mexc_futures"].Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, futuresProv)
	assert.Equal(t, "mexc_futures", futuresProv.Name)
}

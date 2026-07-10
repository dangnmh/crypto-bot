package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
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
		"sunx", "fameex", "fmfw", "coinbase", "koinbay", "trubit",
	}

	supported := map[string]bool{
		"binance":     true,
		"bybit":       true,
		"gate":        true,
		"mexc":        true,
		"okx":         true,
		"hyperliquid": true,
		"apex":        true,
		"aster":       true,
		"backpack":    true,
		"batonex":     true,
		"bingx":       true,
		"kucoin":      true,
		"bitget":      true,
		"zoomex":      true,
		"deepcoin":    true,
		"toobit":      true,
		"bitmart":     true,
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

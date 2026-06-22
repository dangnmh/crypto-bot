package krakenfutures_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/krakenfutures"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	t.Run("success parsing and filtering", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/derivatives/api/v3/tickers", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"result": "success",
				"tickers": [
					{
						"tag": "perpetual",
						"pair": "XBT:USD",
						"symbol": "PF_XBTUSD",
						"markPrice": 30209.9,
						"vol24h": 15304,
						"volumeQuote": 7305.2,
						"fundingRate": 0.0125
					},
					{
						"tag": "perpetual",
						"pair": "ETH:USD",
						"symbol": "PF_ETHUSD",
						"markPrice": 1800.0,
						"vol24h": 200,
						"volumeQuote": 360000.0,
						"fundingRate": -0.005
					},
					{
						"tag": "month",
						"pair": "XBT:USD",
						"symbol": "FI_XBTUSD_211231",
						"markPrice": 20478.5,
						"vol24h": 100,
						"volumeQuote": 843.9
					}
				],
				"serverTime": "2022-06-17T11:00:31.335Z"
			}`))
		}))
		defer server.Close()

		client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})
		results, err := client.GetPotentialFundingSymbols(context.Background(), 1000, 0, nil, nil)
		require.NoError(t, err)

		// Only perpetual futures PF_XBTUSD (mapped to BTCUSD) and PF_ETHUSD (mapped to ETHUSD) should remain.
		// Monthly contract FI_XBTUSD_211231 is filtered out.
		// PF_XBTUSD has volume 7305.2 (above 1000), so it is present.
		// PF_ETHUSD has volume 360000 (above 1000), so it is present.
		require.Len(t, results, 2)

		// Verification for PF_XBTUSD -> BTCUSD
		assert.Equal(t, "BTCUSD", results[0].Symbol)
		assert.Equal(t, 0.000125, results[0].Rate)
		assert.Equal(t, 7305.2, results[0].Volume24h)

		// SettleTime is 2022-06-17T11:00:31.335Z rounded up to 12:00:00Z
		expectedSettle := time.Date(2022, 6, 17, 12, 0, 0, 0, time.UTC).UnixMilli()
		assert.Equal(t, expectedSettle, results[0].SettleTime)

		// Verification for PF_ETHUSD -> ETHUSD
		assert.Equal(t, "ETHUSD", results[1].Symbol)
		assert.Equal(t, -0.00005, results[1].Rate)
		assert.Equal(t, 360000.0, results[1].Volume24h)
		assert.Equal(t, expectedSettle, results[1].SettleTime)
	})

	t.Run("whitelist and blacklist", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"result": "success",
				"tickers": [
					{"tag": "perpetual", "symbol": "PF_XBTUSD", "volumeQuote": 50000.0, "fundingRate": 0.01},
					{"tag": "perpetual", "symbol": "PF_ETHUSD", "volumeQuote": 60000.0, "fundingRate": 0.02},
					{"tag": "perpetual", "symbol": "PF_SOLUSD", "volumeQuote": 70000.0, "fundingRate": 0.03}
				],
				"serverTime": "2022-06-17T11:00:00Z"
			}`))
		}))
		defer server.Close()

		client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})

		// Whitelist BTCUSD (standardized name for XBT), blacklist ETHUSD -> Only BTCUSD should remain
		results, err := client.GetPotentialFundingSymbols(context.Background(), 0, 0, []string{"BTCUSD"}, []string{"ETHUSD"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "BTCUSD", results[0].Symbol)
	})

	t.Run("volume filtering limit", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"result": "success",
				"tickers": [
					{"tag": "perpetual", "symbol": "PF_XBTUSD", "volumeQuote": 50000.0, "fundingRate": 0.01},
					{"tag": "perpetual", "symbol": "PF_ETHUSD", "volumeQuote": 150000.0, "fundingRate": 0.02}
				],
				"serverTime": "2022-06-17T11:00:00Z"
			}`))
		}))
		defer server.Close()

		client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})

		// minVol 100,000, maxVol 200,000 -> Only ETHUSD should remain
		results, err := client.GetPotentialFundingSymbols(context.Background(), 100000, 200000, nil, nil)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "ETHUSD", results[0].Symbol)
	})
}

func TestClient_ServerTime(t *testing.T) {
	t.Parallel()

	client := krakenfutures.NewClient(nil, "https://futures.kraken.com", config.LoggingConfig{})
	serverTime, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.True(t, serverTime > 0)
}

package config_test

import (
	"encoding/json"
	"testing"

	"crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointConfig_DirectMapping(t *testing.T) {
	t.Parallel()
	rawJSON := `{
		"enable": true,
		"baseURL": "https://contract.mexc.com",
		"websocket": {
			"publicURL": "wss://contract.mexc.com/edge",
			"privateURL": "wss://contract.mexc.com/edge",
			"maxPairsPerWSConn": 30
		}
	}`

	var ep config.EndpointConfig
	err := json.Unmarshal([]byte(rawJSON), &ep)
	require.NoError(t, err)

	assert.True(t, ep.IsEnabled())
	assert.Equal(t, "https://contract.mexc.com", ep.BaseURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", ep.WebSocket.PublicURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", ep.WebSocket.PrivateURL)
	assert.Equal(t, 30, ep.WebSocket.MaxPairsPerWSConn)
	assert.Equal(t, "http", ep.GetTradeMode())
}

func TestExchangeConfig_ExactNameMap(t *testing.T) {
	t.Parallel()
	rawJSON := `{
		"mexc_futures": {
			"enable": true,
			"baseURL": "https://api.mexc.com",
			"websocket": {
				"publicURL": "wss://contract.mexc.com/edge",
				"privateURL": "wss://contract.mexc.com/edge",
				"maxPairsPerWSConn": 30
			}
		},
		"bybit_futures": {
			"enable": false,
			"baseURL": "https://api.bybit.com",
			"accountType": "unified",
			"websocket": {
				"publicURL": "wss://stream.bybit.com/v5/public/linear",
				"privateURL": "wss://stream.bybit.com/v5/private"
			}
		}
	}`

	var exchMap config.ExchangeConfig
	err := json.Unmarshal([]byte(rawJSON), &exchMap)
	require.NoError(t, err)

	require.Contains(t, exchMap, "mexc_futures")
	assert.True(t, exchMap["mexc_futures"].IsEnabled())
	assert.Equal(t, "https://api.mexc.com", exchMap["mexc_futures"].BaseURL)

	require.Contains(t, exchMap, "bybit_futures")
	assert.False(t, exchMap["bybit_futures"].IsEnabled())
	assert.Equal(t, "unified", exchMap["bybit_futures"].AccountType)
}

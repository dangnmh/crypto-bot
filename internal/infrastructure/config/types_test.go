package config_test

import (
	"encoding/json"
	"testing"

	"crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIConfigSpotFutureToggle(t *testing.T) {
	t.Parallel()
	rawJSON := `{
		"enable": true,
		"spot": {
			"enable": true,
			"baseURL": "https://api.mexc.com",
			"websocket": {
				"wsURL": "wss://wbs.mexc.com/ws",
				"maxPairsPerWSConn": 30
			}
		},
		"future": {
			"enable": false,
			"baseURL": "https://contract.mexc.com",
			"websocket": {
				"wsURL": "wss://contract.mexc.com/edge",
				"maxPairsPerWSConn": 30
			}
		}
	}`

	var apiCfg config.APIConfig
	err := json.Unmarshal([]byte(rawJSON), &apiCfg)
	require.NoError(t, err)

	assert.True(t, apiCfg.IsEnabled())
	assert.True(t, apiCfg.Spot.Enable)
	assert.Equal(t, "https://api.mexc.com", apiCfg.Spot.BaseURL)
	assert.False(t, apiCfg.Future.Enable)
	assert.Equal(t, "https://contract.mexc.com", apiCfg.Future.BaseURL)
	assert.Equal(t, "wss://wbs.mexc.com/ws", apiCfg.Spot.WebSocket.WSURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", apiCfg.Future.WebSocket.WSURL)
}

func TestAPIConfig_GetSpotAndFutureEndpoints(t *testing.T) {
	t.Parallel()
	rawJSON := `{
		"enable": true,
		"spot": {
			"enable": true,
			"baseURL": "https://api.mexc.com",
			"websocket": {
				"wsURL": "wss://wbs.mexc.com/ws",
				"maxPairsPerWSConn": 30
			}
		},
		"future": {
			"enable": false,
			"baseURL": "https://contract.mexc.com",
			"websocket": {
				"wsURL": "wss://contract.mexc.com/edge",
				"maxPairsPerWSConn": 30
			}
		}
	}`

	var apiCfg config.APIConfig
	err := json.Unmarshal([]byte(rawJSON), &apiCfg)
	require.NoError(t, err)

	spotEP := apiCfg.GetSpotEndpoint()
	assert.True(t, spotEP.Enable)
	assert.Equal(t, "https://api.mexc.com", spotEP.BaseURL)
	assert.Equal(t, "wss://wbs.mexc.com/ws", spotEP.WebSocket.WSURL)

	futEP := apiCfg.GetFutureEndpoint()
	assert.False(t, futEP.Enable)
	assert.Equal(t, "https://contract.mexc.com", futEP.BaseURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", futEP.WebSocket.WSURL)
}

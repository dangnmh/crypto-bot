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
				"publicURL": "wss://wbs-api.mexc.com/ws",
				"privateURL": "wss://wbs-api.mexc.com/ws",
				"maxPairsPerWSConn": 30
			}
		},
		"future": {
			"enable": false,
			"baseURL": "https://contract.mexc.com",
			"websocket": {
				"publicURL": "wss://contract.mexc.com/edge",
				"privateURL": "wss://contract.mexc.com/edge",
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
	assert.Equal(t, "wss://wbs-api.mexc.com/ws", apiCfg.Spot.WebSocket.PublicURL)
	assert.Equal(t, "wss://wbs-api.mexc.com/ws", apiCfg.Spot.WebSocket.PrivateURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", apiCfg.Future.WebSocket.PublicURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", apiCfg.Future.WebSocket.PrivateURL)
}

func TestAPIConfig_GetSpotAndFutureEndpoints(t *testing.T) {
	t.Parallel()
	rawJSON := `{
		"enable": true,
		"spot": {
			"enable": true,
			"baseURL": "https://api.mexc.com",
			"websocket": {
				"publicURL": "wss://wbs-api.mexc.com/ws",
				"privateURL": "wss://wbs-api.mexc.com/ws",
				"maxPairsPerWSConn": 30
			}
		},
		"future": {
			"enable": false,
			"baseURL": "https://contract.mexc.com",
			"websocket": {
				"publicURL": "wss://contract.mexc.com/edge",
				"privateURL": "wss://contract.mexc.com/edge",
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
	assert.Equal(t, "wss://wbs-api.mexc.com/ws", spotEP.WebSocket.PublicURL)
	assert.Equal(t, "wss://wbs-api.mexc.com/ws", spotEP.WebSocket.PrivateURL)

	futEP := apiCfg.GetFutureEndpoint()
	assert.False(t, futEP.Enable)
	assert.Equal(t, "https://contract.mexc.com", futEP.BaseURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", futEP.WebSocket.PublicURL)
	assert.Equal(t, "wss://contract.mexc.com/edge", futEP.WebSocket.PrivateURL)
}

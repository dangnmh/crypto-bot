package xt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_BuildStringToSign(t *testing.T) {
	t.Parallel()

	// Example from documentation breakdown:
	// APPKEY = "++++++"
	// timestamp = "*****"
	// end_point = "/future/user/v1/balance/detail"
	// query_string = "coin=btc"
	// unencrypted_string = "validate-appkey=++++++&validate-timestamp=*****#/future/user/v1/balance/detail#coin=btc"

	toSign := buildStringToSign("++++++", "*****", "/future/user/v1/balance/detail", "coin=btc", "")
	assert.Equal(t, "validate-appkey=++++++&validate-timestamp=*****#/future/user/v1/balance/detail#coin=btc", toSign)

	// No params case
	toSignNoParams := buildStringToSign("++++++", "*****", "/future/user/v1/user/listen-key", "", "")
	assert.Equal(t, "validate-appkey=++++++&validate-timestamp=*****#/future/user/v1/user/listen-key", toSignNoParams)

	// Body case
	toSignBody := buildStringToSign("++++++", "*****", "/future/user/v1/user/collection/add", "", `{"symbol":"btc_usdt"}`)
	assert.Equal(t, `validate-appkey=++++++&validate-timestamp=*****#/future/user/v1/user/collection/add#{"symbol":"btc_usdt"}`, toSignBody)
}

func TestClient_RawGetHistoryPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/future/trade/v1/position/list-history", r.URL.Path)
		_, _ = w.Write([]byte(`{
			"returnCode": 0,
			"msgInfo": "success",
			"result": {
				"hasNext": false,
				"hasPrev": false,
				"items": [
					{
						"id": "1987654321098765432",
						"positionSide": "LONG",
						"symbol": "BTCUSDT",
						"positionType": 2,
						"closeProfit": "127.83",
						"closePositionSize": "0.050",
						"closeOpenPrice": "63250.80",
						"closePrice": "65808.60"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "my-key", "my-secret", config.LoggingConfig{})
	res, err := client.rawGetHistoryPositions(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "1987654321098765432", res[0].ID)
	assert.Equal(t, "LONG", res[0].PositionSide)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)
	assert.Equal(t, 2, res[0].PositionType)
	assert.Equal(t, "127.83", res[0].CloseProfit.String())
}

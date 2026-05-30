package ws_test

import (
	"encoding/json"
	"testing"

	"crypto-bot/internal/infrastructure/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessage_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := ws.Message{
		Channel: "push.ticker",
		Symbol:  "BTC_USDT",
		Data:    json.RawMessage(`{"lastPrice":50000}`),
		Ts:      1609459200000,
	}

	// Marshal.
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal.
	var decoded ws.Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Channel, decoded.Channel)
	assert.Equal(t, original.Symbol, decoded.Symbol)
	assert.Equal(t, original.Ts, decoded.Ts)
	assert.JSONEq(t, `{"lastPrice":50000}`, string(decoded.Data))
}

func TestMessage_OmitEmpty(t *testing.T) {
	t.Parallel()
	// Symbol, Data, and Ts are omitempty.
	msg := ws.Message{Channel: "pong"}
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// Verify omitted fields are not present.
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Contains(t, raw, "channel")
	assert.NotContains(t, raw, "symbol")
	assert.NotContains(t, raw, "data")
	assert.NotContains(t, raw, "ts")
}

func TestMessage_UnmarshalFromWS(t *testing.T) {
	t.Parallel()
	wsPayload := `{"channel":"push.depth.full","symbol":"ETH_USDT","data":{"asks":[[3000,5]],"bids":[[2999,10]],"version":42},"ts":1700000000}`

	var msg ws.Message
	err := json.Unmarshal([]byte(wsPayload), &msg)
	require.NoError(t, err)

	assert.Equal(t, "push.depth.full", msg.Channel)
	assert.Equal(t, "ETH_USDT", msg.Symbol)
	assert.Equal(t, int64(1700000000), msg.Ts)
	assert.NotEmpty(t, msg.Data)
}

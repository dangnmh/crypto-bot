package xjson_test

import (
	"encoding/json"
	"testing"

	"crypto-bot/pkg/xjson"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Symbol   string      `json:"symbol"`
	Price    json.Number `json:"price"`
	Quantity json.Number `json:"qty"`
	Volume   float64     `json:"volume"`
	Nested   *testNested `json:"nested"`
}

type testNested struct {
	Val int `json:"val"`
}

type collisionStruct struct {
	Event string `json:"e"`
	Time  int    `json:"E"`
}

func TestUnmarshal_Collision(t *testing.T) {
	t.Parallel()

	input := `{"e": "outboundContractPositionInfo", "E": 123456}`

	var res collisionStruct
	err := xjson.Unmarshal([]byte(input), &res)
	require.NoError(t, err)

	assert.Equal(t, "outboundContractPositionInfo", res.Event)
	assert.Equal(t, 123456, res.Time)
}

func TestUnmarshal_Slice(t *testing.T) {
	t.Parallel()

	input := `[
		{"symbol": "BTC"},
		{"symbol": "ETH"}
	]`

	var res []testStruct
	err := xjson.Unmarshal([]byte(input), &res)
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, "BTC", res[0].Symbol)
	assert.Equal(t, "ETH", res[1].Symbol)
}

func TestMarshalUnmarshal(t *testing.T) {
	t.Parallel()

	val := testStruct{
		Symbol:   "BTCUSDT",
		Price:    json.Number("12.34"),
		Quantity: json.Number("5"),
		Volume:   100.5,
		Nested:   &testNested{Val: 42},
	}

	data, err := xjson.Marshal(val)
	require.NoError(t, err)

	var decoded testStruct
	err = xjson.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, val.Symbol, decoded.Symbol)
	assert.Equal(t, val.Price, decoded.Price)
	assert.Equal(t, val.Quantity, decoded.Quantity)
	assert.Equal(t, val.Volume, decoded.Volume)
	assert.Equal(t, val.Nested.Val, decoded.Nested.Val)
}

func TestNumberHelpers(t *testing.T) {
	t.Parallel()

	n := json.Number("123.45")
	assert.Equal(t, 123.45, xjson.ToFloat64(n))

	n2 := json.Number("678")
	assert.Equal(t, int64(678), xjson.ToInt64(n2))
}

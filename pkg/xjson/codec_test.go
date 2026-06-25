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

func TestNumber(t *testing.T) {
	t.Parallel()

	// Test UnmarshalJSON
	tests := []struct {
		name      string
		input     string
		expectErr bool
		expected  xjson.Number
	}{
		{"raw int", `123`, false, xjson.Number("123")},
		{"raw float", `123.45`, false, xjson.Number("123.45")},
		{"string int", `"123"`, false, xjson.Number("123")},
		{"string float", `"123.45"`, false, xjson.Number("123.45")},
		{"bool true", `true`, false, xjson.Number("true")},
		{"bool false", `false`, false, xjson.Number("false")},
		{"null", `null`, false, xjson.Number("")},
		{"invalid type object", `{"foo":"bar"}`, true, xjson.Number("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n xjson.Number
			err := json.Unmarshal([]byte(tt.input), &n)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, n)
			}
		})
	}

	// Test MarshalJSON
	marshalTests := []struct {
		name     string
		input    xjson.Number
		expected string
	}{
		{"empty", xjson.Number(""), "0"},
		{"int", xjson.Number("123"), "123"},
		{"float", xjson.Number("123.45"), "123.45"},
		{"string format", xjson.Number("abc"), `"abc"`},
	}

	for _, tt := range marshalTests {
		t.Run("marshal_"+tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}

	// Test methods
	n := xjson.Number("123.45")
	f, err := n.Float64()
	require.NoError(t, err)
	assert.Equal(t, 123.45, f)

	i, err := xjson.Number("123").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(123), i)

	assert.Equal(t, "123.45", n.String())

	// Test default / empty method behaviors
	nEmpty := xjson.Number("")
	fEmpty, err := nEmpty.Float64()
	require.NoError(t, err)
	assert.Equal(t, 0.0, fEmpty)

	iEmpty, err := nEmpty.Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(0), iEmpty)

	assert.Equal(t, "", nEmpty.String())
}


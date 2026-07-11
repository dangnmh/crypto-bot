package gate_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/gate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type orderMappingTestCase struct {
	name              string
	req               exchange.SubmitOrderRequest
	wantSize          int64
	wantPrice         string
	wantTif           string
	wantClose         bool
	wantAuto          string
	wantTP            string
	wantSL            string
	wantTPSLSubmitted bool
}

func TestClient_CreateOrder_Mapping(t *testing.T) {
	t.Parallel()

	tests := []orderMappingTestCase{
		{
			name: "Open Long Limit (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
			},
			wantSize:  2,
			wantPrice: "50000",
			wantTif:   "gtc",
		},
		{
			name: "Open Short Market (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideOpenShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  -2,
			wantPrice: "0",
			wantTif:   "ioc",
		},
		{
			name: "Close Long (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideCloseLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  0,
			wantPrice: "0",
			wantTif:   "ioc",
			wantAuto:  "close_long",
		},
		{
			name: "Close Short (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          2,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSize:  0,
			wantPrice: "0",
			wantTif:   "ioc",
			wantAuto:  "close_short",
		},
		{
			name: "Open Long Market (OneWay)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          3,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // OneWay
				ReduceOnly:   true,
			},
			wantSize:  3,
			wantPrice: "0",
			wantTif:   "ioc",
			wantClose: true,
		},
		{
			name: "Open Short Market (OneWay)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTC_USDT",
				Vol:          3,
				Side:         exchange.SideOpenShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // OneWay
			},
			wantSize:  -3,
			wantPrice: "0",
			wantTif:   "ioc",
		},
		{
			name: "Open Long Limit with TP/SL (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:          "BTC_USDT",
				Vol:             2,
				Side:            exchange.SideOpenLong,
				Type:            exchange.OrderTypeLimit,
				Price:           50000.0,
				PositionMode:    1, // Hedge
				TakeProfitPrice: 55000.0,
				StopLossPrice:   45000.0,
			},
			wantSize:          2,
			wantPrice:         "50000",
			wantTif:           "gtc",
			wantTP:            "55000",
			wantSL:            "45000",
			wantTPSLSubmitted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runOrderMappingTest(t, tt)
		})
	}
}

func runOrderMappingTest(t *testing.T, tt orderMappingTestCase) {
	// Spin up a mock HTTP server to intercept the POST request to Gate.io API.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/futures/usdt/orders")

		// Read body bytes for signature verification
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Verify Signature validation
		key := r.Header.Get("KEY")
		timestamp := r.Header.Get("Timestamp")
		signature := r.Header.Get("SIGN")
		if key != "" && timestamp != "" && signature != "" {
			verifyMockSignature(t, r, bodyBytes, timestamp, signature)
		}

		// Decode the request body (which is gateFuturesOrder).
		var body map[string]any
		err = json.Unmarshal(bodyBytes, &body)
		require.NoError(t, err)

		// Verify fields inside the serialized request.
		assert.Equal(t, tt.req.Symbol, body["contract"])

		// Assert price and size.
		assert.Equal(t, tt.wantPrice, body["price"])

		assertOrderSize(t, body, tt.wantSize)

		if tt.wantTif != "" {
			assert.Equal(t, tt.wantTif, body["tif"])
		}

		if tt.wantClose {
			assert.Equal(t, true, body["close"])
		}

		if tt.wantAuto != "" {
			assert.Equal(t, tt.wantAuto, body["auto_size"])
		}

		if tt.wantTP != "" {
			assert.Equal(t, tt.wantTP, body["tpsl_tp_trigger_price"])
			assert.Equal(t, "last", body["tpsl_tp_price_type"])
		}

		if tt.wantSL != "" {
			assert.Equal(t, tt.wantSL, body["tpsl_sl_trigger_price"])
			assert.Equal(t, "last", body["tpsl_sl_price_type"])
		}

		// Return a successful order response with ID 987654.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 987654}`))
	}))
	defer server.Close()

	// Create client pointed to our mock server.
	client := gate.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	res, err := client.CreateOrder(context.Background(), tt.req)
	require.NoError(t, err)
	assert.Equal(t, "987654", res.OrderID)
	assert.Equal(t, tt.wantTPSLSubmitted, res.TPSLSubmitted)
}

func verifyMockSignature(t *testing.T, r *http.Request, bodyBytes []byte, timestamp, signature string) {
	h := sha512.New()
	h.Write(bodyBytes)
	hashedPayload := hex.EncodeToString(h.Sum(nil))

	rawQuery, err := url.QueryUnescape(r.URL.RawQuery)
	require.NoError(t, err)
	msg := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", r.Method, r.URL.Path, rawQuery, hashedPayload, timestamp)
	mac := hmac.New(sha512.New, []byte("api_secret"))
	mac.Write([]byte(msg))
	expectedSign := hex.EncodeToString(mac.Sum(nil))

	assert.Equal(t, expectedSign, signature)
}

func assertOrderSize(t *testing.T, body map[string]any, wantSize int64) {
	sizeVal, exists := body["size"]
	if wantSize != 0 {
		require.True(t, exists)
		val, ok := sizeVal.(float64)
		require.True(t, ok, "size must be a float64")
		assert.Equal(t, float64(wantSize), val)
	} else if exists && sizeVal != nil {
		val, ok := sizeVal.(float64)
		require.True(t, ok, "size must be a float64")
		assert.Equal(t, float64(0), val)
	}
}

func TestClient_GateRemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/futures/usdt/contracts":
			_, _ = w.Write([]byte(`[{"name":"BTC_USDT","settle":"usdt","config_leverage_limit":100}]`))
		case "/futures/usdt/tickers":
			_, _ = w.Write([]byte(`[{"contract":"BTC_USDT","volume_24h_quote":"15000000","last":"64000"}]`))
		case "/futures/usdt/positions/BTC_USDT/margin_mode":
			_, _ = w.Write([]byte(`{"contract":"BTC_USDT","margin_mode":"crossed"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	client := gate.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	// 1. GetPotentialFundingSymbols
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTC_USDT", res[0].Symbol)

	// 2. SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTC_USDT", domain.MarginModeCross, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// 3. SupportLeverageOnOrder
	assert.False(t, client.SupportLeverageOnOrder())

	// 4. Raw methods
	_, _ = client.GetHistoryPositionsRaw(context.Background(), nil)
	_, _ = client.GetHistoryOrdersRaw(context.Background(), nil)
	_, _ = client.GetOrderPNLRaw(context.Background(), nil)
}

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		start         time.Time
		end           time.Time
		expectedFrom  string
		expectedTo    string
		expectedLimit string
	}{
		{
			name:          "both start and end specified - limit omitted",
			start:         time.Unix(1783665240, 0),
			end:           time.Unix(1783681200, 0),
			expectedFrom:  "1783665240",
			expectedTo:    "1783681200",
			expectedLimit: "",
		},
		{
			name:          "only start specified - limit set to 100",
			start:         time.Unix(1783665240, 0),
			end:           time.Time{},
			expectedFrom:  "1783665240",
			expectedTo:    "",
			expectedLimit: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/futures/usdt/candlesticks", r.URL.Path)

				q := r.URL.Query()
				assert.Equal(t, "BTC_USDT", q.Get("contract"))
				assert.Equal(t, "1m", q.Get("interval"))
				assert.Equal(t, tt.expectedFrom, q.Get("from"))
				assert.Equal(t, tt.expectedTo, q.Get("to"))
				assert.Equal(t, tt.expectedLimit, q.Get("limit"))

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[
					{"t": 1783665240, "v": 668, "c": "63888.7", "h": "63889.5", "l": "63875.1", "o": "63889.5", "sum": "42677651"}
				]`))
			}))
			defer server.Close()

			client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

			klines, err := client.FetchKlines(
				context.Background(),
				"BTC_USDT",
				exchange.Interval1m,
				tt.start,
				tt.end,
			)
			require.NoError(t, err)
			require.Len(t, klines, 1)

			assert.Equal(t, int64(1783665240000), klines[0].Timestamp)
			assert.Equal(t, 63888.7, klines[0].Close)
		})
	}
}

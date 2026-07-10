# Deepcoin Futures FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the Deepcoin client and verify it using unit tests and factory integration tests.

---

### Task 1: Write Unit Tests and Implement FetchKlines on Deepcoin REST Client

**Files:**
- Create: `internal/infrastructure/exchange/deepcoin/klines_test.go`
- Modify: `internal/infrastructure/exchange/deepcoin/klines.go`

- [x] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/deepcoin/klines_test.go` containing:
  ```go
  package deepcoin_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/deepcoin"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/deepcoin/market/candles", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTC-USDT-SWAP", q.Get("instId"))
  		assert.Equal(t, "1H", q.Get("bar"))
  		assert.Equal(t, "1783681200000", q.Get("after"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "0",
  			"msg": "",
  			"data": [
  				[
  					"1783681200000",
  					"64375.7",
  					"64456",
  					"64302.4",
  					"64436.1",
  					"448016",
  					"28841508.980000008"
  				],
  				[
  					"1783677600000",
  					"64362.2",
  					"64478.7",
  					"64246.5",
  					"64375.7",
  					"936968",
  					"60303388.4182006"
  				]
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC-USDT-SWAP",
  		exchange.Interval1h,
  		time.Time{},
  		time.UnixMilli(1783681200000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
  	assert.Equal(t, 64375.7, klines[0].Open)
  	assert.Equal(t, 64456.0, klines[0].High)
  	assert.Equal(t, 64302.4, klines[0].Low)
  	assert.Equal(t, 64436.1, klines[0].Close)
  	assert.Equal(t, 448016.0, klines[0].Volume)

  	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
  	assert.Equal(t, 64362.2, klines[1].Open)
  	assert.Equal(t, 64478.7, klines[1].High)
  	assert.Equal(t, 64246.5, klines[1].Low)
  	assert.Equal(t, 64375.7, klines[1].Close)
  	assert.Equal(t, 936968.0, klines[1].Volume)
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "10001",
  			"msg": "invalid symbol"
  		}`))
  	}))
  	defer server.Close()

  	client := deepcoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
  	_, err := client.FetchKlines(
  		context.Background(),
  		"INVALID",
  		exchange.Interval1h,
  		time.Time{},
  		time.Time{},
  	)
  	assert.Error(t, err)
  }
  ```

- [x] **Step 2: Run test to verify it fails**
  Run command: `go test -v ./internal/infrastructure/exchange/deepcoin/... -run TestClient_FetchKlines`
  Expected: FAIL

- [x] **Step 3: Write minimal implementation**
  Replace `internal/infrastructure/exchange/deepcoin/klines.go` with:
  ```go
  package deepcoin

  import (
  	"context"
  	"fmt"
  	"net/http"
  	"strconv"
  	"time"

  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/pkg/xjson"
  )

  var intervalMap = map[exchange.Interval]string{
  	exchange.Interval1m:  "1m",
  	exchange.Interval3m:  "1m",
  	exchange.Interval5m:  "5m",
  	exchange.Interval15m: "15m",
  	exchange.Interval30m: "30m",
  	exchange.Interval1h:  "1H",
  	exchange.Interval2h:  "1H",
  	exchange.Interval4h:  "4H",
  	exchange.Interval6h:  "4H",
  	exchange.Interval8h:  "4H",
  	exchange.Interval12h: "12H",
  	exchange.Interval1d:  "1D",
  	exchange.Interval1w:  "1W",
  	exchange.Interval1M:  "1M",
  }

  func mapDeepcoinInterval(interval exchange.Interval) string {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return "1m"
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
  	params := map[string]string{
  		"instId": symbol,
  		"bar":    mapDeepcoinInterval(interval),
  	}
  	if !end.IsZero() {
  		params["after"] = strconv.FormatInt(end.UnixMilli(), 10)
  	}

  	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/candles", params, nil)
  	if err != nil {
  		return nil, err
  	}

  	return ParseResponse[[]xjson.Number](body, "klines")
  }

  // FetchKlines fetches public K-lines for deepcoin.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("deepcoin fetch klines: %w", err)
  	}

  	klines := make([]exchange.Kline, 0, len(rawKlines))
  	for _, k := range rawKlines {
  		if len(k) < 6 {
  			continue
  		}
  		klines = append(klines, exchange.Kline{
  			Timestamp: xjson.ToInt64(k[0]),
  			Open:      xjson.ToFloat64(k[1]),
  			High:      xjson.ToFloat64(k[2]),
  			Low:       xjson.ToFloat64(k[3]),
  			Close:     xjson.ToFloat64(k[4]),
  			Volume:    xjson.ToFloat64(k[5]),
  		})
  	}

  	return klines, nil
  }
  ```

- [x] **Step 4: Run unit tests to verify they pass**
  Run command: `go test -v ./internal/infrastructure/exchange/deepcoin/... -run TestClient_FetchKlines`
  Expected: PASS

- [x] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/deepcoin/klines.go && git add internal/infrastructure/exchange/deepcoin/klines_test.go && git commit -m "feat(deepcoin): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: `internal/infrastructure/app/client_factory_test.go`

- [x] **Step 1: Modify the factory test condition**
  Add `"deepcoin"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport`:
  ```go
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
	}
  ```

- [x] **Step 2: Run tests to verify all pass**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [x] **Step 3: Run full package quality checks**
  Run commands:
  `make lint`
  `make test`
  `make cover`

- [x] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register deepcoin in FetchKlines support tests"`

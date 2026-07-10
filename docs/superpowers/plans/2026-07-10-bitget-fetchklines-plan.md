# Bitget Futures FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the Bitget V2 Futures client and verify it using unit tests and factory integration tests.

**Architecture:** We will implement the `FetchKlines` method in `internal/infrastructure/exchange/bitget/klines.go`. Following the "Raw Request Wrapper Rule", we will define a private `rawGetKlines` helper that calls `GET /api/v2/mix/market/candles` with `productType: "USDT-FUTURES"` and parses the envelope response. The public `FetchKlines` method will map standard timeframes, call the raw helper, and convert the heterogeneous list values safely to the standard `exchange.Kline` struct using `xjson.Number`.

**Tech Stack:** Go (1.21+), Testify (`assert`, `require`), `httptest.NewServer` for mocking, `crypto-bot/pkg/xjson`.

## Global Constraints
- Target codebase-wide coverage threshold is >= 75.0%.
- We must strictly follow Clean Architecture and the 6-file REST client layout conventions.
- Standard Go unmarshaling/marshaling uses `crypto-bot/pkg/xjson`.
- Direct REST requests must be wrapped with a private `raw` function prefix.

---

### Task 1: Write Unit Tests and Implement FetchKlines on Bitget REST Client

**Files:**
- Create: `internal/infrastructure/exchange/bitget/klines_test.go`
- Modify: `internal/infrastructure/exchange/bitget/klines.go`

**Interfaces:**
- Produces: `func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error)`

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/bitget/klines_test.go` containing:
  ```go
  package bitget_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/bitget"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/api/v2/mix/market/candles", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
  		assert.Equal(t, "USDT-FUTURES", q.Get("productType"))
  		assert.Equal(t, "1H", q.Get("granularity"))
  		assert.Equal(t, "1783674000000", q.Get("startTime"))
  		assert.Equal(t, "1783681200000", q.Get("endTime"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "00000",
  			"msg": "success",
  			"data": [
  				[
  					"1783674000000",
  					"64199.4",
  					"64438",
  					"64060.6",
  					"64340.5",
  					"1804.7384",
  					"116077022.09106"
  				],
  				[
  					"1783677600000",
  					"64340.5",
  					"64455.5",
  					"64220.1",
  					"64359.8",
  					"1122.0411",
  					"72199775.3255"
  				]
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSDT",
  		exchange.Interval1h,
  		time.UnixMilli(1783674000000),
  		time.UnixMilli(1783681200000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783674000000), klines[0].Timestamp)
  	assert.Equal(t, 64199.4, klines[0].Open)
  	assert.Equal(t, 64438.0, klines[0].High)
  	assert.Equal(t, 64060.6, klines[0].Low)
  	assert.Equal(t, 64340.5, klines[0].Close)
  	assert.Equal(t, 1804.7384, klines[0].Volume)

  	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
  	assert.Equal(t, 64340.5, klines[1].Open)
  	assert.Equal(t, 64455.5, klines[1].High)
  	assert.Equal(t, 64220.1, klines[1].Low)
  	assert.Equal(t, 64359.8, klines[1].Close)
  	assert.Equal(t, 1122.0411, klines[1].Volume)
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "400001",
  			"msg": "invalid symbol"
  		}`))
  	}))
  	defer server.Close()

  	client := bitget.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
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

- [ ] **Step 2: Run test to verify it fails**
  Run command: `go test -v ./internal/infrastructure/exchange/bitget/... -run TestClient_FetchKlines`
  Expected: FAIL (FetchKlines stub returns "bitget does not support FetchKlines")

- [ ] **Step 3: Write minimal implementation**
  Replace `internal/infrastructure/exchange/bitget/klines.go` with:
  ```go
  package bitget

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
  	exchange.Interval3m:  "3m",
  	exchange.Interval5m:  "5m",
  	exchange.Interval15m: "15m",
  	exchange.Interval30m: "30m",
  	exchange.Interval1h:  "1H",
  	exchange.Interval2h:  "1H",
  	exchange.Interval4h:  "4H",
  	exchange.Interval6h:  "6H",
  	exchange.Interval8h:  "6H",
  	exchange.Interval12h: "12H",
  	exchange.Interval1d:  "1D",
  	exchange.Interval1w:  "1W",
  	exchange.Interval1M:  "1M",
  }

  func mapBitgetInterval(interval exchange.Interval) string {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return "1m"
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
  	params := map[string]string{
  		paramSymbol:        symbol,
  		paramProductType:   productTypeUsdtFutures,
  		"granularity":      mapBitgetInterval(interval),
  	}
  	if !start.IsZero() {
  		params["startTime"] = strconv.FormatInt(start.UnixMilli(), 10)
  	}
  	if !end.IsZero() {
  		params["endTime"] = strconv.FormatInt(end.UnixMilli(), 10)
  	}

  	body, err := c.RawRequest(ctx, http.MethodGet, pathKlines, params, nil)
  	if err != nil {
  		return nil, err
  	}

  	return ParseResponse[[][]xjson.Number](body, "klines")
  }

  // FetchKlines fetches public K-lines for bitget.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("bitget fetch klines: %w", err)
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

- [ ] **Step 4: Run unit tests to verify they pass**
  Run command: `go test -v ./internal/infrastructure/exchange/bitget/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/bitget/klines.go && git add internal/infrastructure/exchange/bitget/klines_test.go && git commit -m "feat(bitget): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: `internal/infrastructure/app/client_factory_test.go:47`

- [ ] **Step 1: Modify the factory test condition**
  Add `"bitget"` to the list of supported exchanges inside `TestAllExchangesFetchKlinesSupport`:
  ```go
  if name == "binance" || name == "bybit" || name == "gate" || name == "mexc" || name == "okx" || name == "hyperliquid" || name == "apex" || name == "aster" || name == "backpack" || name == "batonex" || name == "bingx" || name == "kucoin" || name == "bitget" {
  ```

- [ ] **Step 2: Run tests to verify all pass**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 3: Run full package quality checks**
  Run commands:
  `make lint`
  `make test`
  `make cover`
  Expected: All checks pass and code coverage remains >= 75.0%.

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register bitget in FetchKlines support tests"`

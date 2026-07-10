# BitMart FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the BitMart REST client and verify it using unit tests and factory integration tests.

**Architecture:** We will implement the `FetchKlines` method on BitMart's REST `Client` under `internal/infrastructure/exchange/bitmart` satisfying the `KlineProvider` interface. This method will fetch candles from BitMart's public market GET `/contract/public/kline` endpoint, parse the wrapped JSON object, and map it to `exchange.Kline` structs (converting timestamps from seconds to milliseconds).

**Tech Stack:** Go (Golang), standard library `net/http`, project testing utilities (`httptest`, `testify`).

## Global Constraints
- Clean Architecture / DDD conventions in `docs/tech/architecture.md`.
- Quality Gates must pass: `make lint`, `make test`, `make cover`.

---

### Task 1: Write Unit Tests and Implement FetchKlines on BitMart REST Client

**Files:**
- Create: [klines_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/bitmart/klines_test.go)
- Modify: [klines.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/bitmart/klines.go)

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/bitmart/klines_test.go` with mock httptest server matching BitMart REST `/contract/public/kline` response format:
  ```go
  package bitmart_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/bitmart"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/contract/public/kline", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
  		assert.Equal(t, "60", q.Get("step"))
  		assert.Equal(t, "1783681200", q.Get("start_time"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": 1000,
  			"message": "Ok",
  			"data": [
  				{
  					"low_price": "64309",
  					"high_price": "64450",
  					"open_price": "64374",
  					"close_price": "64394.2",
  					"volume": "542186",
  					"timestamp": 1783681200
  				},
  				{
  					"low_price": "64241.4",
  					"high_price": "64580",
  					"open_price": "64399.9",
  					"close_price": "64258.8",
  					"volume": "2138920",
  					"timestamp": 1783684800
  				}
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSDT",
  		exchange.Interval1h,
  		time.Unix(1783681200, 0),
  		time.Time{},
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
  	assert.Equal(t, 64374.0, klines[0].Open)
  	assert.Equal(t, 64450.0, klines[0].High)
  	assert.Equal(t, 64309.0, klines[0].Low)
  	assert.Equal(t, 64394.2, klines[0].Close)
  	assert.Equal(t, 542186.0, klines[0].Volume)

  	assert.Equal(t, int64(1783684800000), klines[1].Timestamp)
  	assert.Equal(t, 64399.9, klines[1].Open)
  	assert.Equal(t, 64580.0, klines[1].High)
  	assert.Equal(t, 64241.4, klines[1].Low)
  	assert.Equal(t, 64258.8, klines[1].Close)
  	assert.Equal(t, 2138920.0, klines[1].Volume)
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		w.WriteHeader(http.StatusBadRequest)
  		_, _ = w.Write([]byte(`{
  			"code": 40039,
  			"message": "Invalid Timestamp"
  		}`))
  	}))
  	defer server.Close()

  	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
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
  Run: `go test -v ./internal/infrastructure/exchange/bitmart/... -run TestClient_FetchKlines`
  Expected: FAIL (Bitmart client does not support FetchKlines)

- [ ] **Step 3: Write implementation**
  Replace `internal/infrastructure/exchange/bitmart/klines.go` with interval mapping and `/contract/public/kline` fetch/parse logic:
  ```go
  package bitmart

  import (
  	"context"
  	"fmt"
  	"net/http"
  	"strconv"
  	"time"

  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/pkg/xjson"
  )

  var intervalMap = map[exchange.Interval]int{
  	exchange.Interval1m:  1,
  	exchange.Interval3m:  3,
  	exchange.Interval5m:  5,
  	exchange.Interval15m: 15,
  	exchange.Interval30m: 30,
  	exchange.Interval1h:  60,
  	exchange.Interval2h:  120,
  	exchange.Interval4h:  240,
  	exchange.Interval6h:  240,
  	exchange.Interval8h:  240,
  	exchange.Interval12h: 240,
  	exchange.Interval1d:  1440,
  	exchange.Interval1w:  10080,
  	exchange.Interval1M:  43200,
  }

  func mapBitmartInterval(interval exchange.Interval) int {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return 1
  }

  type bitmartKlineItem struct {
  	LowPrice   xjson.Number `json:"low_price"`
  	HighPrice  xjson.Number `json:"high_price"`
  	OpenPrice  xjson.Number `json:"open_price"`
  	ClosePrice xjson.Number `json:"close_price"`
  	Volume     xjson.Number `json:"volume"`
  	Timestamp  int64        `json:"timestamp"`
  }

  type bitmartKlineResponse struct {
  	Code    int                `json:"code"`
  	Message string             `json:"message"`
  	Data    []bitmartKlineItem `json:"data"`
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]bitmartKlineItem, error) {
  	params := map[string]string{
  		"symbol": symbol,
  		"step":   strconv.Itoa(mapBitmartInterval(interval)),
  	}
  	if !start.IsZero() {
  		params["start_time"] = strconv.FormatInt(start.Unix(), 10)
  	}
  	if !end.IsZero() {
  		params["end_time"] = strconv.FormatInt(end.Unix(), 10)
  	}

  	body, err := c.RawRequest(ctx, http.MethodGet, "/contract/public/kline", params, nil)
  	if err != nil {
  		return nil, err
  	}

  	var resp bitmartKlineResponse
  	if err := xjson.Unmarshal(body, &resp); err != nil {
  		return nil, fmt.Errorf("unmarshal klines: %w", err)
  	}
  	if resp.Code != 1000 {
  		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
  	}
  	return resp.Data, nil
  }

  // FetchKlines fetches public K-lines for bitmart.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("bitmart fetch klines: %w", err)
  	}

  	klines := make([]exchange.Kline, 0, len(rawKlines))
  	for _, k := range rawKlines {
  		klines = append(klines, exchange.Kline{
  			Timestamp: k.Timestamp * 1000,
  			Open:      xjson.ToFloat64(k.OpenPrice),
  			High:      xjson.ToFloat64(k.HighPrice),
  			Low:       xjson.ToFloat64(k.LowPrice),
  			Close:     xjson.ToFloat64(k.ClosePrice),
  			Volume:    xjson.ToFloat64(k.Volume),
  		})
  	}

  	return klines, nil
  }
  ```

- [ ] **Step 4: Run unit tests to verify they pass**
  Run: `go test -v ./internal/infrastructure/exchange/bitmart/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/bitmart/klines.go internal/infrastructure/exchange/bitmart/klines_test.go && git commit -m "feat(bitmart): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: [client_factory_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/app/client_factory_test.go)

- [ ] **Step 1: Modify the factory test condition**
  Add `"bitmart"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport` in `internal/infrastructure/app/client_factory_test.go`.

- [ ] **Step 2: Run factory tests to verify BitMart passes**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 3: Run full quality gates**
  Run: `make lint && make test && make cover`
  Expected: PASS

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register bitmart in FetchKlines support tests"`

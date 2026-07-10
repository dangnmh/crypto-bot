# Zoomex Futures FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the Zoomex client and verify it using unit tests and factory integration tests.

---

### Task 1: Write Unit Tests and Implement FetchKlines on Zoomex REST Client

**Files:**
- Create: `internal/infrastructure/exchange/zoomex/klines_test.go`
- Modify: `internal/infrastructure/exchange/zoomex/klines.go`

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/zoomex/klines_test.go` containing:
  ```go
  package zoomex_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/zoomex"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/cloud/trade/v3/market/kline", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
  		assert.Equal(t, "linear", q.Get("category"))
  		assert.Equal(t, "60", q.Get("interval"))
  		assert.Equal(t, "1783674000000", q.Get("start"))
  		assert.Equal(t, "1783681200000", q.Get("end"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"retCode": 0,
  			"retMsg": "OK",
  			"result": {
  				"symbol": "BTCUSDT",
  				"category": "linear",
  				"list": [
  					[
  						"1783681200000",
  						"64373.4",
  						"64448.5",
  						"64328.4",
  						"64365",
  						"91.882",
  						"5916361.7961"
  					],
  					[
  						"1783677600000",
  						"64357.9",
  						"64470.6",
  						"64241.6",
  						"64373.4",
  						"193.689",
  						"12465918.782"
  					]
  				]
  			},
  			"retExtInfo": {},
  			"time": 1783683127965
  		}`))
  	}))
  	defer server.Close()

  	client := zoomex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSDT",
  		exchange.Interval1h,
  		time.UnixMilli(1783674000000),
  		time.UnixMilli(1783681200000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
  	assert.Equal(t, 64373.4, klines[0].Open)
  	assert.Equal(t, 64448.5, klines[0].High)
  	assert.Equal(t, 64328.4, klines[0].Low)
  	assert.Equal(t, 64365.0, klines[0].Close)
  	assert.Equal(t, 91.882, klines[0].Volume)

  	assert.Equal(t, int64(1783677600000), klines[1].Timestamp)
  	assert.Equal(t, 64357.9, klines[1].Open)
  	assert.Equal(t, 64470.6, klines[1].High)
  	assert.Equal(t, 64241.6, klines[1].Low)
  	assert.Equal(t, 64373.4, klines[1].Close)
  	assert.Equal(t, 193.689, klines[1].Volume)
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"retCode": 10001,
  			"retMsg": "invalid symbol"
  		}`))
  	}))
  	defer server.Close()

  	client := zoomex.NewClient(server.Client(), server.URL, config.LoggingConfig{})
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
  Run command: `go test -v ./internal/infrastructure/exchange/zoomex/... -run TestClient_FetchKlines`
  Expected: FAIL

- [ ] **Step 3: Write minimal implementation**
  Replace `internal/infrastructure/exchange/zoomex/klines.go` with:
  ```go
  package zoomex

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
  	exchange.Interval1m:  "1",
  	exchange.Interval3m:  "3",
  	exchange.Interval5m:  "5",
  	exchange.Interval15m: "15",
  	exchange.Interval30m: "30",
  	exchange.Interval1h:  "60",
  	exchange.Interval2h:  "120",
  	exchange.Interval4h:  "240",
  	exchange.Interval6h:  "360",
  	exchange.Interval8h:  "360",
  	exchange.Interval12h: "720",
  	exchange.Interval1d:  "D",
  	exchange.Interval1w:  "W",
  	exchange.Interval1M:  "M",
  }

  func mapZoomexInterval(interval exchange.Interval) string {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return "1"
  }

  type zoomexKlinesResult struct {
  	List [][]xjson.Number `json:"list"`
  }

  type zoomexKlinesResponse struct {
  	RetCode int64              `json:"retCode"`
  	RetMsg  string             `json:"retMsg"`
  	Result  zoomexKlinesResult `json:"result"`
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
  	params := map[string]string{
  		"symbol":   symbol,
  		"category": categoryLinear,
  		"interval": mapZoomexInterval(interval),
  	}
  	if !start.IsZero() {
  		params["start"] = strconv.FormatInt(start.UnixMilli(), 10)
  	}
  	if !end.IsZero() {
  		params["end"] = strconv.FormatInt(end.UnixMilli(), 10)
  	}

  	body, err := c.request(ctx, http.MethodGet, "/cloud/trade/v3/market/kline", params)
  	if err != nil {
  		return nil, err
  	}

  	var resp zoomexKlinesResponse
  	if err := xjson.Unmarshal(body, &resp); err != nil {
  		return nil, fmt.Errorf("unmarshal zoomex klines: %w", err)
  	}

  	if resp.RetCode != 0 {
  		return nil, fmt.Errorf("zoomex API error: %d - %s", resp.RetCode, resp.RetMsg)
  	}

  	return resp.Result.List, nil
  }

  // FetchKlines fetches public K-lines for zoomex.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("zoomex fetch klines: %w", err)
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
  Run command: `go test -v ./internal/infrastructure/exchange/zoomex/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/zoomex/klines.go && git add internal/infrastructure/exchange/zoomex/klines_test.go && git commit -m "feat(zoomex): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: `internal/infrastructure/app/client_factory_test.go`

- [ ] **Step 1: Modify the factory test condition**
  Add `"zoomex"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport`:
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
	}
  ```

- [ ] **Step 2: Run tests to verify all pass**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 3: Run full package quality checks**
  Run commands:
  `make lint`
  `make test`
  `make cover`

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register zoomex in FetchKlines support tests"`

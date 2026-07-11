# Kraken Futures FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the Kraken Futures client and verify it using unit tests and factory integration tests.

---

### Task 1: Write Unit Tests and Implement FetchKlines on Kraken Futures REST Client

**Files:**
- Create: `internal/infrastructure/exchange/krakenfutures/klines_test.go`
- Modify: `internal/infrastructure/exchange/krakenfutures/klines.go`

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/krakenfutures/klines_test.go` containing:
  ```go
  package krakenfutures_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/krakenfutures"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/api/charts/v1/trade/PF_XBTUSD/1m", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "1783681200000", q.Get("from"))
  		assert.Equal(t, "1783683000000", q.Get("to"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"candles": [
  				{
  					"time": 1783681200000,
  					"open": "64373.4",
  					"high": "64448.5",
  					"low": "64328.4",
  					"close": "64365",
  					"volume": "91.882"
  				},
  				{
  					"time": 1783681260000,
  					"open": "64365.0",
  					"high": "64370.0",
  					"low": "64350.0",
  					"close": "64360.0",
  					"volume": "10.5"
  				}
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSD",
  		exchange.Interval1m,
  		time.UnixMilli(1783681200000),
  		time.UnixMilli(1783683000000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783681200000), klines[0].Timestamp)
  	assert.Equal(t, 64373.4, klines[0].Open)
  	assert.Equal(t, 64448.5, klines[0].High)
  	assert.Equal(t, 64328.4, klines[0].Low)
  	assert.Equal(t, 64365.0, klines[0].Close)
  	assert.Equal(t, 91.882, klines[0].Volume)

  	assert.Equal(t, int64(1783681260000), klines[1].Timestamp)
  	assert.Equal(t, 64365.0, klines[1].Open)
  	assert.Equal(t, 64370.0, klines[1].High)
  	assert.Equal(t, 64350.0, klines[1].Low)
  	assert.Equal(t, 64360.0, klines[1].Close)
  	assert.Equal(t, 10.5, klines[1].Volume)
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		w.WriteHeader(http.StatusBadRequest)
  		_, _ = w.Write([]byte(`{"result":"error","error":"invalid resolution"}`))
  	}))
  	defer server.Close()

  	client := krakenfutures.NewClient(server.Client(), server.URL, config.LoggingConfig{})
  	_, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSD",
  		exchange.Interval1m,
  		time.Time{},
  		time.Time{},
  	)
  	assert.Error(t, err)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run command: `go test -v ./internal/infrastructure/exchange/krakenfutures/... -run TestClient_FetchKlines`
  Expected: FAIL

- [ ] **Step 3: Write minimal implementation**
  Replace `internal/infrastructure/exchange/krakenfutures/klines.go` with:
  ```go
  package krakenfutures

  import (
  	"context"
  	"encoding/json"
  	"fmt"
  	"net/http"
  	"sort"
  	"strings"
  	"time"

  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/pkg/xjson"
  )

  var krakenIntervals = map[exchange.Interval]string{
  	exchange.Interval1m:  "1m",
  	exchange.Interval3m:  "1m",
  	exchange.Interval5m:  "5m",
  	exchange.Interval15m: "15m",
  	exchange.Interval30m: "30m",
  	exchange.Interval1h:  "1h",
  	exchange.Interval2h:  "1h",
  	exchange.Interval4h:  "4h",
  	exchange.Interval6h:  "4h",
  	exchange.Interval8h:  "4h",
  	exchange.Interval12h: "12h",
  	exchange.Interval1d:  "1d",
  	exchange.Interval1w:  "1w",
  	exchange.Interval1M:  "1d",
  }

  func mapKrakenInterval(interval exchange.Interval) (string, error) {
  	if mapped, ok := krakenIntervals[interval]; ok {
  		return mapped, nil
  	}
  	return "", fmt.Errorf("unsupported interval: %s", interval)
  }

  func toKrakenSymbol(symbol string) string {
  	upper := strings.ToUpper(symbol)
  	upper = strings.ReplaceAll(upper, "BTC", "XBT")
  	if !strings.HasPrefix(upper, "PF_") && !strings.HasPrefix(upper, "PI_") {
  		upper = "PF_" + upper
  	}
  	return upper
  }

  type krakenCandlesResponse struct {
  	Candles []krakenCandle `json:"candles"`
  }

  type krakenCandle struct {
  	Time   int64        `json:"time"`
  	Open   xjson.Number `json:"open"`
  	High   xjson.Number `json:"high"`
  	Low    xjson.Number `json:"low"`
  	Close  xjson.Number `json:"close"`
  	Volume xjson.Number `json:"volume"`
  }

  // FetchKlines fetches public K-lines for krakenfutures.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	krakenSymbol := toKrakenSymbol(symbol)
  	mappedInterval, err := mapKrakenInterval(interval)
  	if err != nil {
  		return nil, fmt.Errorf("krakenfutures interval map: %w", err)
  	}

  	query := make(map[string]string)
  	if !start.IsZero() {
  		query["from"] = fmt.Sprintf("%d", start.UnixMilli())
  	}
  	if !end.IsZero() {
  		query["to"] = fmt.Sprintf("%d", end.UnixMilli())
  	}

  	path := fmt.Sprintf("/api/charts/v1/trade/%s/%s", krakenSymbol, mappedInterval)
  	body, err := c.request(ctx, http.MethodGet, path, query)
  	if err != nil {
  		return nil, fmt.Errorf("krakenfutures request: %w", err)
  	}

  	var resp krakenCandlesResponse
  	if err := json.Unmarshal(body, &resp); err != nil {
  		return nil, fmt.Errorf("krakenfutures unmarshal: %w", err)
  	}

  	klines := make([]exchange.Kline, 0, len(resp.Candles))
  	for _, candle := range resp.Candles {
  		klines = append(klines, exchange.Kline{
  			Timestamp: candle.Time,
  			Open:      xjson.ToFloat64(candle.Open),
  			High:      xjson.ToFloat64(candle.High),
  			Low:       xjson.ToFloat64(candle.Low),
  			Close:     xjson.ToFloat64(candle.Close),
  			Volume:    xjson.ToFloat64(candle.Volume),
  		})
  	}

  	sort.Slice(klines, func(i, j int) bool {
  		return klines[i].Timestamp < klines[j].Timestamp
  	})

  	return klines, nil
  }
  ```

- [ ] **Step 4: Run unit tests to verify they pass**
  Run command: `go test -v ./internal/infrastructure/exchange/krakenfutures/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/krakenfutures/klines.go && git add internal/infrastructure/exchange/krakenfutures/klines_test.go && git commit -m "feat(krakenfutures): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests and Update exchanges.md

**Files:**
- Modify: `internal/infrastructure/app/client_factory_test.go`
- Modify: `exchanges.md`

- [ ] **Step 1: Register krakenfutures in factory tests**
  Add `"krakenfutures": true` to the `supported` map inside `TestAllExchangesFetchKlinesSupport` in `internal/infrastructure/app/client_factory_test.go`.

- [ ] **Step 2: Update exchanges.md**
  Modify `- [ ] krakenfutures` to `- [x] krakenfutures`.

- [ ] **Step 3: Run factory tests and quality gates**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Run quality checks:
  `make lint`
  `make test`
  `make cover`

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go exchanges.md && git commit -m "test: register krakenfutures in FetchKlines support tests"`

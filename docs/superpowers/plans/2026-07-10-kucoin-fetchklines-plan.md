# KuCoin Futures FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the KuCoin Futures REST client and verify it using unit tests and factory integration tests.

**Architecture:** We will implement the `FetchKlines` method in `internal/infrastructure/exchange/kucoin/klines.go`. Following the "Raw Request Wrapper Rule", we will define a private `rawGetKlines` helper that calls `GET /api/v1/kline/query` and parses the envelope response. The public `FetchKlines` method will map standard timeframes, call the raw helper, and convert the heterogeneous list values safely to the standard `exchange.Kline` struct.

**Tech Stack:** Go (1.21+), Testify (`assert`, `require`), `httptest.NewServer` for mocking.

## Global Constraints
- Target codebase-wide coverage threshold is >= 75.0%.
- We must strictly follow Clean Architecture and the 6-file REST client layout conventions.
- Standard Go unmarshaling/marshaling uses `crypto-bot/pkg/xjson`.
- Direct REST requests must be wrapped with a private `raw` function prefix.

---

### Task 1: Write Unit Tests and Implement FetchKlines on KuCoin REST Client

**Files:**
- Create: `internal/infrastructure/exchange/kucoin/klines_test.go`
- Modify: `internal/infrastructure/exchange/kucoin/klines.go`

**Interfaces:**
- Produces: `func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error)`

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/kucoin/klines_test.go` containing:
  ```go
  package kucoin_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/kucoin"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/api/v1/kline/query", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "XBTUSDM", q.Get("symbol"))
  		assert.Equal(t, "60", q.Get("granularity"))
  		assert.Equal(t, "1783447200000", q.Get("from"))
  		assert.Equal(t, "1783458000000", q.Get("to"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "200000",
  			"msg": "success",
  			"data": [
  				[
  					1783447200000,
  					64085.7,
  					64089.0,
  					63618.5,
  					63618.5,
  					7282,
  					7282.0
  				],
  				[
  					1783450800000,
  					63600.0,
  					63790.4,
  					63302.2,
  					63756.5,
  					87269,
  					87269.0
  				]
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"XBTUSDM",
  		exchange.Interval1h,
  		time.UnixMilli(1783447200000),
  		time.UnixMilli(1783458000000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 2)

  	assert.Equal(t, int64(1783447200000), klines[0].Timestamp)
  	assert.Equal(t, 64085.7, klines[0].Open)
  	assert.Equal(t, 64089.0, klines[0].High)
  	assert.Equal(t, 63618.5, klines[0].Low)
  	assert.Equal(t, 63618.5, klines[0].Close)
  	assert.Equal(t, 7282.0, klines[0].Volume)

  	assert.Equal(t, int64(1783450800000), klines[1].Timestamp)
  	assert.Equal(t, 63600.0, klines[1].Open)
  	assert.Equal(t, 63790.4, klines[1].High)
  	assert.Equal(t, 63302.2, klines[1].Low)
  	assert.Equal(t, 63756.5, klines[1].Close)
  	assert.Equal(t, 87269.0, klines[1].Volume)
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

  	client := kucoin.NewClient(server.Client(), server.URL, "key", "secret", "pass", config.LoggingConfig{})
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
  Run command: `go test -v ./internal/infrastructure/exchange/kucoin/... -run TestClient_FetchKlines`
  Expected: FAIL (FetchKlines stub returns "kucoin does not support FetchKlines")

- [ ] **Step 3: Write minimal implementation**
  Replace `internal/infrastructure/exchange/kucoin/klines.go` with:
  ```go
  package kucoin

  import (
  	"context"
  	"fmt"
  	"net/http"
  	"strconv"
  	"time"

  	"crypto-bot/internal/infrastructure/exchange"
  )

  func parseJSONFloat(val any) float64 {
  	if val == nil {
  		return 0
  	}
  	switch v := val.(type) {
  	case float64:
  		return v
  	case string:
  		f, _ := strconv.ParseFloat(v, 64)
  		return f
  	case int64:
  		return float64(v)
  	case int:
  		return float64(v)
  	default:
  		return 0
  	}
  }

  func parseJSONInt(val any) int64 {
  	if val == nil {
  		return 0
  	}
  	switch v := val.(type) {
  	case float64:
  		return int64(v)
  	case string:
  		i, _ := strconv.ParseInt(v, 10, 64)
  		return i
  	case int64:
  		return v
  	case int:
  		return int64(v)
  	default:
  		return 0
  	}
  }

  func mapKucoinInterval(interval exchange.Interval) string {
  	switch interval {
  	case exchange.Interval1m:
  		return "1"
  	case exchange.Interval3m:
  		return "3"
  	case exchange.Interval5m:
  		return "5"
  	case exchange.Interval15m:
  		return "15"
  	case exchange.Interval30m:
  		return "30"
  	case exchange.Interval1h:
  		return "60"
  	case exchange.Interval2h:
  		return "120"
  	case exchange.Interval4h:
  		return "240"
  	case exchange.Interval6h:
  		return "360"
  	case exchange.Interval8h:
  		return "480"
  	case exchange.Interval12h:
  		return "720"
  	case exchange.Interval1d:
  		return "1440"
  	case exchange.Interval1w:
  		return "10080"
  	case exchange.Interval1M:
  		return "43200"
  	default:
  		return "1"
  	}
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]any, error) {
  	params := map[string]string{
  		paramSymbol:  symbol,
  		"granularity": mapKucoinInterval(interval),
  	}
  	if !start.IsZero() {
  		params["from"] = strconv.FormatInt(start.UnixMilli(), 10)
  	}
  	if !end.IsZero() {
  		params["to"] = strconv.FormatInt(end.UnixMilli(), 10)
  	}

  	body, err := c.RawRequest(ctx, http.MethodGet, pathKlines, params, nil)
  	if err != nil {
  		return nil, err
  	}

  	return ParseResponse[[][]any](body, "klines")
  }

  // FetchKlines fetches public K-lines for kucoin.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("kucoin fetch klines: %w", err)
  	}

  	klines := make([]exchange.Kline, 0, len(rawKlines))
  	for _, k := range rawKlines {
  		if len(k) < 6 {
  			continue
  		}
  		klines = append(klines, exchange.Kline{
  			Timestamp: parseJSONInt(k[0]),
  			Open:      parseJSONFloat(k[1]),
  			High:      parseJSONFloat(k[2]),
  			Low:       parseJSONFloat(k[3]),
  			Close:     parseJSONFloat(k[4]),
  			Volume:    parseJSONFloat(k[5]),
  		})
  	}

  	return klines, nil
  }
  ```

- [ ] **Step 4: Run unit tests to verify they pass**
  Run command: `go test -v ./internal/infrastructure/exchange/kucoin/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/kucoin/klines.go && git add internal/infrastructure/exchange/kucoin/klines_test.go && git commit -m "feat(kucoin): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: `internal/infrastructure/app/client_factory_test.go:47`

- [ ] **Step 1: Modify the factory test condition**
  Add `"kucoin"` to the list of supported exchanges inside `TestAllExchangesFetchKlinesSupport`:
  ```go
  if name == "binance" || name == "bybit" || name == "gate" || name == "mexc" || name == "okx" || name == "hyperliquid" || name == "apex" || name == "aster" || name == "backpack" || name == "batonex" || name == "bingx" || name == "kucoin" {
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
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register kucoin in FetchKlines support tests"`

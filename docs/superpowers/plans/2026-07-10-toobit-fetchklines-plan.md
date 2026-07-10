# Toobit FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the Toobit REST client and verify it using unit tests and factory integration tests.

**Architecture:** We will implement the `FetchKlines` method on Toobit's REST `Client` under `internal/infrastructure/exchange/toobit` satisfying the `KlineProvider` interface. This method will fetch candles from Toobit's public market GET `/quote/v1/klines` endpoint, parse the raw JSON array of arrays, and map it to `exchange.Kline` structs.

**Tech Stack:** Go (Golang), standard library `net/http`, project testing utilities (`httptest`, `testify`).

## Global Constraints
- Clean Architecture / DDD conventions in `docs/tech/architecture.md`.
- Quality Gates must pass: `make lint`, `make test`, `make cover`.

---

### Task 1: Write Unit Tests and Implement FetchKlines on Toobit REST Client

**Files:**
- Create: [klines_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/klines_test.go)
- Modify: [klines.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/klines.go)

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/toobit/klines_test.go` with mock httptest server matching Toobit REST `/quote/v1/klines` response format:
  ```go
  package toobit_test

  import (
  	"context"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/toobit"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/quote/v1/klines", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
  		assert.Equal(t, "1h", q.Get("interval"))
  		assert.Equal(t, "1783681200000", q.Get("startTime"))
  		assert.Equal(t, "1000", q.Get("limit"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`[
  			[
  				1783681200000,
  				"64375.7",
  				"64456",
  				"64302.4",
  				"64436.1",
  				"448016",
  				1783684799999,
  				"28841508.980000008",
  				308,
  				"1756.87402397",
  				"28.46694368",
  				"0"
  			],
  			[
  				1783677600000,
  				"64362.2",
  				"64478.7",
  				"64246.5",
  				"64375.7",
  				"936968",
  				1783681199999,
  				"60303388.4182006",
  				512,
  				"3256.12345",
  				"42.12345",
  				"0"
  			]
  		]`))
  	}))
  	defer server.Close()

  	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSDT",
  		exchange.Interval1h,
  		time.UnixMilli(1783681200000),
  		time.Time{},
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
  		w.WriteHeader(http.StatusBadRequest)
  		_, _ = w.Write([]byte(`{
  			"code": -1121,
  			"msg": "Invalid symbol"
  		}`))
  	}))
  	defer server.Close()

  	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
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
  Run: `go test -v ./internal/infrastructure/exchange/toobit/... -run TestClient_FetchKlines`
  Expected: FAIL (Toobit client does not support FetchKlines)

- [ ] **Step 3: Write implementation**
  Replace `internal/infrastructure/exchange/toobit/klines.go` with interval mapping and `/quote/v1/klines` fetch/parse logic:
  ```go
  package toobit

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
  	exchange.Interval1h:  "1h",
  	exchange.Interval2h:  "2h",
  	exchange.Interval4h:  "4h",
  	exchange.Interval6h:  "6h",
  	exchange.Interval8h:  "8h",
  	exchange.Interval12h: "12h",
  	exchange.Interval1d:  "1d",
  	exchange.Interval1w:  "1w",
  	exchange.Interval1M:  "1M",
  }

  func mapToobitInterval(interval exchange.Interval) string {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return "1m"
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
  	params := map[string]string{
  		"symbol":   symbol,
  		"interval": mapToobitInterval(interval),
  	}
  	if !start.IsZero() {
  		params["startTime"] = strconv.FormatInt(start.UnixMilli(), 10)
  	}
  	if !end.IsZero() {
  		params["endTime"] = strconv.FormatInt(end.UnixMilli(), 10)
  	}
  	params["limit"] = "1000"

  	body, err := c.RawRequest(ctx, http.MethodGet, "/quote/v1/klines", params, nil)
  	if err != nil {
  		return nil, err
  	}

  	var rawKlines [][]xjson.Number
  	if err := xjson.Unmarshal(body, &rawKlines); err != nil {
  		return nil, fmt.Errorf("unmarshal klines: %w", err)
  	}
  	return rawKlines, nil
  }

  // FetchKlines fetches public K-lines for toobit.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("toobit fetch klines: %w", err)
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
  Run: `go test -v ./internal/infrastructure/exchange/toobit/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/toobit/klines.go internal/infrastructure/exchange/toobit/klines_test.go && git commit -m "feat(toobit): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: [client_factory_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/app/client_factory_test.go)

- [ ] **Step 1: Modify the factory test condition**
  Add `"toobit"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport` in `internal/infrastructure/app/client_factory_test.go`.

- [ ] **Step 2: Run factory tests to verify Toobit passes**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 3: Run full quality gates**
  Run: `make lint && make test && make cover`
  Expected: PASS

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register toobit in FetchKlines support tests"`

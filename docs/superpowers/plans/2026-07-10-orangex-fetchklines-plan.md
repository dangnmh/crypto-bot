# OrangeX FetchKlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `FetchKlines` method on the OrangeX REST client and verify it using unit tests and factory integration tests.

**Architecture:** We will implement the `FetchKlines` method on OrangeX's REST `Client` under `internal/infrastructure/exchange/orangex` satisfying the `KlineProvider` interface. This method will fetch candles from OrangeX's private JSON-RPC POST `/private/get_tradingview_chart_data` endpoint (which is a Deribit clone endpoint), parse the column-oriented arrays (`ticks`, `open`, `high`, `low`, `close`, `volume`), and map them to `exchange.Kline` structs.

**Tech Stack:** Go (Golang), standard library `net/http`, project testing utilities (`httptest`, `testify`).

## Global Constraints
- Clean Architecture / DDD conventions in `docs/tech/architecture.md`.
- Quality Gates must pass: `make lint`, `make test`, `make cover`.

---

### Task 1: Write Unit Tests and Implement FetchKlines on OrangeX REST Client

**Files:**
- Create: [klines_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/orangex/klines_test.go)
- Modify: [klines.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/orangex/klines.go)

- [ ] **Step 1: Write the failing unit tests**
  Create `internal/infrastructure/exchange/orangex/klines_test.go` with mock httptest server matching OrangeX / Deribit JSON-RPC `/private/get_tradingview_chart_data` response format:
  ```go
  package orangex_test

  import (
  	"context"
  	"encoding/json"
  	"net/http"
  	"net/http/httptest"
  	"testing"
  	"time"

  	"crypto-bot/internal/infrastructure/config"
  	"crypto-bot/internal/infrastructure/exchange"
  	"crypto-bot/internal/infrastructure/exchange/orangex"

  	"github.com/stretchr/testify/assert"
  	"github.com/stretchr/testify/require"
  )

  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "POST", r.Method)
  		assert.Equal(t, "/api/v1/private/get_tradingview_chart_data", r.URL.Path)

  		var reqBody struct {
  			Method string `json:"method"`
  			Params struct {
  				InstrumentName string `json:"instrument_name"`
  				Resolution     string `json:"resolution"`
  				StartTimestamp int64  `json:"start_timestamp"`
  				EndTimestamp   int64  `json:"end_timestamp"`
  			} `json:"params"`
  		}
  		err := json.NewDecoder(r.Body).Decode(&reqBody)
  		require.NoError(t, err)

  		assert.Equal(t, "/private/get_tradingview_chart_data", reqBody.Method)
  		assert.Equal(t, "BTC-USDT-PERPETUAL", reqBody.Params.InstrumentName)
  		assert.Equal(t, "60", reqBody.Params.Resolution)
  		assert.Equal(t, int64(1783681200000), reqBody.Params.StartTimestamp)

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"jsonrpc": "2.0",
  			"id": 1,
  			"result": {
  				"status": "ok",
  				"ticks": [1783681200000, 1783684800000],
  				"open": ["64374", "64399.9"],
  				"high": ["64450", "64580"],
  				"low": ["64309", "64241.4"],
  				"close": ["64394.2", "64258.8"],
  				"volume": ["542186", "2138920"]
  			}
  		}`))
  	}))
  	defer server.Close()

  	client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
  	// Mock access token to bypass token refresher auth call
  	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		if r.URL.Path == "/api/v1/public/auth" {
  			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
  			return
  		}
  		if r.URL.Path == "/api/v1/private/get_tradingview_chart_data" {
  			_, _ = w.Write([]byte(`{
  				"jsonrpc": "2.0",
  				"id": 1,
  				"result": {
  					"status": "ok",
  					"ticks": [1783681200000, 1783684800000],
  					"open": ["64374", "64399.9"],
  					"high": ["64450", "64580"],
  					"low": ["64309", "64241.4"],
  					"close": ["64394.2", "64258.8"],
  					"volume": ["542186", "2138920"]
  				}
  			}`))
  			return
  		}
  	})

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC-USDT-PERPETUAL",
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
  }

  func TestClient_FetchKlines_Error(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		w.Header().Set("Content-Type", "application/json")
  		if r.URL.Path == "/api/v1/public/auth" {
  			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
  			return
  		}
  		w.WriteHeader(http.StatusBadRequest)
  		_, _ = w.Write([]byte(`{
  			"jsonrpc": "2.0",
  			"error": {
  				"code": 8121,
  				"message": "No service found"
  			}
  		}`))
  	}))
  	defer server.Close()

  	client := orangex.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
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
  Run: `go test -v ./internal/infrastructure/exchange/orangex/... -run TestClient_FetchKlines`
  Expected: FAIL (Orangex client does not support FetchKlines)

- [ ] **Step 3: Write implementation**
  Replace `internal/infrastructure/exchange/orangex/klines.go` with interval mapping and `/private/get_tradingview_chart_data` fetch/parse logic:
  ```go
  package orangex

  import (
  	"context"
  	"fmt"
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
  	exchange.Interval1d:  "1D",
  	exchange.Interval1w:  "1W",
  	exchange.Interval1M:  "1M",
  }

  func mapOrangexInterval(interval exchange.Interval) string {
  	if val, ok := intervalMap[interval]; ok {
  		return val
  	}
  	return "1"
  }

  type orangexKlineResult struct {
  	Volume []xjson.Number `json:"volume"`
  	Ticks  []int64        `json:"ticks"`
  	Status string         `json:"status"`
  	Open   []xjson.Number `json:"open"`
  	Low    []xjson.Number `json:"low"`
  	High   []xjson.Number `json:"high"`
  	Close  []xjson.Number `json:"close"`
  }

  func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) (*orangexKlineResult, error) {
  	params := map[string]any{
  		"instrument_name": symbol,
  		"resolution":      mapOrangexInterval(interval),
  	}
  	if !start.IsZero() {
  		params["start_timestamp"] = start.UnixMilli()
  	} else {
  		params["start_timestamp"] = c.clock.Now().Add(-24 * time.Hour).UnixMilli()
  	}
  	if !end.IsZero() {
  		params["end_timestamp"] = end.UnixMilli()
  	} else {
  		params["end_timestamp"] = c.clock.Now().UnixMilli()
  	}

  	body, err := c.postRPC(ctx, "/private/get_tradingview_chart_data", "/private/get_tradingview_chart_data", params, true)
  	if err != nil {
  		return nil, err
  	}

  	var envelope orangexRPCResponse[orangexKlineResult]
  	if err := xjson.Unmarshal(body, &envelope); err != nil {
  		return nil, fmt.Errorf("unmarshal klines: %w", err)
  	}
  	if envelope.Error != nil {
  		return nil, envelope.Error
  	}
  	return &envelope.Result, nil
  }

  // FetchKlines fetches public K-lines for orangex.
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	res, err := c.rawGetKlines(ctx, symbol, interval, start, end)
  	if err != nil {
  		return nil, fmt.Errorf("orangex fetch klines: %w", err)
  	}

  	n := len(res.Ticks)
  	if n == 0 {
  		return nil, nil
  	}

  	if len(res.Open) < n || len(res.High) < n || len(res.Low) < n || len(res.Close) < n || len(res.Volume) < n {
  		return nil, fmt.Errorf("invalid kline response: array length mismatch")
  	}

  	klines := make([]exchange.Kline, 0, n)
  	for i := 0; i < n; i++ {
  		klines = append(klines, exchange.Kline{
  			Timestamp: res.Ticks[i],
  			Open:      xjson.ToFloat64(res.Open[i]),
  			High:      xjson.ToFloat64(res.High[i]),
  			Low:       xjson.ToFloat64(res.Low[i]),
  			Close:     xjson.ToFloat64(res.Close[i]),
  			Volume:    xjson.ToFloat64(res.Volume[i]),
  		})
  	}

  	return klines, nil
  }
  ```

- [ ] **Step 4: Run unit tests to verify they pass**
  Run: `go test -v ./internal/infrastructure/exchange/orangex/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/orangex/klines.go internal/infrastructure/exchange/orangex/klines_test.go && git commit -m "feat(orangex): implement FetchKlines with unit tests"`

---

### Task 2: Register in Factory Integration Tests

**Files:**
- Modify: [client_factory_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/app/client_factory_test.go)

- [ ] **Step 1: Modify the factory test condition**
  Add `"orangex"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport` in `internal/infrastructure/app/client_factory_test.go`.

- [ ] **Step 2: Run factory tests to verify OrangeX passes**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 3: Run full quality gates**
  Run: `make lint && make test && make cover`
  Expected: PASS

- [ ] **Step 4: Commit**
  Run: `git add internal/infrastructure/app/client_factory_test.go && git commit -m "test: register orangex in FetchKlines support tests"`

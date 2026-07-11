# FetchKlines EndTime and Limit Fixes Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modify FetchKlines implementations in `binance`, `gate`, and `xt` to support end times and suitable limits, and verify with tests.

---

### Task 1: Fix XT FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/xt/klines.go`
- Modify: `internal/infrastructure/exchange/xt/klines_test.go`

- [ ] **Step 1: Update unit tests in `xt`**
  Modify `internal/infrastructure/exchange/xt/klines_test.go` to assert `"limit"` query param is `"100"`.
  Add `TestClient_FetchKlines_WithEnd` verifying the `"endTime"` query parameter mapping.
  ```go
  func TestClient_FetchKlines_WithEnd(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/future/market/v1/public/q/kline", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "btc_usdt", q.Get("symbol"))
  		assert.Equal(t, "1m", q.Get("interval"))
  		assert.Equal(t, "1783681200000", q.Get("endTime"))
  		assert.Equal(t, "100", q.Get("limit"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"returnCode": 0,
  			"msgInfo": "success",
  			"result": [
  				{
  					"t": 1783681200000,
  					"o": "64374",
  					"c": "64394.2",
  					"h": "64450",
  					"l": "64309",
  					"a": "31.4166",
  					"v": "2022077.26553"
  				}
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := xt.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC_USDT",
  		exchange.Interval1m,
  		time.Time{},
  		time.Unix(1783681200, 0),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 1)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/xt/...`
  Expected: FAIL

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/xt/klines.go` to set `"endTime"` if `!end.IsZero()` and pass `"limit": "100"`.
  ```go
  	params := map[string]string{
  		"symbol":   cleanSymbol,
  		"interval": mappedInterval,
  		"limit":    "100",
  	}

  	if !start.IsZero() {
  		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
  	}
  	if !end.IsZero() {
  		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
  	}
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/xt/...`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/xt/klines.go internal/infrastructure/exchange/xt/klines_test.go && git commit -m "feat(xt): map end time and limit in FetchKlines"`

---

### Task 2: Fix Gate FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/gate/market.go`
- Modify: `internal/infrastructure/exchange/gate/client_test.go`

- [ ] **Step 1: Write unit tests in `gate/client_test.go`**
  Add `TestClient_FetchKlines` to `internal/infrastructure/exchange/gate/client_test.go`:
  ```go
  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/futures/usdt/candlesticks", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTC_USDT", q.Get("contract"))
  		assert.Equal(t, "1m", q.Get("interval"))
  		assert.Equal(t, "1783665240", q.Get("from"))
  		assert.Equal(t, "1783681200", q.Get("to"))
  		assert.Equal(t, "100", q.Get("limit"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`[
  			[1783665240, "668", "63888.7", "63889.5", "63889.5", "63875.1"]
  		]`))
  	}))
  	defer server.Close()

  	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC_USDT",
  		exchange.Interval1m,
  		time.Unix(1783665240, 0),
  		time.Unix(1783681200, 0),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 1)

  	assert.Equal(t, int64(1783665240000), klines[0].Timestamp)
  	assert.Equal(t, 63888.7, klines[0].Close)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/gate/... -run TestClient_FetchKlines`
  Expected: FAIL

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/gate/market.go` to only set `"from"` if `!start.IsZero()`, set `"to"` if `!end.IsZero()`, and change limit to `"100"`.
  ```go
  	q := url.Values{}
  	q.Set("contract", symbol)
  	q.Set("interval", string(interval))
  	q.Set("limit", "100")

  	if !start.IsZero() {
  		q.Set("from", strconv.FormatInt(start.Unix(), 10))
  	}
  	if !end.IsZero() {
  		q.Set("to", strconv.FormatInt(end.Unix(), 10))
  	}
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/gate/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/gate/market.go internal/infrastructure/exchange/gate/client_test.go && git commit -m "feat(gate): map end time and limit in FetchKlines"`

---

### Task 3: Fix Binance FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/binance/market.go`
- Modify: `internal/infrastructure/exchange/binance/client_test.go`

- [ ] **Step 1: Write unit tests in `binance/client_test.go`**
  Add `TestClient_FetchKlines` to `internal/infrastructure/exchange/binance/client_test.go`:
  ```go
  func TestClient_FetchKlines(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/fapi/v1/klines", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTCUSDT", q.Get("symbol"))
  		assert.Equal(t, "1m", q.Get("interval"))
  		assert.Equal(t, "1783665240000", q.Get("startTime"))
  		assert.Equal(t, "1783681200000", q.Get("endTime"))
  		assert.Equal(t, "100", q.Get("limit"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`[
  			[
  				1783665240000,
  				"63889.5",
  				"63889.5",
  				"63875.1",
  				"63888.7",
  				"668",
  				1783665300000,
  				"42735.07",
  				100,
  				"500",
  				"1000",
  				"0"
  			]
  		]`))
  	}))
  	defer server.Close()

  	client := binance.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTCUSDT",
  		exchange.Interval1m,
  		time.Unix(1783665240, 0),
  		time.Unix(1783681200, 0),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 1)

  	assert.Equal(t, int64(1783665240000), klines[0].Timestamp)
  	assert.Equal(t, 63888.7, klines[0].Close)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/binance/... -run TestClient_FetchKlines`
  Expected: FAIL

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/binance/market.go` to set `"endTime"` if `!end.IsZero()`, only set `"startTime"` if `!start.IsZero()`, and set `"limit"` to `100`.
  ```go
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  	params := map[string]any{
  		"symbol":   symbol,
  		"interval": string(interval),
  		"limit":    100,
  	}
  	if !start.IsZero() {
  		params["startTime"] = start.UnixMilli()
  	}
  	if !end.IsZero() {
  		params["endTime"] = end.UnixMilli()
  	}
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/binance/... -run TestClient_FetchKlines`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/binance/market.go internal/infrastructure/exchange/binance/client_test.go && git commit -m "feat(binance): map end time and limit in FetchKlines"`

---

### Task 4: Run Factory Tests and Verification

- [ ] **Step 1: Run client factory integration tests**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 2: Run quality checks**
  Run:
  `make lint`
  `make test`
  `make cover`
  Expected: PASS

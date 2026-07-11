# FetchKlines Parameter and Limit Fixes Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modify FetchKlines implementations for BloFin, Hotcoin, and Pionex adapters to support timestamps and limits, and verify all tests and quality checks.

---

### Task 1: Fix BloFin FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/blofin/klines.go`
- Modify: `internal/infrastructure/exchange/blofin/klines_test.go`

- [ ] **Step 1: Write/Update the unit tests in blofin**
  Modify `internal/infrastructure/exchange/blofin/klines_test.go` to assert the limit parameter, and add a test case verifying the `start` parameter maps to `before` query filter.
  Specifically, change the mock handler:
  ```go
		assert.Equal(t, "100", q.Get("limit"))
  ```
  And add `TestClient_FetchKlines_WithStart` test case:
  ```go
  func TestClient_FetchKlines_WithStart(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/api/v1/market/candles", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "BTC-USDT", q.Get("instId"))
  		assert.Equal(t, "1m", q.Get("bar"))
  		assert.Equal(t, "1783665240000", q.Get("before"))
  		assert.Equal(t, "1783681200000", q.Get("after"))
  		assert.Equal(t, "100", q.Get("limit"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": "0",
  			"msg": "success",
  			"data": [
  				[
  					"1783665240000",
  					"63889.5",
  					"63889.5",
  					"63875.1",
  					"63888.7",
  					"668",
  					"0.6689",
  					"42735.07"
  				]
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := blofin.NewClient(server.Client(), server.URL, slog.Default())

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC_USDT",
  		exchange.Interval1m,
  		time.Unix(1783665240, 0),
  		time.Unix(1783681200, 0),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 1)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/blofin/...`
  Expected: FAIL (missing limit assertion, missing before mapping, build error on test compilation)

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/blofin/klines.go` to rename `_` parameter to `start`, map `start` to `"before"`, and pass `"limit": "100"`:
  ```go
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  ...
  	params := map[string]string{
  		"instId": cleanSymbol,
  		"bar":    mappedInterval,
  		"limit":  "100",
  	}

  	if !start.IsZero() {
  		params["before"] = fmt.Sprintf("%d", start.UnixMilli())
  	}

  	if !end.IsZero() {
  		params["after"] = fmt.Sprintf("%d", end.UnixMilli())
  	}
  ...
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/blofin/...`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/blofin/klines.go internal/infrastructure/exchange/blofin/klines_test.go && git commit -m "feat(blofin): map start time and limit in FetchKlines"`

---

### Task 2: Fix Hotcoin FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/hotcoin/klines.go`
- Modify: `internal/infrastructure/exchange/hotcoin/klines_test.go`

- [ ] **Step 1: Write/Update the unit tests in hotcoin**
  Modify `internal/infrastructure/exchange/hotcoin/klines_test.go` to assert `"size"` is `"100"`.
  Add `TestClient_FetchKlines_WithEnd` test case:
  ```go
  func TestClient_FetchKlines_WithEnd(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "GET", r.Method)
  		assert.Equal(t, "/api/v1/perpetual/public/BTCUSDT/candles", r.URL.Path)

  		q := r.URL.Query()
  		assert.Equal(t, "1min", q.Get("kline"))
  		assert.Equal(t, "1783687800000", q.Get("since"))
  		assert.Equal(t, "100", q.Get("size"))

  		w.Header().Set("Content-Type", "application/json")
  		_, _ = w.Write([]byte(`{
  			"code": 200,
  			"msg": "success",
  			"data": [
  				[
  					1783687800000,
  					"64293.7",
  					"64325.8",
  					"64305.2",
  					"64323.1",
  					"9027",
  					"580895.15"
  				]
  			]
  		}`))
  	}))
  	defer server.Close()

  	client := hotcoin.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

  	klines, err := client.FetchKlines(
  		context.Background(),
  		"BTC_USDT",
  		exchange.Interval1m,
  		time.Time{},
  		time.UnixMilli(1783687800000),
  	)
  	require.NoError(t, err)
  	require.Len(t, klines, 1)
  }
  ```

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/hotcoin/...`
  Expected: FAIL

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/hotcoin/klines.go` to import `"sort"`, rename `_, _` parameters to `start, end`, map `end` to `"since"`, add `"size": "100"`, and sort results:
  ```go
  func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
  ...
  	params := map[string]string{
  		"kline": mappedInterval,
  		"size":  "100",
  	}
  	if !end.IsZero() {
  		params["since"] = fmt.Sprintf("%d", end.UnixMilli())
  	}

  	path := fmt.Sprintf("/api/v1/perpetual/public/%s/candles", contractCode)
  ...
  	// loop appending klines
  ...
  	sort.Slice(klines, func(i, j int) bool {
  		return klines[i].Timestamp < klines[j].Timestamp
  	})

  	return klines, nil
  }
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/hotcoin/...`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/hotcoin/klines.go internal/infrastructure/exchange/hotcoin/klines_test.go && git commit -m "feat(hotcoin): map end time and size in FetchKlines"`

---

### Task 3: Fix Pionex FetchKlines Implementation

**Files:**
- Modify: `internal/infrastructure/exchange/pionex/klines.go`
- Modify: `internal/infrastructure/exchange/pionex/klines_test.go`

- [ ] **Step 1: Write/Update the unit tests in pionex**
  Modify `internal/infrastructure/exchange/pionex/klines_test.go` to assert `"limit"` is `"100"`.

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v ./internal/infrastructure/exchange/pionex/...`
  Expected: FAIL

- [ ] **Step 3: Update implementation**
  Modify `internal/infrastructure/exchange/pionex/klines.go` to set `"limit": "100"`:
  ```go
  	params := map[string]string{
  		"symbol":   cleanSymbol,
  		"interval": mappedInterval,
  		"limit":    "100",
  	}
  ```

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/pionex/...`
  Expected: PASS

- [ ] **Step 5: Commit**
  Run: `git add internal/infrastructure/exchange/pionex/klines.go internal/infrastructure/exchange/pionex/klines_test.go && git commit -m "feat(pionex): add limit parameter in FetchKlines"`

---

### Task 4: Run Factory Tests and Verification

**Files:**
- None

- [ ] **Step 1: Run client factory integration tests**
  Run: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
  Expected: PASS

- [ ] **Step 2: Run quality checks**
  Run:
  `make lint`
  `make test`
  `make cover`
  Expected: PASS

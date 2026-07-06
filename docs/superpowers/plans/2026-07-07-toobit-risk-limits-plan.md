# Toobit Risk Limits and Leverage Adjustment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retrieve Toobit's risk limits config and use it to dynamically cap/adjust the target leverage before changing leverage on the client.

**Architecture:** Define an optional `MaxLeverageProvider` interface in `interfaces.go`. Implement it in the Toobit exchange client by calling the public `GET /api/v1/futures/riskLimits` endpoint. In the funding bot reversion execution (`fire_ioc.go`), type-assert the client to this interface and adjust/cap the target leverage before invoking `ChangeLeverage`.

**Tech Stack:** Go, standard library net/http, testify for tests.

## Global Constraints
- Pass `make lint` before declaring task complete.
- Pass `make test` before declaring task complete.
- Verify test coverage via `make cover` or `make cover-check`.

---

### Task 1: Add MaxLeverageProvider Interface

**Files:**
- Modify: `internal/infrastructure/exchange/interfaces.go`

**Interfaces:**
- Produces: `MaxLeverageProvider` interface

- [ ] **Step 1: Write the interface definition in `internal/infrastructure/exchange/interfaces.go`**
  Add the following code block to [interfaces.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/interfaces.go):
  ```go
  // MaxLeverageProvider is an optional interface that exchange REST clients can implement
  // to support retrieving the maximum leverage allowed for a symbol.
  type MaxLeverageProvider interface {
  	GetMaxLeverage(ctx context.Context, symbol string) (int, error)
  }
  ```

- [ ] **Step 2: Run `make test` to verify it compiles**
  Run: `make test`
  Expected: Success, compiles and runs existing tests.

- [ ] **Step 3: Commit interface addition**
  Run:
  ```bash
  git add internal/infrastructure/exchange/interfaces.go
  git commit -m "feat: define MaxLeverageProvider interface"
  ```

---

### Task 2: Implement GetMaxLeverage in Toobit Client

**Files:**
- Modify: `internal/infrastructure/exchange/toobit/trade.go`

**Interfaces:**
- Consumes: `MaxLeverageProvider`
- Produces: `toobitRiskLimitConfig` struct, `rawGetRiskLimits`, and `GetMaxLeverage` methods

- [ ] **Step 1: Implement the response struct, raw endpoint call, and public method in Toobit client**
  Add the following implementation to [trade.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/trade.go):
  ```go
  type toobitRiskLimitConfig struct {
  	Level          int          `json:"level"`
  	Quantity       string       `json:"quantity"`
  	MaintainMargin string       `json:"maintainMargin"`
  	InitialMargin  string       `json:"initialMargin"`
  	MaxLeverage    xjson.Number `json:"maxLeverage"`
  }

  func (c *Client) rawGetRiskLimits(ctx context.Context, symbol string) ([]byte, error) {
  	params := map[string]string{
  		symbolKey: symbol,
  	}
  	return c.request(ctx, http.MethodGet, "/api/v1/futures/riskLimits", params, false)
  }

  // GetMaxLeverage queries risk limits for the specified symbol and returns the maximum leverage allowed.
  func (c *Client) GetMaxLeverage(ctx context.Context, symbol string) (int, error) {
  	body, err := c.rawGetRiskLimits(ctx, symbol)
  	if err != nil {
  		return 0, err
  	}
  	limits, err := parseResponse[[]toobitRiskLimitConfig](body)
  	if err != nil {
  		return 0, err
  	}
  	maxLev := 0
  	for _, rl := range limits {
  		val, err := rl.MaxLeverage.Float64()
  		if err == nil && int(val) > maxLev {
  			maxLev = int(val)
  		}
  	}
  	if maxLev == 0 {
  		return 0, fmt.Errorf("no valid risk limits found for symbol %s", symbol)
  	}
  	return maxLev, nil
  }
  ```

- [ ] **Step 2: Ensure correct imports are present in `toobit/trade.go`**
  Make sure `fmt`, `net/http`, and `crypto-bot/pkg/xjson` are imported.

- [ ] **Step 3: Run `make test` to ensure it compiles**
  Run: `make test`
  Expected: Success.

- [ ] **Step 4: Commit client implementation**
  Run:
  ```bash
  git add internal/infrastructure/exchange/toobit/trade.go
  git commit -m "feat: implement GetMaxLeverage in Toobit client"
  ```

---

### Task 3: Add Client Unit Tests for GetMaxLeverage

**Files:**
- Modify: `internal/infrastructure/exchange/toobit/client_test.go`

- [ ] **Step 1: Write unit tests in `internal/infrastructure/exchange/toobit/client_test.go`**
  Add the test case to mock the endpoint and verify correct parsing and returned max leverage value:
  ```go
  func TestClient_GetMaxLeverage(t *testing.T) {
  	t.Parallel()

  	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  		assert.Equal(t, "/api/v1/futures/riskLimits", r.URL.Path)
  		assert.Equal(t, "BTC-SWAP-USDT", r.URL.Query().Get("symbol"))
  		w.WriteHeader(http.StatusOK)
  		_, _ = w.Write([]byte(`[
  			{
  				"level": 1,
  				"quantity": "1000000.0",
  				"maintainMargin": "0.005",
  				"initialMargin": "0.01",
  				"maxLeverage": 100
  			},
  			{
  				"level": 2,
  				"quantity": "2000000.0",
  				"maintainMargin": "0.01",
  				"initialMargin": "0.02",
  				"maxLeverage": 50
  			}
  		]`))
  	}))
  	defer server.Close()

  	client := toobit.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
  	maxLev, err := client.GetMaxLeverage(context.Background(), "BTC-SWAP-USDT")
  	require.NoError(t, err)
  	assert.Equal(t, 100, maxLev)
  }
  ```

- [ ] **Step 2: Run unit test to verify it passes**
  Run: `go test -v ./internal/infrastructure/exchange/toobit -run TestClient_GetMaxLeverage`
  Expected: PASS

- [ ] **Step 3: Commit client unit test**
  Run:
  ```bash
  git add internal/infrastructure/exchange/toobit/client_test.go
  git commit -m "test: add TestClient_GetMaxLeverage unit test"
  ```

---

### Task 4: Integrate Max Leverage Cap in FireIOC

**Files:**
- Modify: `internal/bots/funding/application/reversion/fire_ioc.go`

- [ ] **Step 1: Modify `fire_ioc.go` to type-assert and adjust the leverage**
  Locate `fire_ioc.go` where `ChangeLeverage` is adjusted and add the optional interface check:
  ```go
  	if leverage > 0 && !r.deps.Client.SupportLeverageOnOrder() {
  		if provider, ok := r.deps.Client.(exchange.MaxLeverageProvider); ok {
  			maxLev, err := provider.GetMaxLeverage(ctx, evt.Symbol)
  			if err != nil {
  				r.log.ErrorContext(ctx, "Failed to get max leverage from client", slog.Any("error", err), slog.String("symbol", evt.Symbol))
  			} else if maxLev > 0 && leverage > maxLev {
  				r.log.InfoContext(ctx, "Configured leverage exceeds exchange risk limits, adjusting to max",
  					slog.String("symbol", evt.Symbol),
  					slog.Int("configured", leverage),
  					slog.Int("max", maxLev),
  				)
  				leverage = maxLev
  			}
  		}

  		r.log.InfoContext(ctx, "Adjusting leverage before fire window", slog.String("symbol", evt.Symbol), slog.Int("leverage", leverage))
  ```

- [ ] **Step 2: Run `make test` to ensure it compiles**
  Run: `make test`
  Expected: Success.

- [ ] **Step 3: Commit integration**
  Run:
  ```bash
  git add internal/bots/funding/application/reversion/fire_ioc.go
  git commit -m "feat: adjust leverage using GetMaxLeverage in fire_ioc"
  ```

---

### Task 5: Add Reversion Test Coverage

**Files:**
- Modify: `internal/bots/funding/application/reversion/reversion_test.go`

- [ ] **Step 1: Write helper mock wrapper inside `reversion_test.go`**
  Add the mock wrapper struct in [reversion_test.go](file:///home/four/projects/crypto-bot/internal/bots/funding/application/reversion/reversion_test.go):
  ```go
  type mockClientWithMaxLeverage struct {
  	exchange.Client
  	maxLeverage    int
  	maxLeverageErr error
  }

  func (m *mockClientWithMaxLeverage) GetMaxLeverage(ctx context.Context, symbol string) (int, error) {
  	return m.maxLeverage, m.maxLeverageErr
  }
  ```

- [ ] **Step 2: Add a test case in `reversion_test.go` to assert leverage capping behavior**
  Add a new test `TestReversion_LeverageCapping` or modify an existing test to use `mockClientWithMaxLeverage` and assert that the target leverage is capped at `maxLeverage`.
  Let's add `TestReversion_LeverageCapping` based on `TestReversion_Execute_Success` but wrapping `mockClient` with `mockClientWithMaxLeverage` where `maxLeverage = 5` and the configured leverage is `10`. Verify that `ChangeLeverage` or `SwitchMarginMode` receives `5` instead of `10`.

- [ ] **Step 3: Run the reversion tests**
  Run: `go test -v ./internal/bots/funding/application/reversion -run TestReversion_LeverageCapping`
  Expected: PASS

- [ ] **Step 4: Run full quality gates**
  Run: `make lint` and `make test` and `make cover-check`
  Expected: Success.

- [ ] **Step 5: Commit reversion tests**
  Run:
  ```bash
  git add internal/bots/funding/application/reversion/reversion_test.go
  git commit -m "test: add reversion test case for leverage capping"
  ```

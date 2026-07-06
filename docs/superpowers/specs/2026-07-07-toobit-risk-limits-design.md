# Toobit Risk Limits Retrieval and Max Leverage Enforcement Design

## 1. Goal Description
The objective is to implement the retrieval of Toobit's risk limits config (`GET /api/v1/futures/riskLimits`) and use this data to dynamically enforce/adjust the target leverage before invoking `ChangeLeverage` on the exchange client.

## 2. Architecture & Design

### 2.1 Interface Definition
We will define a new optional interface, `MaxLeverageProvider`, inside [interfaces.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/interfaces.go):

```go
// MaxLeverageProvider is an optional interface that exchange REST clients can implement
// to support retrieving the maximum leverage allowed for a symbol.
type MaxLeverageProvider interface {
	GetMaxLeverage(ctx context.Context, symbol string) (int, error)
}
```

### 2.2 Client Implementation
The Toobit `Client` will implement this interface.

#### 2.2.1 Data Mapping
Define a new struct `toobitRiskLimitConfig` in `toobit` to parse the API response from `GET /api/v1/futures/riskLimits`.
```go
type toobitRiskLimitConfig struct {
	Level          int          `json:"level"`
	Quantity       string       `json:"quantity"`
	MaintainMargin string       `json:"maintainMargin"`
	InitialMargin  string       `json:"initialMargin"`
	MaxLeverage    xjson.Number `json:"maxLeverage"`
}
```

#### 2.2.2 Endpoints and Private Methods
We will declare the private method `rawGetRiskLimits` in [trade.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/trade.go):
```go
func (c *Client) rawGetRiskLimits(ctx context.Context, symbol string) ([]byte, error) {
	params := map[string]string{
		symbolKey: symbol,
	}
	// Public endpoint, signed = false
	return c.request(ctx, http.MethodGet, "/api/v1/futures/riskLimits", params, false)
}
```

#### 2.2.3 Implementing the Interface Method
Add `GetMaxLeverage` to [trade.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/trade.go):
```go
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

### 2.3 Bot Integration
In [fire_ioc.go](file:///home/four/projects/crypto-bot/internal/bots/funding/application/reversion/fire_ioc.go), check if the Client satisfies `MaxLeverageProvider`. If it does, call `GetMaxLeverage` before calling `ChangeLeverage`.

```go
	if provider, ok := r.deps.Client.(exchange.MaxLeverageProvider); ok {
		maxLev, err := provider.GetMaxLeverage(ctx, evt.Symbol)
		if err != nil {
			r.log.ErrorContext(ctx, "Failed to get max leverage from client", slog.Any("error", err), slog.String("symbol", evt.Symbol))
			// Fallback: we do not abort if query fails (best effort), or should we?
			// We will log the error and fall back to the existing configured leverage.
		} else if maxLev > 0 && leverage > maxLev {
			r.log.InfoContext(ctx, "Configured leverage exceeds exchange risk limits, adjusting to max",
				slog.String("symbol", evt.Symbol),
				slog.Int("configured", leverage),
				slog.Int("max", maxLev),
			)
			leverage = maxLev
		}
	}
```

## 3. Testing Plan

### 3.1 Unit Testing Toobit Client
In [client_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/toobit/client_test.go), add a unit test:
*   `TestClient_GetMaxLeverage` to mock `GET /api/v1/futures/riskLimits` response and verify correct parser behavior and the correct returned max leverage value.

### 3.2 Mocking & Verification
Ensure we update mocks for `exchange.Client` (if needed) or verify that other exchange tests are unaffected. Since `MaxLeverageProvider` is an optional interface checked by type assertion at runtime, the mock client doesn't need to implement it unless explicitly tested in bot-level tests.

---
*Created on 2026-07-07.*

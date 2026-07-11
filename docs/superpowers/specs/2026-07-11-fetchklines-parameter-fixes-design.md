# Design Spec: FetchKlines Parameter and Limit Fixes across Exchanges

This document outlines the design for correcting the `FetchKlines` implementations for BloFin, Hotcoin, and Pionex REST clients to properly support query timestamps and limits.

## 1. Objective
Ensure that `FetchKlines` queries use the provided `start` and `end` times whenever supported by the exchange's REST API, and utilize suitable `limit` parameters to optimize response payloads and avoid default maximums.

## 2. Exchange-Specific Corrections

### 2.1. BloFin (`internal/infrastructure/exchange/blofin/klines.go`)
- **Current Behavior:** Ignores `start` parameter completely (`_` signature). Only maps `end` to `after`.
- **Target Behavior:**
  - Rename the unused signature parameter `_` to `start`.
  - If `start` is not zero, add `"before"` query parameter: `params["before"] = fmt.Sprintf("%d", start.UnixMilli())`.
  - Add explicit `"limit": "100"`.
- **Expected Mock Handler Assertions:**
  - Assert `"limit"` is `"100"`.
  - Assert `"before"` is `"1783665240000"` (in a new test verifying both parameters).

### 2.2. Hotcoin (`internal/infrastructure/exchange/hotcoin/klines.go`)
- **Current Behavior:** Ignores both `start` and `end` parameters (`_, _` signature) and retrieves the default latest 200 candles.
- **Target Behavior:**
  - Rename signature parameters `_, _` to `start, end`.
  - If `end` is not zero, add `"since"` query parameter: `params["since"] = fmt.Sprintf("%d", end.UnixMilli())` (since is the API cursor for candles leading up to the timestamp).
  - Add explicit `"size": "100"` (size is the API parameter for limit).
  - Sort the resulting `exchange.Kline` slice chronologically (ascending) before returning.
- **Expected Mock Handler Assertions:**
  - Assert `"size"` is `"100"`.
  - Assert `"since"` is `"1783687800000"`.

### 2.3. Pionex (`internal/infrastructure/exchange/pionex/klines.go`)
- **Current Behavior:** Ignores `start` (not supported by V1 API) and uses `end` mapped to `endTime`, but does not pass a limit parameter.
- **Target Behavior:**
  - Keep `start` ignored as the Pionex API V1 `/api/v1/market/klines` does not support starting time query filters.
  - Add explicit `"limit": "100"`.
- **Expected Mock Handler Assertions:**
  - Assert `"limit"` is `"100"`.

---

## 3. Code Modifications Summary

1. **`internal/infrastructure/exchange/blofin/klines.go`**: Use `start`, `end`, and add `limit` param.
2. **`internal/infrastructure/exchange/blofin/klines_test.go`**: Assert `limit` and add test case for `start` time.
3. **`internal/infrastructure/exchange/hotcoin/klines.go`**: Use `end` as `since`, sort results, and add `size` param.
4. **`internal/infrastructure/exchange/hotcoin/klines_test.go`**: Assert `size` and add test case for `end` time.
5. **`internal/infrastructure/exchange/pionex/klines.go`**: Add `limit` param.
6. **`internal/infrastructure/exchange/pionex/klines_test.go`**: Assert `limit` in mock server.

---

## 4. Verification Plan
- Run tests for each modified package:
  - `go test -v ./internal/infrastructure/exchange/blofin/...`
  - `go test -v ./internal/infrastructure/exchange/hotcoin/...`
  - `go test -v ./internal/infrastructure/exchange/pionex/...`
- Run client factory integration tests:
  - `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run quality gate checks: `make lint`, `make test`, `make cover`.

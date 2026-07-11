# Design Spec: FetchKlines EndTime and Limit Fixes (Binance, XT, Gate)

This document outlines the design for correcting `FetchKlines` parameter serialization in `binance`, `xt`, and `gate` exchange clients.

## 1. Objective
Ensure that `binance`, `xt`, and `gate` adapters properly serialize the `end` time parameter (mapping to `endTime` or `to` depending on exchange API) and include a suitable limit of `100`.

## 2. Target Adjustments

### 2.1. Binance (`internal/infrastructure/exchange/binance/market.go`)
- **Fixes:**
  - Only map `"startTime"` if `!start.IsZero()`.
  - Map `end` $\rightarrow$ `"endTime"` if `!end.IsZero()`.
  - Pass `"limit"` with value `100`.
- **Unit Tests:**
  - Add `TestClient_FetchKlines` in `internal/infrastructure/exchange/binance/client_test.go` verifying query parameter mapping.

### 2.2. Gate (`internal/infrastructure/exchange/gate/market.go`)
- **Fixes:**
  - Only map `"from"` if `!start.IsZero()`.
  - Map `end` $\rightarrow$ `"to"` (as Unix seconds timestamp string) if `!end.IsZero()`.
  - Change `"limit"` from `"35"` to `"100"`.
- **Unit Tests:**
  - Add `TestClient_FetchKlines` in `internal/infrastructure/exchange/gate/client_test.go` verifying query parameter mapping.

### 2.3. XT (`internal/infrastructure/exchange/xt/klines.go`)
- **Fixes:**
  - Map `end` $\rightarrow$ `"endTime"` (as Unix millisecond timestamp string) if `!end.IsZero()`.
  - Set `"limit"` to `"100"`.
- **Unit Tests:**
  - Update `internal/infrastructure/exchange/xt/klines_test.go` to assert `"limit"` query param is `"100"`.
  - Add test case `TestClient_FetchKlines_WithEnd` to verify `endTime` mapping.

---

## 3. Verification Plan
- Run tests for each package:
  - `go test -v ./internal/infrastructure/exchange/binance/...`
  - `go test -v ./internal/infrastructure/exchange/gate/...`
  - `go test -v ./internal/infrastructure/exchange/xt/...`
- Run client factory integration tests.
- Run project-wide quality gates (`make lint`, `make test`, `make cover`).

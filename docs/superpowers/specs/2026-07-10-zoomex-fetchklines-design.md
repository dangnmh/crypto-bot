# Design Spec: Implement FetchKlines for Zoomex Futures

This document outlines the design for implementing the `FetchKlines` method on the Zoomex client.

## 1. Objective
Replace the stub implementation of `FetchKlines` in `internal/infrastructure/exchange/zoomex/klines.go` with a working integration that queries the public Zoomex Futures REST API.

## 2. API Endpoint & Parameters
We will call the public REST API endpoint:
- **Path:** `/cloud/trade/v3/market/kline`
- **Method:** `GET`
- **Authentication:** Public
- **Query Parameters:**
  - `symbol` (string): The standard symbol (e.g. `BTCUSDT`).
  - `category` (string): Fixed to `linear`.
  - `interval` (string): Timeframe interval minutes or letters (e.g. `60`, `D`).
  - `start` (string, optional): Start time Unix timestamp in milliseconds.
  - `end` (string, optional): End time Unix timestamp in milliseconds.

### Interval Mapping
Our `exchange.Interval` values will map to standard Bybit/Zoomex interval strings:
- `Interval1m` -> `1`
- `Interval3m` -> `3`
- `Interval5m` -> `5`
- `Interval15m` -> `15`
- `Interval30m` -> `30`
- `Interval1h` -> `60`
- `Interval2h` -> `120`
- `Interval4h` -> `240`
- `Interval6h` -> `360`
- `Interval8h` -> `360` (fallback)
- `Interval12h` -> `720`
- `Interval1d` -> `D`
- `Interval1w` -> `W`
- `Interval1M` -> `M`

### Response Format & Parsing
The Zoomex API returns the envelope:
```json
{
  "retCode": 0,
  "retMsg": "OK",
  "result": {
    "symbol": "BTCUSDT",
    "category": "linear",
    "list": [
      [
        "1783681200000", // 0: Timestamp (milliseconds)
        "64373.4",       // 1: Open Price
        "64448.5",       // 2: High Price
        "64328.4",       // 3: Low Price
        "64365",         // 4: Close Price
        "91.882",        // 5: Volume
        "5916361.7961"   // 6: Turnover (Amount)
      ]
    ]
  }
}
```
We will parse this nested slice into `[]exchange.Kline` where:
- `Timestamp` = Index 0 (converted to int64)
- `Open` = Index 1 (converted to float64)
- `High` = Index 2 (converted to float64)
- `Low` = Index 3 (converted to float64)
- `Close` = Index 4 (converted to float64)
- `Volume` = Index 5 (converted to float64)

We will use `xjson.Number` to safely convert values.

## 3. Code Modifications

### 3.1. `internal/infrastructure/exchange/zoomex/klines.go`
- Implement `mapZoomexInterval(interval exchange.Interval) string`.
- Implement `rawGetKlines` private helper calling `/cloud/trade/v3/market/kline`.
- Implement the public `FetchKlines` method mapping the request parameters, executing `rawGetKlines`, parsing, and returning the `exchange.Kline` slice.

### 3.2. `internal/infrastructure/app/client_factory_test.go`
- Add `"zoomex"` to the `supported` map in `TestAllExchangesFetchKlinesSupport`.

### 3.3. `internal/infrastructure/exchange/zoomex/klines_test.go`
- Create `klines_test.go` with mock HTTP server unit tests.

## 4. Verification Plan
- Run the factory test: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run the Zoomex package tests: `go test -v ./internal/infrastructure/exchange/zoomex/...`
- Verify pre-commit quality gates pass: `make lint`, `make test`, `make cover`.

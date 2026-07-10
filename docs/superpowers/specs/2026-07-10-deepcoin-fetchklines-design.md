# Design Spec: Implement FetchKlines for Deepcoin Futures

This document outlines the design for implementing the `FetchKlines` method on the Deepcoin client.

## 1. Objective
Replace the stub implementation of `FetchKlines` in `internal/infrastructure/exchange/deepcoin/klines.go` with a working integration that queries the public Deepcoin Futures REST API.

## 2. API Endpoint & Parameters
We will call the public REST API endpoint:
- **Path:** `/deepcoin/market/candles`
- **Method:** `GET`
- **Authentication:** Public
- **Query Parameters:**
  - `instId` (string): Product ID (e.g. `BTC-USDT-SWAP`).
  - `bar` (string): Time granularity (e.g. `1H`, `1D`).
  - `after` (string, optional): Used for pagination; requests content before this timestamp.
  - `limit` (string, optional): Number of results (max 300).

### Interval Mapping
Our `exchange.Interval` values will map to casing-sensitive Deepcoin `bar` strings:
- `Interval1m` -> `1m`
- `Interval3m` -> `1m` (fallback)
- `Interval5m` -> `5m`
- `Interval15m` -> `15m`
- `Interval30m` -> `30m`
- `Interval1h` -> `1H`
- `Interval2h` -> `1H` (fallback)
- `Interval4h` -> `4H`
- `Interval6h` -> `4H` (fallback)
- `Interval8h` -> `4H` (fallback)
- `Interval12h` -> `12H`
- `Interval1d` -> `1D`
- `Interval1w` -> `1W`
- `Interval1M` -> `1M`

### Response Format & Parsing
The Deepcoin API returns:
```json
{
  "code": "0",
  "msg": "",
  "data": [
    [
      "1783681200000", // 0: Timestamp (milliseconds)
      "64375.7",       // 1: Open Price
      "64456",         // 2: High Price
      "64302.4",       // 3: Low Price
      "64436.1",       // 4: Close Price
      "448016",        // 5: Volume (Base)
      "28841508.98"    // 6: Turnover (Quote)
    ]
  ]
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

### 3.1. `internal/infrastructure/exchange/deepcoin/klines.go`
- Implement `mapDeepcoinInterval(interval exchange.Interval) string`.
- Implement `rawGetKlines` private helper calling `/deepcoin/market/candles`.
- Implement the public `FetchKlines` method mapping the request parameters, executing `rawGetKlines`, parsing, and returning the `exchange.Kline` slice.

### 3.2. `internal/infrastructure/app/client_factory_test.go`
- Add `"deepcoin"` to the `supported` map inside `TestAllExchangesFetchKlinesSupport`.

### 3.3. `internal/infrastructure/exchange/deepcoin/klines_test.go`
- Create `klines_test.go` with mock HTTP server unit tests.

## 4. Verification Plan
- Run the factory test: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run the Deepcoin package tests: `go test -v ./internal/infrastructure/exchange/deepcoin/...`
- Verify pre-commit quality gates pass: `make lint`, `make test`, `make cover`.

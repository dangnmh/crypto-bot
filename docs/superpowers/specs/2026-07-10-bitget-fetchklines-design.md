# Design Spec: Implement FetchKlines for Bitget Futures

This document outlines the design for implementing the `FetchKlines` method on the Bitget V2 Futures client.

## 1. Objective
Replace the stub implementation of `FetchKlines` in `internal/infrastructure/exchange/bitget/klines.go` with a working integration that queries the public Bitget Futures REST API.

## 2. API Endpoint & Parameters
We will call the public REST API endpoint:
- **Path:** `/api/v2/mix/market/candles`
- **Method:** `GET`
- **Authentication:** Public
- **Query Parameters:**
  - `symbol` (string): The futures contract symbol (e.g. `BTCUSDT`).
  - `productType` (string): Product type (fixed to `USDT-FUTURES` using `productTypeUsdtFutures`).
  - `granularity` (string): Timeframe interval (e.g. `1H` for 1 hour, `1D` for 1 day).
  - `startTime` (string, optional): Start time Unix timestamp in milliseconds.
  - `endTime` (string, optional): End time Unix timestamp in milliseconds.

### Interval Mapping
Our `exchange.Interval` values will map to casing-sensitive Bitget granularity strings:
- `Interval1m` -> `1m`
- `Interval3m` -> `3m`
- `Interval5m` -> `5m`
- `Interval15m` -> `15m`
- `Interval30m` -> `30m`
- `Interval1h` -> `1H`
- `Interval2h` -> `1H` (fallback)
- `Interval4h` -> `4H`
- `Interval6h` -> `6H`
- `Interval8h` -> `6H` (fallback)
- `Interval12h` -> `12H`
- `Interval1d` -> `1D`
- `Interval1w` -> `1W`
- `Interval1M` -> `1M`

### Response Format & Parsing
The Bitget Futures API returns the standard envelope:
```json
{
  "code": "00000",
  "msg": "success",
  "data": [
    [
      "1783674000000", // 0: Timestamp (milliseconds)
      "64199.4",       // 1: Open Price
      "64438",         // 2: High Price
      "64060.6",       // 3: Low Price
      "64340.5",       // 4: Close Price
      "1804.7384",     // 5: Volume
      "116077022.091"  // 6: Turnover (Amount)
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

### 3.1. `internal/infrastructure/exchange/bitget/klines.go`
- Implement `mapBitgetInterval(interval exchange.Interval) string`.
- Implement `rawGetKlines` private helper following standard client conventions (e.g. wrapper calling `c.RawRequest`).
- Implement the public `FetchKlines` method mapping the request parameters, executing `rawGetKlines`, parsing, and returning the `exchange.Kline` slice.

### 3.2. `internal/infrastructure/app/client_factory_test.go`
- Add `"bitget"` to the list of supported exchanges in the test assertion `TestAllExchangesFetchKlinesSupport` to verify its integration.

### 3.3. `internal/infrastructure/exchange/bitget/klines_test.go`
- Create `klines_test.go` using a mock http server mapping the `/api/v2/mix/market/candles` path to verify both success and error handling paths.

## 4. Verification Plan
- Run the factory test: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run the Bitget package tests: `go test -v ./internal/infrastructure/exchange/bitget/...`
- Verify pre-commit quality gates pass: `make lint`, `make test`, `make cover`.

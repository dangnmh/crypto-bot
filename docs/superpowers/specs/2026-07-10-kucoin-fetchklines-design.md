# Design Spec: Implement FetchKlines for KuCoin Futures

This document outlines the design for implementing the `FetchKlines` method on the KuCoin Futures client.

## 1. Objective
Replace the stub implementation of `FetchKlines` in `internal/infrastructure/exchange/kucoin/klines.go` with a working integration that queries the public KuCoin Futures REST API.

## 2. API Endpoint & Parameters
We will call the public REST API endpoint:
- **Path:** `/api/v1/kline/query`
- **Method:** `GET`
- **Authentication:** Public (No signature/authentication required, but standard wrapper handles signature optionally if keys are present).
- **Query Parameters:**
  - `symbol` (string): The futures contract symbol (e.g. `XBTUSDM`).
  - `granularity` (string): Timeframe interval in minutes (as an integer string, e.g. `60` for 1 hour).
  - `from` (string, optional): Start time Unix timestamp in milliseconds.
  - `to` (string, optional): End time Unix timestamp in milliseconds.

### Interval Mapping
Our `exchange.Interval` values will map to minute-based granularity strings:
- `Interval1m` -> `1`
- `Interval3m` -> `3`
- `Interval5m` -> `5`
- `Interval15m` -> `15`
- `Interval30m` -> `30`
- `Interval1h` -> `60`
- `Interval2h` -> `120`
- `Interval4h` -> `240`
- `Interval6h` -> `360`
- `Interval8h` -> `480`
- `Interval12h` -> `720`
- `Interval1d` -> `1440`
- `Interval1w` -> `10080`
- `Interval1M` -> `43200`

### Response Format & Parsing
The KuCoin Futures API returns the standard envelope:
```json
{
  "code": "200000",
  "data": [
    [
      1783447200000, // 0: Timestamp (milliseconds)
      64085.7,       // 1: Open Price
      64089.0,       // 2: High Price
      63618.5,       // 3: Low Price
      63618.5,       // 4: Close Price
      7282,          // 5: Volume
      7282.0         // 6: Turnover (Amount)
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

## 3. Code Modifications

### 3.1. `internal/infrastructure/exchange/kucoin/klines.go`
- Implement `mapKucoinInterval(interval exchange.Interval) string`.
- Implement `rawGetKlines` private helper following standard client conventions (e.g. wrapper calling `c.RawRequest`).
- Implement the public `FetchKlines` method mapping the request parameters, executing `rawGetKlines`, parsing, and returning the `exchange.Kline` slice.

### 3.2. `internal/infrastructure/app/client_factory_test.go`
- Add `"kucoin"` to the list of supported exchanges in the test assertion `TestAllExchangesFetchKlinesSupport` to verify its integration.

### 3.3. `internal/infrastructure/exchange/kucoin/client_test.go`
- Add `TestClient_FetchKlines` using a mock http server mapping the `/api/v1/kline/query` path to verify both success and error handling paths.

## 4. Verification Plan
- Run the factory test: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run the KuCoin package tests: `go test -v ./internal/infrastructure/exchange/kucoin/...`
- Verify pre-commit quality gates pass: `make lint`, `make test`, `make cover`.

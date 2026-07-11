# Design Spec: Implement FetchKlines for Kraken Futures

This document outlines the design for implementing the `FetchKlines` method on the Kraken Futures REST client.

## 1. Objective
Replace the stub implementation of `FetchKlines` in `internal/infrastructure/exchange/krakenfutures/klines.go` with a working integration that queries the public Kraken Futures charts REST API.

## 2. API Endpoint & Parameters
We will call the public charts REST API endpoint:
- **Path:** `/api/charts/v1/trade/{symbol}/{resolution}`
- **Method:** `GET`
- **Authentication:** Public (No credentials or signatures needed)
- **Query Parameters:**
  - `from` (string/integer, optional): The starting UNIX timestamp in milliseconds.
  - `to` (string/integer, optional): The ending UNIX timestamp in milliseconds.

### Symbol Mapping
Standard symbols (e.g. `BTCUSD`, `ETHUSD`) must be mapped to Kraken Futures symbols (e.g. `PF_XBTUSD`, `PF_ETHUSD`):
1. Convert the symbol to uppercase.
2. Replace `BTC` with `XBT`.
3. Prepend `PF_` if not already present.

### Interval Mapping
The `exchange.Interval` constants will map to standard Kraken Futures resolutions:
- `Interval1m` -> `1m`
- `Interval3m` -> `1m` (fallback)
- `Interval5m` -> `5m`
- `Interval15m` -> `15m`
- `Interval30m` -> `30m`
- `Interval1h` -> `1h`
- `Interval2h` -> `1h` (fallback)
- `Interval4h` -> `4h`
- `Interval6h` -> `4h` (fallback)
- `Interval8h` -> `4h` (fallback)
- `Interval12h` -> `12h`
- `Interval1d` -> `1d`
- `Interval1w` -> `1w`
- `Interval1M` -> `1d` (fallback)

### Response Format & Parsing
The response payload contains a `candles` array of objects:
```json
{
  "candles": [
    {
      "time": 1625616000000,
      "open": "34557.84",
      "high": "34803.20",
      "low": "33816.32",
      "close": "33880.22",
      "volume": "100.5"
    }
  ]
}
```
We will parse this into `[]exchange.Kline` where:
- `Timestamp` = `time` (Directly in milliseconds)
- `Open` = `open` (converted using `xjson.Number`)
- `High` = `high` (converted using `xjson.Number`)
- `Low` = `low` (converted using `xjson.Number`)
- `Close` = `close` (converted using `xjson.Number`)
- `Volume` = `volume` (converted using `xjson.Number`)

We will sort the resulting slice chronologically by `Timestamp` ascending before returning.

## 3. Code Modifications

### 3.1. `internal/infrastructure/exchange/krakenfutures/klines.go`
- Implement `toKrakenSymbol(symbol string) string`.
- Implement `mapKrakenInterval(interval exchange.Interval) (string, error)`.
- Replace the `FetchKlines` method to map the request, hit `/api/charts/v1/trade/{symbol}/{resolution}`, parse the JSON response, and return the sorted `exchange.Kline` slice.

### 3.2. `internal/infrastructure/app/client_factory_test.go`
- Add `"krakenfutures": true` to the `supported` map in `TestAllExchangesFetchKlinesSupport`.

### 3.3. `internal/infrastructure/exchange/krakenfutures/klines_test.go` [NEW]
- Write unit tests using a mock `httptest.NewServer` to verify correct query generation and response parsing.

### 3.4. `exchanges.md`
- Check krakenfutures: `- [x] krakenfutures`.

## 4. Verification Plan
- Run tests: `go test -v ./internal/infrastructure/exchange/krakenfutures/...`
- Run client factory tests: `go test -v ./internal/infrastructure/app/... -run TestAllExchangesFetchKlinesSupport`
- Run pre-commit quality gates: `make lint`, `make test`, `make cover`.

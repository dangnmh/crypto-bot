# Hotcoin Position List Integration Design Spec

Date: 2026-07-06

## 1. Goal Description
Implement the `GetOpenPositions` and `GetOpenPositionsRaw` client methods for Hotcoin exchange integration in the High-Frequency Trading cryptocurrency bot. The Hotcoin exchange API now provides a position listing endpoint: `GET /api/v1/perpetual/position/{contractCode}/list`.

## 2. API Details
- **Endpoint**: `GET /api/v1/perpetual/position/{contractCode}/list`
- **Method**: `GET`
- **Path Parameter**: `contractCode` (Required). E.g., `btcusdt` for symbol `BTC_USDT`.
- **Query Parameters**: Signed parameters (`AccessKeyId`, `SignatureVersion`, `SignatureMethod`, `Signature`, `Timestamp`).
- **Response Structure**:
  Returns a list of position objects. The API can return this directly as a JSON array or wrapped inside a `{"code": 200, "msg": "success", "data": [...]}` envelope. The implementation must support both response types.

## 3. Proposed Changes

### [Component: Hotcoin Exchange Adapter]

#### [MODIFY] [position.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/hotcoin/position.go)
- Define `hotcoinPosition` struct containing the required response fields (using `xjson.Number` for numeric values):
  - `Amount`
  - `ContractCode`
  - `Side`
  - `Price`
  - `Fee`
  - `Lever`
- Implement `GetOpenPositions(ctx, symbol)`:
  - If `symbol` is empty, return an error immediately (contractCode required).
  - Convert `symbol` to `contractCode` (lowercase, no underscores).
  - Call `GetOpenPositionsRaw` with `symbol` param.
  - Parse the raw response, support both direct array and wrapped response shapes.
  - Map each `hotcoinPosition` to `exchange.Position` if `HoldVol > 0`.
  - Normalize symbol name (e.g. `btcusdt` -> `BTC_USDT`) for the returned structures.

#### [MODIFY] [client.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/hotcoin/client.go)
- Modify `GetOpenPositionsRaw` to construct and invoke the path `/api/v1/perpetual/position/{contractCode}/list` when a symbol is provided.
- If `symbol` is empty, return an error.

#### [MODIFY] [client_test.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/hotcoin/client_test.go)
- Add mock HTTP server tests to verify `GetOpenPositions` functionality:
  - Test with a valid symbol returning a direct JSON array.
  - Test with a valid symbol returning a wrapped JSON envelope.
  - Test handling of empty symbol (should return error).
  - Verify mapping of fields (`HoldVol`, `PositionType`, `OpenAvgPrice`, `HoldAvgPrice`, `Leverage`, `Fee`).

## 4. Verification Plan

### Automated Tests
- Run `go test ./internal/infrastructure/exchange/hotcoin/...` to ensure all tests pass.
- Run `make lint` and `make cover` to satisfy pre-commit quality gates.

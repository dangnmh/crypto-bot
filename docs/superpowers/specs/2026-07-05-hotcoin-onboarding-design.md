# Onboard Hotcoin Exchange Client Design

This document describes the design to fully onboard the Hotcoin perpetual futures exchange client into the `crypto-bot` trading system, moving it from a public-only scanner integration to a fully operational High-Frequency Trading (HFT) arbitrage exchange client.

## 1. System Components & Config Registration

### A. Environment Configuration & Vault
- **Local Config**:
  - Add template credentials to `.env.example`:
    ```bash
    HOTCOIN_API_KEY=""
    HOTCOIN_API_SECRET=""
    ```
- **Kubernetes Secret Seeding**:
  - Add `HOTCOIN_API_KEY` and `HOTCOIN_API_SECRET` to the list of vault secrets in [deploy/k8s/vault-init.sh](file:///home/four/projects/crypto-bot/deploy/k8s/vault-init.sh).

### B. Code Types & Metadata Specs
- **Exchange Constants**:
  - Register constant `ExchangeHotcoin = "hotcoin"` in [internal/infrastructure/exchange/types.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/types.go).
  - Register constant `HotcoinName = "hotcoin"` in [internal/infrastructure/config/types.go](file:///home/four/projects/crypto-bot/internal/infrastructure/config/types.go).
- **Exchange Specs**:
  - Register Hotcoin in the `ExchangeSpecs` registry map inside [internal/infrastructure/config/types.go](file:///home/four/projects/crypto-bot/internal/infrastructure/config/types.go):
    ```go
    HotcoinName: {
        RequiresPassphrase: false,
        Validate: func(cfg APIConfig) error {
            if cfg.APIKey == "" || cfg.APISecret == "" {
                return fmt.Errorf("hotcoin requires api key and secret")
            }
            return nil
        },
    },
    ```

### C. JSONC Configuration Files
Add standard profiles for `hotcoin` under:
- `configs/funding/local/exchange.jsonc` & `configs/funding/prod/exchange.jsonc`:
  ```jsonc
  "hotcoin": {
    "enable": false,
    "future": {
      "baseURL": "https://api-ct.hotcoin.fit"
    },
    "websocket": {
      "publicURL": "wss://wss.hotcoinfin.com/trade/multiple",
      "privateURL": "wss://wss.hotcoinfin.com/trade/multiple",
      "maxPairsPerWSConn": 20
    }
  }
  ```
- `configs/funding/local/blacklist.jsonc` & `configs/funding/prod/blacklist.jsonc`:
  ```jsonc
  "hotcoin": []
  ```
- `configs/funding/local/reversion.jsonc` & `configs/funding/prod/reversion.jsonc`:
  ```jsonc
  "hotcoin": {
    "takeProfitPct": 1,
    "stopLossPct": 2,
    "bufferTime": "30ms",
    "postSettleTimeout": "300s"
  }
  ```

---

## 2. REST Client Structure (6-File Layout)

The client code will be organized inside [internal/infrastructure/exchange/hotcoin/](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/hotcoin/).

### A. client.go
- Manages HTTP request building, logging via `transportlog` (redacting sensitive keys `AccessKeyId` and `Signature`), and custom signing.
- **Signing Scheme (Signature Version 2)**:
  - Base string generated as: `HTTP_METHOD + "\n" + HOSTNAME + "\n" + REQUEST_PATH + "\n" + SORTED_QUERY_PARAMS`
  - Compute `HmacSHA256` hash of base string using `SecretKey`.
  - Base64 encode the resulting hash to form the signature.
  - Append the signature to request parameters as `Signature`.
- **Clock Synchronization**: Support timestamp drifts by integrating `exchange.Clock`.

### B. system.go
- `GetServerTime`: GET `/api/v1/perpetual/public/time` or local sync offset.
- `WarmUp`: Ping API to keep TCP connection warm.
- `SupportLeverageOnOrder`: Returns `false` (leverage must be set out-of-band).

### C. market.go
- `GetTickers`: GET `/api/v1/perpetual/public/contracts` (maps bids, asks, last price, volumes).
- `GetContractDetails`: GET `/api/v1/perpetual/public/contracts` (maps instrument decimals, price scale, min volume, lot size).
- `GetFundingRates`: GET `/api/v1/perpetual/public/contracts` (maps `fundingRate` and `nextFundingRate`).

### D. order.go
- `CreateOrder`: `POST /api/v1/perpetual/products/{contractCode}/order`
  - Parameters: `type` (10=Limit, 11=Market), `side` (`open_long`, `open_short`, `close_long`, `close_short`), `price`, `amount`, `beMaker` (1 for PostOnly), `ioc` (1 for IOC).
- `CancelOrder`: `DELETE /api/v1/perpetual/products/{contractCode}/order/{id}`
- `CancelOrders` / `CancelAllOpenOrders`: Loops through target order IDs and deletes them.
- `GetOrder`: `GET /api/v1/perpetual/products/{contractCode}/{orderId}`
- `GetOpenOrders`: `GET /api/v1/perpetual/products/{contractCode}/open-orders` (or fallback).
- `GetOrderPNL`: Computes PnL metrics for closed positions.

### E. trade.go
- `ChangeLeverage`: `POST /api/v1/perpetual/position/leverage` or similar leverage update route.
- `SwitchMarginMode`: Programmatically switches isolated vs cross modes.

### F. position.go
- `GetOpenPositions`: `GET /api/v1/perpetual/position/{contractCode}` or list of active contracts.
- `ClosePosition`: Executed via `CreateOrder` with a market order.
- `CloseAllPositions`: Closed natively or loops through open positions.

---

## 3. WebSocket Subscription Adapter (ws_adapter.go)

- Implement `ws.ExchangeAdapter` to route WS streams:
  - Connection protocol handshakes and authentication signature verification.
  - Extractor routing of `"ticker"`, `"personal.position"`, and `"personal.order"` events.
  - Event parsing mapping raw JSON payloads to `store.PriceData`, `exchange.PersonalPositionUpdate`, and `WsOrderDeal` models.

---

## 4. Provider Factory & Verification

- Register Hotcoin simple provider factory inside `DefaultProviderFactories()` in [internal/infrastructure/app/provider_factory.go](file:///home/four/projects/crypto-bot/internal/infrastructure/app/provider_factory.go).
- Run unit tests: `go test -v ./internal/infrastructure/exchange/hotcoin/...`.
- Verify compilation and code quality gates: `make lint` and `make cover`.

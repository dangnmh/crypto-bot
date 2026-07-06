# LBank Trade Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement full private trading capabilities (REST client and WebSocket adapter) for LBank futures/CFD and register it as an active reversion exchange provider.

**Architecture:** Extend the existing public-only LBank client into the standard 6-file REST layout. Implement signature authentication using MD5 and HMAC-SHA256, write the WebSocket adapter, wire LBank into the provider factory registry, configure local/production JSONC settings, and write comprehensive unit tests.

**Tech Stack:** Go, standard library net/http, crypto/hmac, crypto/sha256, crypto/md5, Gorilla WebSocket, testify.

## Global Constraints
- Strictly follow Clean Architecture and Domain-Driven Design.
- All JSON marshaling/unmarshaling must use `crypto-bot/pkg/xjson` package.
- All HTTP requests to LBank API endpoints must go through private camelCase `rawDoSomething` functions.
- Pass `make lint`, `make test`, and maintain project-wide test coverage >= 75%.

---

### Task 1: Setup Environment Variables and Configuration Loader

**Files:**
- Modify: `.env.example`
- Modify: `deploy/k8s/vault-init.sh`
- Modify: `internal/infrastructure/exchange/types.go`
- Modify: `internal/infrastructure/config/types.go`

- [ ] **Step 1: Add LBank variables to `.env.example`**
  Add the following lines at the end of [.env.example](file:///home/four/projects/crypto-bot/.env.example):
  ```bash
  # LBank API Configuration
  LBANK_API_KEY=""
  LBANK_API_SECRET=""
  ```

- [ ] **Step 2: Add LBank variables to Kubernetes Vault initialization**
  Add the parameters to the KV secrets in [vault-init.sh](file:///home/four/projects/crypto-bot/deploy/k8s/vault-init.sh):
  ```bash
  LBANK_API_KEY="lbank_api_key_from_vault" \
  LBANK_API_SECRET="lbank_api_secret_from_vault" \
  ```

- [ ] **Step 3: Update Exchange Type Constant**
  In [types.go](file:///home/four/projects/crypto-bot/internal/infrastructure/exchange/types.go), ensure `ExchangeLbank` is defined:
  ```go
  ExchangeLbank = "lbank"
  ```

- [ ] **Step 4: Register LBank Config Package Name and Spec**
  In [types.go](file:///home/four/projects/crypto-bot/internal/infrastructure/config/types.go), define `LbankName` and update the `ExchangeSpecs` registry map:
  ```go
  const LbankName = "lbank"

  var ExchangeSpecs = map[string]ExchangeSpec{
  	// ...
  	LbankName: {
  		RequiresPassphrase: false,
  		Validate: func(cfg APIConfig) error {
  			if cfg.APIKey == "" || cfg.APISecret == "" {
  				return fmt.Errorf("lbank requires apiKey and apiSecret")
  			}
  			return nil
  		},
  	},
  }
  ```

- [ ] **Step 5: Run tests and commit config changes**
  Run: `go test ./internal/infrastructure/config/...`
  Commit:
  ```bash
  git add .env.example deploy/k8s/vault-init.sh internal/infrastructure/exchange/types.go internal/infrastructure/config/types.go
  git commit -m "feat: setup lbank credentials configuration and specs"
  ```

---

### Task 2: Configure JSONC Files

**Files:**
- Modify: `configs/funding/local/exchange.jsonc`
- Modify: `configs/funding/prod/exchange.jsonc`
- Modify: `configs/funding/local/blacklist.jsonc`
- Modify: `configs/funding/prod/blacklist.jsonc`
- Modify: `configs/funding/local/reversion.jsonc`
- Modify: `configs/funding/prod/reversion.jsonc`

- [ ] **Step 1: Update local/production `exchange.jsonc`**
  Add the LBank API block:
  ```jsonc
  "lbank": {
    "enable": false,
    "future": {
      "baseURL": "https://lbkperp.lbank.com"
    },
    "websocket": {
      "publicURL": "wss://lbkperp.lbank.com/cfd/ws/v1/pub",
      "privateURL": "wss://lbkperp.lbank.com/cfd/ws/v1/user"
    }
  }
  ```

- [ ] **Step 2: Update local/production `blacklist.jsonc`**
  Add `"lbank": []` entry.

- [ ] **Step 3: Update local/production `reversion.jsonc`**
  Add the reversion parameters:
  ```jsonc
  "lbank": {
    "takeProfitPct": 1,
    "stopLossPct": 2,
    "bufferTime": "30ms",
    "postSettleTimeout": "300s"
  }
  ```

- [ ] **Step 4: Run tests to verify config loading**
  Run: `go test ./internal/infrastructure/config/...`
  Expected: PASS
  Commit:
  ```bash
  git add configs/
  git commit -m "config: add lbank settings for exchange, blacklist, and reversion"
  ```

---

### Task 3: Implement Client Credentials Signer and Headers

**Files:**
- Create: `internal/infrastructure/exchange/lbank/client.go` (Overwrite/extend)

- [ ] **Step 1: Extend Client struct with apiKey and apiSecret**
  Update `client.go` constructor to accept credentials:
  ```go
  type Client struct {
  	httpClient *http.Client
  	baseURL    string
  	apiKey     string
  	apiSecret  string
  	logCfg     config.LoggingConfig
  	logger     *slog.Logger
  	clock      exchange.Clock
  }

  func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
  	logger := slog.Default().With("component", "exchange").With("exchange", "lbank")
  	// Wrap transport with transportlog to redact signature, api_key, etc.
  	// Initialize clock to exchange.RealClock{}
  	// Return &Client
  }
  ```

- [ ] **Step 2: Implement query params sorting, MD5 digest, and HmacSHA256 signature logic**
  Implement private request builder `signRequest` method that adds headers (`signature_method=HmacSHA256`, `timestamp`, `echostr`) and appends the signature to the parameters.

- [ ] **Step 3: Run `make test` to ensure it compiles**
  Expected: Success.
  Commit:
  ```bash
  git add internal/infrastructure/exchange/lbank/client.go
  git commit -m "feat: implement request signature helper in lbank client"
  ```

---

### Task 4: Implement System and Market API Endpoints

**Files:**
- Create: `internal/infrastructure/exchange/lbank/system.go`
- Create: `internal/infrastructure/exchange/lbank/market.go` (Extend/Implement)

- [ ] **Step 1: Implement `system.go` endpoints**
  Create `system.go` and implement:
  * `rawGetServerTime`: hits `GET /cfd/openApi/v1/pub/getTime`
  * `GetServerTime`: maps server time to unix milli
  * `SupportLeverageOnOrder`: returns `false` (LBank leverage is managed out-of-band)
  * `WarmUp`: empty/default ping logic

- [ ] **Step 2: Implement `market.go` endpoints**
  Implement:
  * `GetTickers`: hits `GET /cfd/openApi/v1/pub/marketData`
  * `GetContractDetails`: hits `GET /cfd/openApi/v1/pub/instrument` (parse tickSize, stepSize, multipliers)
  * `GetFundingRates`: queries funding rates

- [ ] **Step 3: Run linter and tests**
  Run: `make lint && go test ./internal/infrastructure/exchange/lbank/...`
  Commit:
  ```bash
  git add internal/infrastructure/exchange/lbank/system.go internal/infrastructure/exchange/lbank/market.go
  git commit -m "feat: implement lbank system and market data endpoints"
  ```

---

### Task 5: Implement Order, Trade, and Position Endpoints

**Files:**
- Create: `internal/infrastructure/exchange/lbank/order.go`
- Create: `internal/infrastructure/exchange/lbank/trade.go`
- Create: `internal/infrastructure/exchange/lbank/position.go`

- [ ] **Step 1: Implement `order.go` endpoints**
  Implement `CreateOrder` (submitting to `/cfd/openApi/v1/trade/placeOrder`), `CancelOrder`, `CancelOrders`, `CancelAllOpenOrders`, `GetOrder`, `GetOrderByExternalID`, `GetOpenOrders`, and `GetOrderPNL` (querying closed position/trade history).

- [ ] **Step 2: Implement `trade.go` endpoints**
  Implement `ChangeLeverage` (adjusting leverage) and `SwitchMarginMode` (adjusting isolated vs cross).

- [ ] **Step 3: Implement `position.go` endpoints**
  Implement `GetOpenPositions`, `ClosePosition` (using market order), and `CloseAllPositions`.

- [ ] **Step 4: Run compilation check**
  Run: `go test ./internal/infrastructure/exchange/lbank/...`
  Commit:
  ```bash
  git add internal/infrastructure/exchange/lbank/order.go internal/infrastructure/exchange/lbank/trade.go internal/infrastructure/exchange/lbank/position.go
  git commit -m "feat: implement lbank order, trade, and position management endpoints"
  ```

---

### Task 6: Implement WebSocket Subscription Adapter

**Files:**
- Create: `internal/infrastructure/exchange/lbank/ws_adapter.go`

- [ ] **Step 1: Implement `ws.ExchangeAdapter` interface**
  Implement:
  * `GetPingConfig`: returns ping interval and payload
  * `GetAuthHook`: signs/sends private WebSocket connection auth handshake
  * `GetChannelExtractor`: maps incoming message keys to `"ticker"` or `"personal.position"`
  * `SubscribeTicker` / `UnsubscribeTicker`
  * `SubscribePersonal`
  * `ParseTicker` (constructs `store.PriceData` using `BestBid`, `BestAsk`, `LastPrice`, `Volume24`)
  * `ParsePosition` (handles `0.0` position volumes as closed updates)
  * `ParseOrder` (parses private order execution updates)

- [ ] **Step 2: Run linter and compile tests**
  Run: `make lint`
  Commit:
  ```bash
  git add internal/infrastructure/exchange/lbank/ws_adapter.go
  git commit -m "feat: implement lbank websocket adapter"
  ```

---

### Task 7: Register in Provider Factory

**Files:**
- Modify: `internal/infrastructure/app/provider_factory.go`

- [ ] **Step 1: Wire LBank into `DefaultProviderFactories()`**
  Instantiate the LBank REST client and WS adapter in the factory registry:
  ```go
  SimpleProviderFactory{
  	name: exchange.ExchangeLbank,
  	buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
  		apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeLbank]
  		client := lbank.NewClient(
  			cfg.HTTPClient,
  			apiCfg.Future.BaseURL,
  			apiCfg.APIKey,
  			apiCfg.APISecret,
  			cfg.SystemConfig.Logging,
  		)
  		adapter := lbank.NewWsAdapter(apiCfg.APIKey, apiCfg.APISecret)
  		return buildProvider(ctx, exchange.ExchangeLbank, exchange.ExchangeLbank, cfg, apiCfg, client, adapter), nil
  	},
  }
  ```

- [ ] **Step 2: Compile the workspace**
  Run: `go test ./internal/infrastructure/app/...`
  Expected: PASS
  Commit:
  ```bash
  git add internal/infrastructure/app/provider_factory.go
  git commit -m "feat: register lbank provider in provider factory"
  ```

---

### Task 8: Test Suite Coverage and Registry Update

**Files:**
- Create: `internal/infrastructure/exchange/lbank/client_test.go`
- Create: `internal/infrastructure/exchange/lbank/ws_adapter_test.go`
- Modify: `docs/tech/exchanges.json`

- [ ] **Step 1: Write `client_test.go`**
  Mock all REST endpoints (`getTime`, `instrument`, `marketData`, `placeOrder`, etc.) using `httptest.NewServer` and assert success and error parsing paths.

- [ ] **Step 2: Write `ws_adapter_test.go`**
  Verify ticker, position, and order updates message parsing.

- [ ] **Step 3: Update `docs/tech/exchanges.json` registry status**
  Mark LBank's `"implementedFundingReversion"` to `true`.

- [ ] **Step 4: Execute full quality gates**
  Run: `make lint` and `make test` and `make cover-check`
  Expected: Success, test coverage remains >= 75%.
  Commit:
  ```bash
  git add .
  git commit -m "test: add comprehensive client and ws_adapter unit tests for lbank"
  ```

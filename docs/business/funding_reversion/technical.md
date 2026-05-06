# Funding Reversion Bot — Technical Reference

Chi tiết kỹ thuật triển khai, cấu trúc dữ liệu, và cấu hình.

---

## GlobalStore — Data Types

```go
type PriceData struct {       // ← WS push (realtime)
    LastPrice, BestBid, BestAsk, FairPrice float64
    UpdatedAt time.Time
}

type TickerData struct {      // ← REST sync (every 5min)
    FundingRate float64
    NextSettleTime int64      // Unix ms
    Volume24, Amount24 float64
    UpdatedAt time.Time
}

type ContractData struct {    // ← REST sync (every 1h)
    PriceUnit float64
    VolUnit, MinVol int
    ContractSize, TakerFeeRate, MakerFeeRate float64
    UpdatedAt time.Time
}

type FundingData struct {     // ← REST sync (every 5min/symbol)
    FundingRate float64
    NextSettleTime int64
    UpdatedAt time.Time
}
```

### Store Interface

```go
store.GetPrice(symbol, maxAge)      → *PriceData
store.GetTicker(symbol)             → *TickerData
store.GetContract(symbol)           → *ContractData
store.GetFunding(symbol)            → *FundingData
store.GetSettleTime(symbol)         → time.Time
store.GetBestBidAsk(symbol)         → bid, ask float64
```

---

## IOC Price Calculation

```go
// CalculateIOCPrice — tính giá IOC dựa trên side
func CalculateIOCPrice(c *Candidate) (float64, error) {
    switch c.Side {
    case SideOpenLong:
        refPrice = c.BestAsk       // mua phải hốt giá bán
        direction = +1             // chấp nhận mua đắt hơn
        snap = math.Floor          // floor để không overshoot
    case SideOpenShort:
        refPrice = c.BestBid       // bán phải vào giá mua
        direction = -1             // chấp nhận bán rẻ hơn
        snap = math.Ceil           // ceil để không undershoot
    }

    slippage = max(refPrice * maxPriceDiffPercent, priceUnit * 2)
    iocPrice = snap((refPrice + direction*slippage) / priceUnit) * priceUnit
    iocPrice = roundToScale(iocPrice, priceScale)
}
```

---

## Volume Calculation

```go
volume = (marginUSDT × leverage) / (contractSize × lastPrice)
volume = floorToScale(volume, volScale)
```

---

## TP/SL Price Calculation

Giá TP/SL được tính từ IOC price và gửi kèm body lệnh entry:

```go
// LONG: giá lên = lãi, giá xuống = lỗ
if side == OpenLong {
    takeProfitPrice = iocPrice × (1 + tpPct)   // e.g. 0.118 × 1.20 = 0.1416
    stopLossPrice   = iocPrice × (1 - slPct)   // e.g. 0.118 × 0.95 = 0.1121
}

// SHORT: giá xuống = lãi, giá lên = lỗ
if side == OpenShort {
    takeProfitPrice = iocPrice × (1 - tpPct)   // e.g. 0.118 × 0.80 = 0.0944
    stopLossPrice   = iocPrice × (1 + slPct)   // e.g. 0.118 × 1.05 = 0.1239
}
```

---

## PnL Calculation (phaseHold)

```go
// LONG: (giá hiện tại - giá vào) / giá vào
if side == OpenLong {
    pnlPct = (currentPrice - entryPrice) / entryPrice
}

// SHORT: (giá vào - giá hiện tại) / giá vào
if side == OpenShort {
    pnlPct = (entryPrice - currentPrice) / entryPrice
}

// Trigger conditions:
if pnlPct >= tpPct   → TAKE_PROFIT
if pnlPct <= -slPct  → STOP_LOSS
if elapsed >= holdDuration → TIMEOUT
```

---

## Safety Evaluation

```go
// Position size
positionSize = marginUSDT × leverage

// Impact ratio — reject if position too large relative to market
impactRatio = positionSize / amount24h
if impactRatio > maxImpactRatio → REJECT

// Minimum volume — reject if margin too small for contract
if volume < minVol → REJECT
```

---

## Order Request Body

### Entry Order (IOC + Server TP/SL)

```json
{
  "symbol": "REQ_USDT",
  "price": 0.118,
  "vol": 100,
  "side": 3,
  "type": 3,
  "openType": 1,
  "positionMode": 2,
  "leverage": 2,
  "externalOid": "so_REQ_USDT_1713534000000",
  "takeProfitPrice": 0.0944,
  "stopLossPrice": 0.1239
}
```

### Close Order (Market)

```json
{
  "symbol": "REQ_USDT",
  "vol": 100,
  "side": 2,
  "type": 5,
  "openType": 1,
  "reduceOnly": true,
  "externalOid": "sc_REQ_USDT_1713534030000"
}
```

---

## WS Order Deal Events

Bot lắng nghe `push.personal.order` cho xác nhận fill:

```go
// States:
// 3 = fully filled
// 4 = canceled
// 5 = partially canceled

type WsOrderDeal struct {
    OrderID      string
    DealAvgPrice float64
    DealVol      float64
    State        int
    TakerFee     float64
    MakerFee     float64
    Profit       float64
}
```

---

## Config Reference

### `system.jsonc` — Global

| Field                           | Type     | Default | Description                         |
| ------------------------------- | -------- | ------- | ----------------------------------- |
| `api.baseURL`                   | string   | —       | MEXC Futures REST endpoint          |
| `api.wsURL`                     | string   | —       | MEXC Futures WebSocket endpoint     |
| `api.timeSyncInterval`          | duration | `60s`   | Server time sync interval           |
| `api.tickerSyncInterval`        | duration | `300s`  | Ticker REST sync interval           |
| `api.contractSyncInterval`      | duration | `3600s` | Contract details sync interval      |
| `api.fundingSyncInterval`       | duration | `300s`  | Funding rate sync interval          |
| `logging.level`                 | string   | `info`  | Log level (debug/info/warn/error)   |
| `logging.console`               | bool     | `true`  | Pretty colored console output       |
| `safety.maxCapitalPctPerSymbol` | float    | `10`    | Max % capital per symbol (10 = 10%) |
| `safety.maxImpactRatio`         | float    | `5`     | Max impact ratio in % (5 = 5%)      |
| `safety.maxLatency`             | duration | `200ms` | Max API round-trip latency          |
| `safety.fireBeforeSettle`       | duration | `300ms` | Fire order N ms before settle       |
| `safety.holdDuration`           | duration | `30s`   | Max time to hold position           |

### `funding.jsonc` — Per-Symbol

| Field                 | Type   | Default    | Description                                   |
| --------------------- | ------ | ---------- | --------------------------------------------- |
| `symbol`              | string | —          | Futures contract symbol                       |
| `simulateSettle`      | string | `""`       | Simulate settle time (RFC3339, empty = live)  |
| `minFundingRate`      | float  | —          | Min \|FR\| to trade (0.3 = 0.3%)              |
| `maxPriceDiffPercent` | float  | —          | Max slippage from best bid/ask (1 = 1%)       |
| `marginUSDT`          | float  | —          | Margin per trade in USDT                      |
| `leverage`            | int    | —          | Leverage multiplier                           |
| `openType`            | string | `ISOLATED` | Margin mode: ISOLATED or CROSS                |
| `positionMode`        | string | `HEDGE`    | Position mode: ONE_WAY or HEDGE               |
| `takeProfitPct`       | float  | `20`       | Take profit % (20 = 20%)                      |
| `stopLossPct`         | float  | `5`        | Stop loss % (5 = 5%)                          |

### `.env` — API Keys

```
MEXC_API_KEY=xxx
MEXC_API_SECRET=xxx
```

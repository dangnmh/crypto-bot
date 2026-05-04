# Logic Tính Giá & Volume

Được thực hiện trong phase `arm`, mục tiêu là tính toán chính xác giá Entry để đặt lệnh IOC và Volume dựa trên tỷ lệ rủi ro. Tính năng Dynamic Pricing cho phép Bot thay đổi mức giá slippage tuỳ theo thanh khoản của Order Book hoặc độ giãn của Spread tại thời điểm giao dịch.

```mermaid
flowchart TD
    START["Bắt đầu tính Giá & Volume"] --> CHECK_SIDE{"Chiều đánh (Side)?"}
    
    CHECK_SIDE -- "LONG" --> L1["Ref Price = BestAsk"]
    L1 --> L2["Direction = +1<br/>(Chấp nhận mua đắt hơn)"]
    L2 --> L3["Snap Func = math.Floor<br/>(Tránh overshoot)"]
    
    CHECK_SIDE -- "SHORT" --> S1["Ref Price = BestBid"]
    S1 --> S2["Direction = -1<br/>(Chấp nhận bán rẻ hơn)"]
    S2 --> S3["Snap Func = math.Ceil<br/>(Tránh undershoot)"]
    
    L3 --> DYN_CHECK
    S3 --> DYN_CHECK
    
    DYN_CHECK{"Dynamic Pricing<br/>Enabled?"}
    
    DYN_CHECK -- "No (Static)" --> STATIC_SLIP["Static Percent<br/>Slippage = RefPrice * maxDiff%"]
    
    DYN_CHECK -- "Yes" --> DYN_MODE{"Slippage Mode"}
    
    DYN_MODE -- "SPREAD_MULTIPLIER" --> DYN_SPREAD["Spread Multiplier<br/>diff = max(maxDiff%, Spread% * Multiplier)<br/>Slippage = RefPrice * diff"]
    
    DYN_MODE -- "OB_IMBALANCE" --> DYN_OB["Orderbook Sweep<br/>Quét OB tới khi đủ Volume<br/>Slippage = |SweepPrice - RefPrice| + Buffer%"]
    
    DYN_OB --> HARD_CAP["Hard Cap (Chống rỗng thanh khoản)<br/>Slippage = min(Slippage, RefPrice * obMaxSlippagePct)"]
    
    STATIC_SLIP --> MIN_TICK
    DYN_SPREAD --> MIN_TICK
    HARD_CAP --> MIN_TICK
    
    MIN_TICK["Bảo vệ Tick Size<br/>Slippage = max(Slippage, 2 * PriceUnit)"] --> CALC_RAW
    
    CALC_RAW["Raw Price = RefPrice + (Direction * Slippage)"] --> SNAP
    SNAP["Tick Snap = SnapFunc(RawPrice / PriceUnit) * PriceUnit"] --> ROUND_PRICE
    ROUND_PRICE["IOC Price = round(TickSnap, priceScale)"] --> CALC_VOL
    
    CALC_VOL["Raw Vol = (Margin * Leverage) / (ContractSize * LastPrice)"] --> ROUND_VOL
    ROUND_VOL["Volume = floor(RawVol, volScale)"] --> DONE["Trả về (IOC Price, Volume)"]

    style START fill:#0f3460,stroke:#e94560,color:#fff
    style DONE fill:#005c2a,stroke:#e94560,color:#fff
```

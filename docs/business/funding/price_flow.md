# Price And Volume Logic

Status: shared primitive.

Logic này được dùng bởi các flow khi cần tính entry price, volume, TP/SL hoặc slippage. Shared scan không nên tính final price cho từng flow; mỗi flow tự gọi pricing ở phase riêng.

Mục tiêu của Reversion là tính IOC price và volume sát thời điểm fire. Trap dùng công thức riêng cho limit price, nhưng vẫn chia sẻ unit convention và rounding/snap rules.

> Percent fields shown in config examples are user-facing percent values: `3` means 3%. Internal `*Pct` formulas use ratios (`0.03`) after config normalization. `maxPriceDiffPercent` is the exception: it remains percent because slippage calculators work in percent units. See [README.md](README.md#percent-unit-convention).

```mermaid
flowchart TD
    START["Bắt đầu tính Giá & Volume"] --> CHECK_SIDE{"Chiều đánh (Side)?"}
    
    CHECK_SIDE -- "LONG" --> L1["Ref Price = BestAsk"]
    L1 --> L2["Direction = +1<br/>(Chấp nhận mua đắt hơn)"]
    L2 --> L3["Snap Func = math.Floor<br/>(Tránh overshoot)"]
    
    CHECK_SIDE -- "SHORT" --> S1["Ref Price = BestBid"]
    S1 --> S2["Direction = -1<br/>(Chấp nhận bán rẻ hơn)"]
    S2 --> S3["Snap Func = math.Ceil<br/>(Tránh undershoot)"]
    
    L3 --> STATIC_SLIP
    S3 --> STATIC_SLIP
    
    STATIC_SLIP["Static Percent<br/>Slippage = RefPrice * maxDiff%"] --> MIN_TICK
    
    MIN_TICK["Bảo vệ Tick Size<br/>Slippage = max(Slippage, 2 * PriceUnit)"] --> CALC_RAW
    
    CALC_RAW["Raw Price = RefPrice + (Direction * Slippage)"] --> SNAP
    SNAP["Tick Snap = SnapFunc(RawPrice / PriceUnit) * PriceUnit"] --> ROUND_PRICE
    ROUND_PRICE["IOC Price = round(TickSnap, priceScale)"] --> CALC_VOL
    
    CALC_VOL["Raw Vol = (Margin * Leverage) / (ContractSize * RefPrice)<br/>RefPrice = BestAsk (LONG) / BestBid (SHORT)"] --> ROUND_VOL
    ROUND_VOL["Volume = floor(RawVol, volScale)"] --> DONE["Trả về (IOC Price, Volume)"]

    style START fill:#0f3460,stroke:#e94560,color:#fff
    style DONE fill:#005c2a,stroke:#e94560,color:#fff
```

---

## Logic Tính Take Profit Price

Được thực hiện trong phase `fire_ioc`. Reversion dùng TP/SL tĩnh từ config, không dùng dynamic FR/ATR, OB sweep, hoặc OB wall cap.

```mermaid
flowchart TD
    START2["Bắt đầu tính TP"] --> ENTRY{"Side?"}

    ENTRY -- "LONG" --> LONG_ENTRY["TP = Entry × (1 + takeProfitPct)<br/>SL = Entry × (1 - stopLossPct)"]
    ENTRY -- "SHORT" --> SHORT_ENTRY["TP = Entry × (1 - takeProfitPct)<br/>SL = Entry × (1 + stopLossPct)"]

    LONG_ENTRY --> TICK["Tick Snap + Scale"]
    SHORT_ENTRY --> TICK

    TICK --> SANITY{"TP đúng phía<br/>so với Entry?"}
    SANITY -- "Đúng" --> TP_DONE["TakeProfitPrice"]
    SANITY -- "Sai" --> TP_ZERO["TP = 0 (bỏ qua)"]

    style START2 fill:#0f3460,stroke:#e94560,color:#fff
    style TP_DONE fill:#005c2a,stroke:#e94560,color:#fff
```

FR vẫn dùng để quyết định hướng và bucket phân tích, nhưng không còn trực tiếp tính TP trong runtime Reversion.

### Required Journal Fields

Price calculation changes are not considered validated until these fields are recorded and reviewed:

| Field | Purpose |
|---|---|
| `ioc_intended_price` | Price submitted by the bot |
| `ioc_fill_price` | Actual exchange fill price |
| `ioc_slippage_pct` | Execution quality versus intended price |
| `best_bid`, `best_ask`, `spread` at fire | Reconstructs the market state used for pricing |
| `volume`, `contract_size`, `ref_price` | Audits position sizing |

### Phối Hợp Đóng Vị Thế

```
Fire IOC (có static TakeProfitPrice + StopLossPrice)
    ↓
IOC Fill → Exchange TP/SL đóng vị thế hoặc timeout force-close
    ↓
Ai chạm trước thì đóng:
  • TP trigger → chốt lời
  • SL trigger → cắt lỗ
  • Timeout → bot force close để tránh giữ stale exposure
```

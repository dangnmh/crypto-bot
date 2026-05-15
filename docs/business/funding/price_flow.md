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
    
    CALC_VOL["Raw Vol = (Margin * Leverage) / (ContractSize * RefPrice)<br/>RefPrice = BestAsk (LONG) / BestBid (SHORT)"] --> ROUND_VOL
    ROUND_VOL["Volume = floor(RawVol, volScale)"] --> DONE["Trả về (IOC Price, Volume)"]

    style START fill:#0f3460,stroke:#e94560,color:#fff
    style DONE fill:#005c2a,stroke:#e94560,color:#fff
```

---

## Logic Tính Take Profit Price

Được thực hiện trong phase `fire_ioc`. Bot tính giá chốt lời tự động dựa trên 2 nguồn: **Dynamic Pricing (FR + ATR)** cho TP chính và **OB wall detection** cho safety cap.

### Dynamic Pricing (TP chính — dựa trên thống kê)

```
TP% = (|FR%| × TpFundingMultiplier) + (ATR% × TpAtrMultiplier)
```

Ví dụ: FR = -0.5%, ATR% = 0.3%, TpFundingMul = 2.0, TpAtrMul = 1.5
→ TP% = (0.5 × 2.0) + (0.3 × 1.5) = **1.45%**

### OB Wall Detection (safety cap — giảm TP nếu có tường chặn)

```mermaid
flowchart TD
    START2["Bắt đầu tính TP"] --> ENTRY{"Side?"}

    ENTRY -- "LONG" --> LONG_ENTRY["Entry = BestAsk<br/>Scan: Ask side<br/>maxTP = Entry × (1 + maxTPPct)"]
    ENTRY -- "SHORT" --> SHORT_ENTRY["Entry = BestBid<br/>Scan: Bid side<br/>maxTP = Entry × (1 - maxTPPct)"]

    LONG_ENTRY --> SCAN
    SHORT_ENTRY --> SCAN

    SCAN["Quét OB levels<br/>Tìm level có Vol ≥ 3× avg"] --> WALL{Tìm thấy Wall?}

    WALL -- "Có" --> SNAP["TP = Wall Price ± 2 ticks<br/>(đặt trước tường)"]
    SNAP --> CLAMP["Clamp: TP ≤ maxTP"]

    WALL -- "Không" --> FALLBACK["TP = maxTP<br/>(fallback FR×mul + ATR)"]

    CLAMP --> TICK["Tick Snap + Scale"]
    FALLBACK --> TICK

    TICK --> SANITY{"TP đúng phía<br/>so với Entry?"}
    SANITY -- "Đúng" --> TP_DONE["TakeProfitPrice"]
    SANITY -- "Sai" --> TP_ZERO["TP = 0 (bỏ qua)"]

    style START2 fill:#0f3460,stroke:#e94560,color:#fff
    style TP_DONE fill:#005c2a,stroke:#e94560,color:#fff
```

> Lưu ý: OB trước settle có thể biến mất sau settle. TP từ wall chỉ là safety cap: chỉ giảm TP, không tăng. Xem [depth.md](depth.md).

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
Fire IOC (có TakeProfitPrice = safety cap)
    ↓
IOC Fill → Đặt Trailing Stop (cưỡi sóng)
    ↓
Ai chạm trước thì đóng:
  • Trailing đóng trước → ăn đậm (sóng mạnh)
  • TP trigger đóng trước → safety net (sóng yếu/chạm wall)
```

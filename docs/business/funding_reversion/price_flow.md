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
    SANITY -- "Đúng" --> TP_DONE["TakeProfitPrice ✅"]
    SANITY -- "Sai" --> TP_ZERO["TP = 0 (bỏ qua)"]

    style START2 fill:#0f3460,stroke:#e94560,color:#fff
    style TP_DONE fill:#005c2a,stroke:#e94560,color:#fff
```

> ⚠️ **Lưu ý:** OB trước settle là "sổ lệnh ma" — wall có thể biến mất sau settle. TP từ wall chỉ là **safety cap** (chỉ giảm TP, không tăng). Xem `depth.md §3.4`.

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


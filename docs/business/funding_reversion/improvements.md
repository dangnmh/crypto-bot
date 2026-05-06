# Funding Reversion — Future Improvements Roadmap

> Tài liệu tổng hợp các cải tiến chiến thuật đã phân tích, sắp xếp theo độ ưu tiên.

---

## P0 — Trailing Dead Zone Fix

**Vấn đề:** PnL từ 0% đến `activationPct` không có cơ chế chốt lời nào.
- TP cứng quá xa (FR × 2.0 — safety net)
- Trailing chưa bật (chưa đạt activation)
- Giá revert 0.8% rồi quay đầu → từ lãi thành lỗ

**Giải pháp:** Đã implement FR-Dynamic `activationPct` (FR × 1.5, clamp [0.2%, 3.0%]).
Khi FR thấp (0.3%), activation giảm xuống 0.45% thay vì cố định 1.0% → trailing bật sớm hơn.

**Trạng thái:** ✅ Đã implement (FR-Dynamic Parameters)

---

## P0 — Circuit Breaker (Global Risk Management)

**Vấn đề:** Multi-symbol fire cùng lúc, không có giới hạn rủi ro toàn cục.
Khi scale vốn lên → rủi ro cháy tài khoản nếu nhiều coin thua cùng lúc.

**Giải pháp cần implement:**
- `TotalMarginExposure` cap: tổng margin đang mở không vượt X% balance
- `DailyPnL` stop: thua Y% trong ngày → dừng bot
- `ConcurrentPositions` limit: tối đa N vị thế cùng lúc

**Trạng thái:** ❌ Chưa implement

---

## P1 — Pre-Settle Entry Mode (Chấp nhận trả Funding Fee)

### Bối cảnh

Hiện tại bot vào SAU settle (IOC ở T-10ms). Với coin có FR thấp (0.2-0.5%), slippage + missed move ăn hết lợi nhuận. Nếu vào TRƯỚC settle bằng Limit Order → giá entry tốt hơn nhiều, dù phải trả FR.

### Công thức PnL khi dính Funding Fee

```
Net% = ΔP% - |FR%| - 2 × TakerFee%

Trong đó:
  ΔP% = (EntryPrice - ExitPrice) / EntryPrice × 100  (SHORT)
  FR%  = Funding Rate phải trả
  TakerFee% = ~0.02% mỗi chiều (MEXC)
```

### Breakeven

```
Min ΔP% = |FR%| + 2 × fee%

VD: FR = 0.5% → giá cần move ≥ 0.54% để hòa vốn
```

### Bảng Breakeven nhanh

| FR    | Min Move để hòa | Muốn lời 0.5% | Muốn lời 1% |
|:-----:|:----------------:|:--------------:|:------------:|
| 0.15% | 0.19%            | 0.69%          | 1.19%        |
| 0.30% | 0.34%            | 0.84%          | 1.34%        |
| 0.50% | 0.54%            | 1.04%          | 1.54%        |
| 0.70% | 0.74%            | 1.24%          | 1.74%        |
| 1.00% | 1.04%            | 1.54%          | 2.04%        |

### So sánh PRE vs POST settle

```
EstNet_PRE  = |FR| × (R - 1) - s₁ - 2×fee
EstNet_POST = |FR| × R - m - s₂ - 2×fee

Với:
  R  = Reversion Multiplier (~2x, cần historical data)
  s₁ = Slippage trước settle  ≈ 0.05% (Limit)
  s₂ = Slippage sau settle   ≈ 0.35% (IOC)
  m  = Missed move            ≈ 0.2%
```

### Auto-Switch Logic

```
PRE tốt hơn khi:  |FR| < m + (s₂ - s₁) ≈ 0.5%
POST tốt hơn khi: |FR| ≥ 0.5%
```

### Timing tối ưu

| T-N  | Đánh giá |
|------|----------|
| T-5m | ❌ Quá sớm, rủi ro FR flip |
| T-60s | ⚠️ OK nhưng vốn khóa lâu |
| **T-30s** | **✅ Sweet spot — OB dày, spread hẹp, fill nhanh** |
| T-10s | ⚠️ Sniper bắt đầu move |

### Implementation Plan

1. Thêm `preSettleThreshold` vào config (mặc định 0.5%)
2. Tại `recheck` (T-2s): tính `EstNet_PRE` vs `EstNet_POST`
3. Nếu PRE tốt hơn → chuyển sang Limit Order flow ở T-30s
4. Nếu Limit không fill trong 20s → cancel + fallback IOC ở T-10ms
5. Hạ `minFundingRate` xuống 0.15% cho mode PRE (mở rộng vùng đánh)

**Trạng thái:** ❌ Chưa implement — cần historical data (P2) trước

---

## P2 — Historical Settle Data Collection (SettleRecorder)

### Vấn đề

Biến `R` (Reversion Multiplier) là yếu tố quyết định cho mọi tính toán EV nhưng hiện tại chỉ là số đoán (R=2.0). R thực tế dao động từ 0.5x (BTC) đến 5x+ (shitcoin thanh khoản mỏng).

### Data cần thu thập (mỗi kỳ settle, mỗi symbol)

```go
type SettleRecord struct {
    Symbol      string
    SettleTime  time.Time
    FundingRate float64

    // Prices
    PriceAtT0   float64 // Mark Price tại settle
    PriceAtT10s float64
    PriceAtT30s float64
    PriceAtT60s float64

    // Derived
    MaxMove     float64 // Max |ΔP%| trong 60s đầu
    R           float64 // MaxMove% / |FR%|

    // Context
    Volume24h   float64
    Spread      float64 // Bid-Ask spread tại T-1s
}
```

### Cách collect (passive, không trade)

Bot đã subscribe WS ticker. Thêm 1 goroutine per settle:
1. Ghi `PriceAtT0` từ ticker tại T
2. Ghi giá tại T+10s, T+30s, T+60s
3. Tính `MaxMove`, `R`
4. Append vào JSON/CSV file

### Sau 1-2 tuần (3 settle/ngày × 800+ coins)

- Tính `R_avg` per-coin (rolling 20 kỳ)
- Loại coin có R < 1.5 (reversion yếu)
- Auto-tune `minFundingRate` per-coin dựa trên breakeven thực

**Trạng thái:** ❌ Chưa implement

---

## P2 — Fire Timing Optimization

**Vấn đề:** `fireBeforeSettle: 10ms` chưa tính latency thực.
Với latency ~80ms từ VN, lệnh đến MEXC ở T+70ms → không "đón đầu" mà "chạy theo".

**Giải pháp:**
```
fireBeforeSettle = measuredLatency + bufferMs
                 = ts.Offset() + 20ms
```
Dùng `timesync.Offset()` đo latency thực, cộng buffer 20ms.

**Trạng thái:** ❌ Chưa implement

---

## P3 — Scorer Risk Penalty

**Vấn đề:** `CoinScore = ExpectedProfit × LiquidityScore` chưa tính yếu tố rủi ro.
Coin spread rộng hoặc OB mỏng có thể rank cao nhưng risk cũng cao.

**Giải pháp:**
```
RiskPenalty = f(spread_width, ob_depth, R_historical)
AdjustedScore = CoinScore × (1 - RiskPenalty)
```

**Trạng thái:** ❌ Chưa implement

---

## Dependency Map

```
P0 Circuit Breaker ──────────────────────────── (độc lập, làm ngay)
P2 SettleRecorder ──→ P1 Pre-Settle Mode ──→ P3 Scorer Risk Penalty
                  └──→ P2 Fire Timing
```

> **Recommended order:**
> 1. ✅ FR-Dynamic Parameters (done)
> 2. Circuit Breaker (P0)
> 3. SettleRecorder (P2) — passive, không ảnh hưởng trading
> 4. Pre-Settle Mode (P1) — sau khi có data từ SettleRecorder
> 5. Fire Timing + Scorer (P2/P3)

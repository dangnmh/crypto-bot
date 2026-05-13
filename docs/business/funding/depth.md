# Phân Tích Orderbook (Depth) — Logic Đã Implement

Tài liệu này mô tả Business Logic **đã triển khai trong code** cho việc sử dụng Orderbook trong chiến lược Funding Reversion trên **altcoin thanh khoản thấp** (MEXC Futures). Quy mô vị thế: **$1,000–1,500 × x20 leverage = $20k–30k notional**.

> Các ý tưởng lý thuyết chưa implement (Imbalance Ratio, co giãn TP/SL theo OB, multi-snapshot protocol...) đã được chuyển sang [backlog.md](backlog.md).

---

## 1. Bối Cảnh Chiến Lược

### 1.1 Funding Reversion — Tóm Tắt Cơ Chế

Funding Rate (FR) cho biết bên nào đang **trả phí** để giữ vị thế. **Funding Snipers** là những người mở vị thế trước settle để **nhận phí**, sau đó đóng lệnh ngay → tạo sóng giá.

| FR | Snipers làm gì? | Sau Settle (T+0) | Sóng giá | Ta vào lệnh |
|---|---|---|---|---|
| **FR > 0** (dương lớn) | Snipers mở **SHORT** để nhận phí từ phe Long | Snipers **buy-to-close** SHORT → lực mua đẩy giá | **PUMP** ↑ | **LONG** ngay sau settle |
| **FR < 0** (âm lớn) | Snipers mở **LONG** để nhận phí từ phe Short | Snipers **sell-to-close** LONG → lực bán đè giá | **DUMP** ↓ | **SHORT** ngay sau settle |

FR quyết định **hướng đi** — ta biết nên Long hay Short. **Vào lệnh ngay sau thời điểm settle** để ăn theo sóng do snipers đóng vị thế.

### 1.2 FR Là Chưa Đủ — Tại Sao Cần Orderbook?

FR chỉ cho ta biết **"Hướng Gió"** chứ không cho ta biết **"Đường đi có đá tảng hay không"**.

Ví dụ thực tế:
- FR = +0.3% trên KSM_USDT → nên LONG
- Nhưng ngay trên giá hiện tại có 1 tường Ask khổng lồ (5,000 contracts tại $18.50 trong khi 24h volume chỉ 50,000 contracts)
- Dù phe Short close position → lực mua chỉ đẩy giá lên đến $18.49 rồi bị tường chặn
- TP đặt ở +0.5% = $18.59 → **không bao giờ chạm** → gồng lời vô nghĩa rồi bị dội xuống

**Trong code hiện tại, OB được dùng cho 2 mục đích chính:**

1. **Slippage IOC** (chính xác) → Sweep OB để tính giá vào lệnh thực tế.
2. **TP wall detection** (safety cap) → Tìm tường chặn để giảm TP nếu cần.

---

## 2. ⚠️ Hạn Chế Nghiêm Trọng: OB Trước Settle Là "Sổ Lệnh Ma"

> **Cảnh báo quan trọng:** OB snapshot lấy trước settle (T-2s, T-5s) **KHÔNG đáng tin** để dự đoán giá sẽ dừng ở đâu **SAU settle**.

**Lý do:**

Đa số trader (bao gồm Snipers) đã **close position trước giờ Funding** để né phí. Điều này tạo ra chuỗi sự kiện:

```
T-30 phút:  Trader bắt đầu close position → rút Bid/Ask orders
T-5 phút:   OB mỏng đáng kể — cả Bid lẫn Ask đều "skeleton"
T-2 giây:   ← Bot snapshot OB tại đây (sổ lệnh ma)
T=0:        Settle → Snipers xả → DUMP/PUMP
T+1~5s:     Giá rơi/bay TỰ DO vì OB đã bị rút trước đó
T+5~15s:    Trader mới quay lại → OB "tái sinh" với wall MỚI
```

**Hệ quả thực tế:**

| Vấn đề | Ví dụ |
|---|---|
| Wall biến mất | Bot thấy Bid Wall $17.85 ở T-2s → Wall bị rút ở T+0.5s → giá rơi thẳng qua |
| Wall giả (pre-settle) | Market Maker đặt wall to ở T-5s để manipulate sentiment → rút ngay khi settle |
| Wall mới xuất hiện | Trader mới vào đặt Bid $17.50 ở T+3s (wall này không tồn tại ở T-2s) |

**Kết luận về vai trò OB:**

| Mục đích | OB pre-settle đáng tin? | Lý do |
|---|---|---|
| **Slippage IOC** (giá vào lệnh) | ✅ Rất tin cậy | OB tại T-2s phản ánh đúng thanh khoản **ngay lúc bắn IOC** |
| **TP wall detection** (giá chốt lời) | ⚠️ Rủi ro cao | Wall ở T-2s **không đại diện** cho OB ở T+2s khi giá chạm TP |
| **Imbalance Ratio** (xu hướng) | 🔶 Tham khảo | Cho biết sentiment chung, nhưng mức dump/pump phụ thuộc volume xả Snipers |
| **Trap placement** (neo bẫy) | ⚠️ Rủi ro cao | Tường neo Trap có thể biến mất — xem [backlog.md §3.4](backlog.md) |

> **Nguyên tắc:** OB dùng cho **slippage IOC = chính xác**. OB dùng cho **TP/Trap = chỉ là safety cap, không phải primary signal**. TP chính nên dựa vào FR × multiplier + ATR (xem §5).

---

## 3. Đặc Thù Altcoin Thanh Khoản Thấp

Coin ta trade có profile đặc biệt cần lưu ý:

| Đặc điểm | Ảnh hưởng | Ứng xử |
|---|---|---|
| **24h volume thấp** (< $5M) | Position $20-30k notional = 0.4–0.6% daily volume → Impact đáng kể | Check impact ratio < config threshold |
| **Spread rộng** (0.3–1%) | IOC bị trượt giá nhiều hơn BTC/ETH | Slippage buffer phải tính từ OB sweep, không phải % cố định |
| **OB mỏng** (5–10 levels thật) | 20 levels depth đã cover 3–8% price range | Đủ cho chiến lược — không cần depth 50+ levels |
| **Spoofing chiếm tỷ trọng lớn** | 1 bot Market Maker có thể tạo/xoá tường trong ms | Dùng Persistence Rule (Wall > 10-15s mới tin) |
| **Sóng settle cực mạnh** | FR 0.3% trên altcoin tạo wick 2–5% (vs BTC chỉ 0.1–0.3%) | TP/SL range phải tương xứng — altcoin cho phép TP lớn hơn |
| **Thanh khoản hồi yếu** | Sóng hồi sau settle có thể chậm (5–15s) vs BTC (1–3s) | Trap cần patience — timeout dài hơn trước khi coi là fail |

---

## 4. Tổng Kết — Bộ Ba Quyết Định

```
┌─────────────────────────────────────────────────────┐
│              FUNDING REVERSION ENGINE                │
│                                                     │
│  ① FR → HƯỚNG ĐI (Long hay Short?)                 │
│     FR > 0 → LONG    FR < 0 → SHORT                │
│                                                     │
│  ② FR × Multiplier + ATR → TP/SL CHÍNH             │
│     Dựa trên thống kê, không phụ thuộc OB           │
│     (Xem §5 — Dynamic Pricing)                      │
│                                                     │
│  ③ OB → SLIPPAGE IOC (chính xác tại T-2s)          │
│     Sweep asks/bids để tính giá vào lệnh            │
│     Wall detection = safety cap cho TP (phụ)        │
│                                                     │
│  ④ TRAILING STOP → ĐÓNG VỊ THẾ (cưỡi sóng)        │
│     Sàn tự theo dõi đỉnh/đáy → đóng khi quay đầu   │
│     Không cần đoán giá dừng ở đâu                   │
└─────────────────────────────────────────────────────┘
```

---

## 5. Cơ Chế Đóng Vị Thế — So Sánh Các Phương Pháp

Bot có 3 cách để đóng vị thế sau khi IOC fill. Mỗi cách phù hợp cho tình huống khác nhau.

### 5.1 ATR là gì?

**ATR (Average True Range)** đo **biên độ dao động trung bình** của giá trong N nến gần nhất.

```
Nến 1: Low=99, High=101 → Biên độ = 2
Nến 2: Low=98, High=103 → Biên độ = 5
Nến 3: Low=100, High=102 → Biên độ = 2
ATR(3) = (2 + 5 + 2) / 3 = 3.0
→ "Coin này thường dao động ±3 đô mỗi phút"
```

ATR giúp bot **tự điều chỉnh** theo từng coin:
- Coin biên độ lớn (ATR cao) → TP/SL rộng hơn
- Coin ít biến động (ATR thấp) → TP/SL hẹp hơn

### 5.2 Dynamic Pricing: TP/SL dựa trên FR + ATR

Công thức trong code (`PrepareDynamicPricing`):

```
TP = (|FR%| × TpFundingMultiplier) + (ATR% × TpAtrMultiplier)
SL = max(|FR%| × SlFundingMultiplier, ATR% × SlAtrMultiplier)
```

Ví dụ:
```
Funding Rate = -0.5%,  ATR% = 0.3%
TpFundingMultiplier = 2.0,  TpAtrMultiplier = 1.5

TP = (0.5 × 2.0) + (0.3 × 1.5) = 1.0 + 0.45 = 1.45%
→ "FR mạnh + coin biên độ trung bình → TP 1.45%"
```

**Ưu điểm:** Dựa trên **thống kê lịch sử**, không phụ thuộc OB skeleton trước settle.

### 5.3 Ba Cơ Chế Đóng Vị Thế

| # | Cơ chế | Loại lệnh MEXC | Cách hoạt động | Khi nào dùng |
|---|---|---|---|---|
| 1 | **TP cố định (Trigger Market)** | `takeProfitPrice` trong IOC request | Sàn tự bán khi giá chạm mức X | Safety cap — luôn đặt để phòng trailing fail |
| 2 | **Trailing Stop (TrackOrder)** | `POST /trackorder/place` | Sàn theo dõi đỉnh/đáy, đóng khi giá quay đầu X% | **Phương pháp chính** — cưỡi sóng tối đa |
| 3 | **Manual close** | Không có lệnh tự động | Trader tự đóng | Khi trailing bị tắt trong config |

### 5.4 Trailing Stop — Giải Thích Chi Tiết

Trailing Stop **KHÔNG đặt giá chốt lời cố định**. Nó "cưỡi sóng" và chỉ đóng khi giá **quay đầu**.

```
Vào SHORT tại $18.00, giá dump:

TP cố định = $17.50:
  $18.00 → $17.80 → $17.50 ← CHỐT! Thu $0.50
  (giá tiếp tục rơi xuống $17.00... nhưng đã ra rồi 😭)

Trailing Stop (activation=1%, callback=0.5%):
  $18.00 → $17.82 ← Trailing kích hoạt (giá rơi 1%)
  $17.82 → $17.50 → $17.20 → $17.00 ← Đáy! Trailing theo sát
  $17.00 → $17.09 ← Giá tăng 0.5% từ đáy → CHỐT! Thu $1.00
```

Tham số:
- **ActivationPct**: Giá phải di chuyển ít nhất X% theo hướng có lời thì Trailing mới bắt đầu theo dõi
- **CallbackPct**: Khi giá quay đầu X% từ đỉnh/đáy → đóng lệnh

> 💡 **Trailing chạy trên server MEXC**, không phải bot. Nếu bot crash, sàn vẫn tự đóng lệnh.

### 5.5 Phối Hợp Giữa Các Cơ Chế

Trong thực tế, bot **kết hợp cả TP trigger lẫn Trailing**:

```
1. Fire IOC với TakeProfitPrice = safety cap (từ OB wall hoặc maxTP%)
2. IOC fill → đặt TrailingStop (cưỡi sóng)
3. Ai chạm trước thì đóng:
   - Trailing đóng trước (sóng mạnh, ăn đậm) ← Trường hợp thường gặp
   - TP trigger đóng trước (sóng yếu, chạm wall) ← Safety net
```

> **Tại sao cần cả hai?** Trailing là primary (tối ưu profit), TP trigger là safety net (phòng trailing không kích hoạt kịp hoặc sóng quá ngắn).

**Funding Rate** là la bàn chỉ hướng. **ATR** là thước đo biên độ. **Trailing Stop** là tay lái tự động. **Orderbook** là bản đồ thanh khoản tức thì (cho slippage IOC). Kết hợp cả bốn, bot vừa biết **đánh hướng nào**, vừa biết **vào giá bao nhiêu**, vừa tự **cưỡi sóng** mà không cần đoán giá dừng ở đâu.

---

## Tài Liệu Liên Quan

| Tài liệu | Nội dung |
|-----------|---------| 
| [backlog.md](backlog.md) | Lý thuyết chưa implement: Imbalance Ratio, co giãn TP/SL, neo tường OB |
| [flow.md](flow.md) | Tổng quan các luồng giao dịch |
| [reversion_flow.md](reversion_flow.md) | Luồng Reversion (IOC + Trailing) |
| [trap_flow.md](trap_flow.md) | Luồng Straddle Trap (Limit + Trailing) |
| [price_flow.md](price_flow.md) | Logic tính IOC Price & Volume |

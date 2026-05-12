# Phân Tích Lực Long/Short Qua Orderbook (Depth)

Tài liệu này trình bày Business Logic của việc áp dụng dữ liệu sổ lệnh (Orderbook/Depth) để tối ưu hoá chiến lược Funding Reversion trên **altcoin thanh khoản thấp** (MEXC Futures). Quy mô vị thế: **$1,000–1,500 × x20 leverage = $20k–30k notional**.

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

> ⚠️ **Lưu ý:** Các khái niệm lý thuyết dưới đây (Imbalance Ratio, TP/SL điều chỉnh linh hoạt, OB-based Trap neo tường) là **phân tích tham khảo**, chưa được triển khai đầy đủ trong code. Xem ghi chú `[LÝ THUYẾT]` tại từng section.

---

## 2. [LÝ THUYẾT] Đọc Orderbook — Lực Long/Short (Imbalance)

### 2.1 Cách Đo

Quét tổng volume Bid vs Ask trong một **biên độ giá gần** (2–3% quanh giá hiện tại):

- **Lực Đỡ (Bid Pressure):** Tổng volume bên Bid — lực sẵn sàng mua nếu giá giảm
- **Lực Đè (Ask Pressure):** Tổng volume bên Ask — lực sẵn sàng bán nếu giá tăng

```
Imbalance Ratio = Tổng Bid Volume / Tổng Ask Volume
```

### 2.2 Đọc Vị Kết Quả

| Imbalance Ratio | Ý nghĩa | Hành động |
|---|---|---|
| **> 2.0** | Bid áp đảo → "Sàn bê tông" bên dưới, bên trên trống | Thuận lợi cho LONG. TP có thể dãn rộng, SL bóp sát dưới Bid Wall |
| **1.5 – 2.0** | Bid nhỉnh hơn → Hơi nghiêng phe mua | Thuận lợi nhẹ cho LONG. Giữ nguyên TP/SL |
| **0.7 – 1.5** | Cân bằng → Giằng co, phụ thuộc Market Order lúc settle | Trung lập. Dùng TP/SL tĩnh từ config |
| **0.5 – 0.7** | Ask nhỉnh hơn → Hơi nghiêng phe bán | Thuận lợi nhẹ cho SHORT |
| **< 0.5** | Ask áp đảo → "Trần sắt" bên trên, bên dưới trống | Thuận lợi cho SHORT. TP có thể dãn rộng |

### 2.3 Cảnh Báo: Spoofing Trên Altcoin

> **⚠️ Altcoin thanh khoản thấp trên MEXC Futures bị spoofing rất nặng.**

Spoofing = đặt lệnh giả khối lượng lớn rồi rút ngay khi giá tiến gần. Trên altcoin vol thấp, 1 Market Maker đơn lẻ có thể tạo ra "tường ảo" chiếm 80% OB.

**Nguyên tắc lọc Spoofing:**

| Quy tắc | Lý do |
|---|---|
| **Chỉ tin 3–5 levels gần nhất** | Sát giá → risk cao nếu spoof → ít bị giả hơn |
| **Wall phải tồn tại > 10–15 giây** | Wall thật thường "dính" lâu vì có mục đích tích luỹ. Wall spoofing thường nhấp nháy |
| **So sánh tương đối, không tuyệt đối** | 1,000 contracts trên coin có 24h vol = 10,000 → Wall thật (10%). 1,000 contracts trên coin 24h vol = 1,000,000 → dust |

**Khi không chắc chắn OB đáng tin → fallback về TP/SL/Trap tĩnh từ config.** Không bao giờ "đoán" dựa trên data nghi ngờ.

---

## 3. [LÝ THUYẾT] Điều Chỉnh TP & SL Linh Hoạt

> ⚠️ **Chưa triển khai trong code.** Code hiện tại chỉ dùng OB wall detection làm **safety cap** cho TP (giảm TP nếu có tường chặn gần). Không có logic co giãn TP/SL dựa trên Imbalance Ratio.

Dựa vào OB Imbalance + vị trí Wall, ta co giãn TP/SL thay vì dùng % cố định.

### 3.1 Khi Đang LONG (FR dương → đón sóng PUMP)

**Mục tiêu: giá bay lên → ta chốt lời ở điểm tối ưu.**

#### Tình huống A: Đường trống phía trên (Ask mỏng, Bid dày)

```
Giá hiện tại: $18.00
Ask Wall gần nhất: KHÔNG CÓ trong 20 levels (tức ~$18.50)
Bid Wall lớn: $17.80 (2,500 contracts)

→ TP: Dãn rộng x1.5–x2 (VD: từ +0.3% lên +0.5%)
→ SL: Đặt ngay dưới Bid Wall $17.80 → SL = $17.78
   Lý do: Nếu giá giảm xuống $17.80, tường Bid sẽ "đỡ" trước. 
   Nếu thủng tường = lực bán quá mạnh, nên chạy.
```

#### Tình huống B: Tường Ask chặn ngay trên đầu

```
Giá hiện tại: $18.00  
Ask Wall lớn: $18.15 (4,000 contracts — chiếm 15% 24h volume)
Bid Wall: Không rõ ràng

→ TP: Bóp ngắn ngay dưới tường $18.15 → TP = $18.13
   Lý do: Dù PUMP mạnh, đập vào 4,000 contracts giá dội ngược.
   Gồng qua tường = tham lam = bị dội SL.
→ SL: Giữ SL tĩnh từ config (vì không có Bid Wall rõ ràng để neo)
```

### 3.2 Khi Đang SHORT (FR âm → đón sóng DUMP)

*Áp dụng ngược lại hoàn toàn:*

| Tình huống | Ask (trên) | Bid (dưới) | Hành động |
|---|---|---|---|
| Đường trống bên dưới | Có Wall | Không có Wall | TP dãn rộng (rơi tự do). SL neo trên Ask Wall |
| Bid Wall chặn bên dưới | Không rõ | Wall lớn | TP bóp sát trên Bid Wall. SL tĩnh |

### 3.3 Quy Tắc An Toàn

> **Hard limit: TP/SL điều chỉnh KHÔNG BAO GIỜ vượt quá giới hạn config.**

Ví dụ:
- Config: maxTP = 1%, maxSL = 0.5%
- OB gợi ý TP = 1.5% vì đường trống → **Bóp lại về maxTP = 1%**
- OB gợi ý SL = 0.8% vì Wall quá xa → **Bóp lại về maxSL = 0.5%**

Đây là "safety rail" — OB chỉ được phép **tối ưu trong vùng an toàn**, không được phá vỡ risk management.

### 3.4 ⚠️ Hạn Chế Nghiêm Trọng: OB Trước Settle Là "Sổ Lệnh Ma"

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
| **Trap placement** (neo bẫy) | ⚠️ Rủi ro cao | Tường neo Trap có thể biến mất — xem §4.5 |

> **Nguyên tắc:** OB dùng cho **slippage IOC = chính xác**. OB dùng cho **TP/Trap = chỉ là safety cap, không phải primary signal**. TP chính nên dựa vào FR × multiplier + ATR (xem §8).

---

## 4. [LÝ THUYẾT] Chiến Lược Đặt Bẫy (Trap) Dựa Vào Orderbook

> ⚠️ **Tham khảo.** Code hiện tại dùng FR × multiplier (Dynamic Pricing) làm primary cho Trap depth. OB wall detection chỉ dùng làm fallback/safety. Xem [trap_flow.md](trap_flow.md) cho logic thực tế.

### 4.1 Bối Cảnh: Tại Sao Cần Trap?

Sau sóng chính (PUMP hoặc DUMP lúc settle), giá thường **dội ngược mạnh** tạo ra râu nến (wick). Trap là lệnh Limit đặt sẵn để "bắt" đúng đỉnh/đáy của râu nến này.

**Trap cũ (% cố định):** Đặt limit cách giá hiện tại 3% → may rủi.

**Trap mới (neo OB):** Đọc OB để biết **giá dội ngược sẽ dừng ở đâu**, rồi neo Trap vào điểm đó.

### 4.2 Cơ Chế Sóng & Sóng Hồi Sau Settle

```
Timeline cho FR dương (LONG):

T±0:     Settle xảy ra → Funding fee được tính
T+0~2s:  Snipers buy-to-close SHORT → lực mua đẩy giá → PUMP ↑↑↑
         Bot fire IOC LONG ngay lúc này để ăn theo sóng PUMP
T+2-5s:  Đà PUMP hết → Giá bắt đầu hồi ngược ↓ (Sóng hồi)
T+5-15s: Sóng hồi tạo ra râu nến (wick) ↓
         Giá rơi xuống đến đâu? → ĐẾN KHI GẶP TƯỜNG BID
T+15s+:  Giá nảy lên lại từ tường Bid → Trap bắt được entry đẹp
```

### 4.3 Nghệ Thuật Neo Tường — Quy Trình Cụ Thể

**Bước 1: Tìm "Khoảng Trống" (Liquidity Void)**

Quét OB phía ngược hướng trade chính. Nếu volume mỗi level rất mỏng → đó là Khoảng Trống. Giá sẽ trượt nhanh qua vùng này khi có sóng hồi.

```
Ví dụ: Ta đang LONG (FR dương). Sóng hồi = giá giảm.
Quét Bid side:
  $18.00 (200 contracts) ← giá hiện tại
  $17.98 (50 contracts)  ← mỏng
  $17.95 (30 contracts)  ← mỏng  ← KHOẢNG TRỐNG
  $17.90 (80 contracts)  ← mỏng
  $17.85 (3,500 contracts) ← TƯỜNG BID 🧱
  $17.80 (2,000 contracts) ← dày
```

**Bước 2: Xác Định "Tường Dội" (Liquidity Cluster)**

Tường = level có volume **≥ 3× average volume per level**.

```
Average = (200 + 50 + 30 + 80 + 3500 + 2000) / 6 = 977
Threshold = 977 × 3 = 2,931
→ $17.85 (3,500) → PASS ✅ = Tường Dội
→ $17.80 (2,000) → FAIL ❌ (nhưng cùng cụm)
```

**Bước 3: Đặt Trap Ngay Trước Tường**

```
Tường Dội: $17.85
Trap (Limit BUY ngược chiều): $17.86 (1 tick trước tường)

Lý do: Sóng hồi lao xuống qua khoảng trống → đập vào tường $17.85 → 
dội lên → Trap $17.87 khớp TRƯỚC tường → ta ăn full sóng nảy.
```

**Bước 4: TP/SL cho Trap**

```
Trap Entry: $17.87
Trap TP: $18.02 (quay lại gần giá ban đầu → sóng nảy thường hồi 50–80%)
Trap SL: $17.82 (bên dưới tường — nếu thủng tường = lực quá mạnh, chạy)
```

### 4.4 Khi Trap KHÔNG NÊN Đặt

| Tình huống | Lý do | Hành động |
|---|---|---|
| Không tìm thấy tường rõ ràng | Không biết giá dội ở đâu → đoán mò | **Skip Trap** hoặc fallback % cố định |
| Tường quá xa (> 5% từ giá hiện tại) | Risk/Reward kém, SL quá rộng | **Skip Trap** |
| Tường quá sát (< 0.5% từ giá hiện tại) | Sóng hồi quá ngắn, profit không đáng | **Skip Trap** |
| Tường nhấp nháy (xuất hiện < 10s) | Khả năng cao là Spoofing | **Skip Trap** — không tin Wall ảo |
| Imbalance Ratio ∈ [0.7, 1.5] | Lực 2 bên cân bằng → sóng hồi yếu, khó đoán | **Skip Trap** hoặc giảm size 50% |

### 4.5 Rủi Ro "Tường Giấy"

> **⚠️ Nguy hiểm lớn nhất của Trap neo tường: tường biến mất lúc settle.**

Kịch bản xấu:
1. T-10s: Thấy Bid Wall lớn tại $17.85 → neo Trap tại $17.87
2. T±0: Settle → PUMP lên $18.20
3. T+3s: Sóng hồi bắt đầu → giá rơi
4. T+4s: **Bid Wall $17.85 bị rút** (spoofing hoặc panic cancel)
5. T+5s: Giá rơi thẳng qua $17.85 → Trap fill tại $17.87 → giá tiếp tục rơi → **kẹt đáy**

**Biện pháp giảm thiểu:**
- **Trailing SL trên Trap:** Nếu Trap fill mà giá không nảy lên trong 3–5 giây → cắt lỗ ngay
- **Position size Trap nhỏ hơn IOC:** VD: IOC = $1,000, Trap = $500 (giảm exposure)
- **Verify Wall tại T-2s:** Nếu Wall đã biến mất → cancel Trap order

---

## 5. Timing Protocol — Khi Nào Đọc OB?

> ⚠️ **Code hiện tại đơn giản hơn:** Bot chỉ đọc OB tại 2 thời điểm: (1) `fire_ioc` — snapshot cho slippage + TP wall, (2) `fire_trap` — snapshot cho wall-based trap placement. Không có multiple snapshots T-60s/T-10s/T-5s.

OB trên altcoin thay đổi nhanh. Đọc quá sớm = data cũ. Đọc quá muộn = không kịp xử lý.

| Thời điểm | Hành động | Mục đích | Độ tin cậy OB |
|---|---|---|---|
| **T-60s** | Subscribe OB WebSocket | Bắt đầu nhận data depth | Thấp — OB đang bị rút dần |
| **T-10s** | Snapshot #1: Tính Imbalance Ratio | Tham khảo sentiment thị trường | ⚠️ Trung bình — traders đang rút |
| **T-5s** | Snapshot #2: Xác nhận Wall positions | So sánh với T-10s xem wall có ổn định | ⚠️ Trung bình |
| **T-2s** | Final Lock: Tính slippage IOC | **Slippage = tin cậy.** Wall detection = tham khảo | ✅ Slippage / ⚠️ TP |
| **T±0** | Settle xảy ra | Funding fee được tính | N/A |
| **T+0~50ms** | Fire IOC (có TakeProfitPrice nếu tìm được wall) | Vào lệnh + set TP server-side | — |
| **T+0.5s** | IOC fill → Trailing Stop kích hoạt | Sàn tự theo dõi đỉnh/đáy | — |
| **T+1-2s** | Fire Trap (nếu config bật) | Đặt lệnh Limit bắt wick | — |

> **⚠️ Lưu ý quan trọng:** OB tại T-2s phản ánh **thanh khoản tức thì** (đủ cho slippage IOC), nhưng **KHÔNG phản ánh** thanh khoản tại T+2s khi giá thực sự chạm TP. Xem §3.4 để hiểu vì sao.

**Quy tắc freshness:** Bất kỳ OB snapshot nào > 3 giây tuổi → coi như stale → fallback về config tĩnh.

---

## 6. Đặc Thù Altcoin Thanh Khoản Thấp

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

## 7. Tổng Kết — Bộ Ba Quyết Định

```
┌─────────────────────────────────────────────────────┐
│              FUNDING REVERSION ENGINE                │
│                                                     │
│  ① FR → HƯỚNG ĐI (Long hay Short?)                 │
│     FR > 0 → LONG    FR < 0 → SHORT                │
│                                                     │
│  ② FR × Multiplier + ATR → TP/SL CHÍNH             │
│     Dựa trên thống kê, không phụ thuộc OB           │
│     (Xem §8 — Dynamic Pricing)                      │
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

## 8. Cơ Chế Đóng Vị Thế — So Sánh Các Phương Pháp

Bot có 3 cách để đóng vị thế sau khi IOC fill. Mỗi cách phù hợp cho tình huống khác nhau.

### 8.1 ATR là gì?

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

### 8.2 Dynamic Pricing: TP/SL dựa trên FR + ATR

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

### 8.3 Ba Cơ Chế Đóng Vị Thế

| # | Cơ chế | Loại lệnh MEXC | Cách hoạt động | Khi nào dùng |
|---|---|---|---|---|
| 1 | **TP cố định (Trigger Market)** | `takeProfitPrice` trong IOC request | Sàn tự bán khi giá chạm mức X | Safety cap — luôn đặt để phòng trailing fail |
| 2 | **Trailing Stop (TrackOrder)** | `POST /trackorder/place` | Sàn theo dõi đỉnh/đáy, đóng khi giá quay đầu X% | **Phương pháp chính** — cưỡi sóng tối đa |
| 3 | **Manual close** | Không có lệnh tự động | Trader tự đóng | Khi trailing bị tắt trong config |

### 8.4 Trailing Stop — Giải Thích Chi Tiết

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

### 8.5 Phối Hợp Giữa Các Cơ Chế

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

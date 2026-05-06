# Wall Trust Score — Thuật Toán Chấm Điểm Uy Tín Tường Lệnh

> Đây là bộ lọc cốt lõi nhất của chiến thuật Penny Jumping. Nếu không phân biệt được **Tường Thật vs Tường Giả**, bot sẽ liên tục bị dính bẫy Spoofing và cháy tài khoản.

---

## 1. Tổng Quan Hệ Thống Chấm Điểm

Mỗi Wall được phát hiện trên Order Book sẽ được chấm điểm từ **0 → 100**. Bot chỉ thực thi Penny Jump khi `TrustScore >= THRESHOLD` (khuyến nghị: **≥ 65**).

```
TrustScore = w1 * AgeScore
           + w2 * SizeScore
           + w3 * AbsorptionScore
           + w4 * StabilityScore
           + w5 * ContextScore
           + w6 * HistoricalScore
           + Penalty (âm)
```

**Trọng số khuyến nghị:**

| Factor | Weight | Lý do |
|---|---|---|
| `AgeScore` (Tuổi tường) | 20% | Spoof wall thường tồn tại < 2 giây |
| `SizeScore` (Kích thước) | 15% | Tường quá nhỏ = vô nghĩa, quá lớn = có thể bẫy |
| `AbsorptionScore` (Hấp thụ) | 25% | **Quan trọng nhất** — tường chịu được xả mới đáng tin |
| `StabilityScore` (Ổn định) | 15% | Tường không bị resize/cancel liên tục |
| `ContextScore` (Bối cảnh) | 15% | Vị trí tường có hợp lý với cấu trúc thị trường không |
| `HistoricalScore` (Lịch sử) | 10% | Mức giá này có hay xuất hiện tường trước đây không |

---

## 2. Chi Tiết Từng Yếu Tố

### 2.1. AgeScore — Tuổi Tường (0-100)

Đo thời gian tường tồn tại liên tục trên Order Book kể từ lần đầu phát hiện.

```
if age < 1s   → 0    (mới xuất hiện, chưa đáng tin)
if age < 3s   → 20   (quá mới)
if age < 10s  → 50   (bắt đầu đáng tin)
if age < 30s  → 75   (tường có chủ đích)
if age < 60s  → 90
if age >= 60s → 100  (tường rất cứng)
```

> **Logic:** Spoofer thường đặt tường rồi rút trong 0.5-3 giây. Tường tồn tại > 10 giây đã vượt qua bài test cơ bản.

### 2.2. SizeScore — Kích Thước Tương Đối (0-100)

So sánh volume của Wall với **volume trung bình của 20 mức giá gần nhất** trên cùng bên (Bid hoặc Ask).

```
ratio = wall_volume / avg_volume_per_level

if ratio < 5x    → 0    (không phải wall, chỉ là lệnh thường)
if ratio < 10x   → 30
if ratio < 20x   → 60
if ratio < 50x   → 85
if ratio < 100x  → 100
if ratio >= 100x → 70   (⚠️ GIẢM ĐIỂM — quá lớn = có thể là bẫy)
```

> **Logic "Quá lớn = Bẫy":** Ở shitcoin, một lệnh chiếm > 100x volume trung bình là bất thường cực kỳ. Đây thường là MM dự án đặt tường khổng lồ để dụ retail, rồi rút tường + xả hàng phía đối diện. Giảm điểm khi kích thước phi thường.

### 2.3. AbsorptionScore — Khả Năng Hấp Thụ (0-100) ⭐ QUAN TRỌNG NHẤT

Theo dõi xem tường có **chịu đựng được áp lực bán/mua từ phía đối diện** hay không. Đây là bài test thực chiến đáng giá nhất.

```
absorbed_volume = tổng volume Market Order đã đập vào tường mà tường vẫn đứng
wall_original_volume = kích thước ban đầu của tường

absorption_ratio = absorbed_volume / wall_original_volume

if absorption_ratio < 0.01  → 10   (chưa bị test, chưa biết thật giả)
if absorption_ratio < 0.05  → 40   (bị gặm nhẹ, vẫn đứng)
if absorption_ratio < 0.15  → 70   (chịu được áp lực trung bình)
if absorption_ratio < 0.30  → 90   (rất cứng, đã hấp thụ lượng lớn)
if absorption_ratio >= 0.30 → 100  (tường sắt — rất đáng tin)
```

> **Logic:** Spoofer sẽ **không bao giờ** để tường bị "ăn" thật sự vì họ sẽ mất tiền thật. Nếu tường hấp thụ được 5-15% volume mà không rút → khả năng cao là tường thật (người đặt thật sự muốn mua/bán ở mức giá đó).

**Cách đo Absorption:**
1. Lưu lại `wall_volume` tại thời điểm phát hiện (T0).
2. Mỗi lần nhận WS update, kiểm tra:
   - Nếu `wall_volume` giảm → có người đã Market vào tường → `absorbed += (old_volume - new_volume)`
   - Nếu `wall_volume` tăng lại → chủ tường "đổ thêm quân" → Reset `absorbed` = 0 (hoặc giảm trọng số vì hành vi bất ổn)

> **⚠️ Giới hạn thực tế (MEXC API):** Channel `sub.depth.step0` chỉ gửi **aggregated snapshots** (20 levels), KHÔNG gửi individual trade executions. Do đó không thể biết chính xác "bao nhiêu volume đã bị Market Order đập vào tường."
>
> **Heuristic workaround:** So sánh `wall_volume` giữa 2 snapshot liên tiếp (interval ~100ms). Nếu giảm → **suy luận** có absorption. Tuy nhiên volume cũng có thể giảm do chủ tường tự cancel một phần. Để giảm false positive, chỉ tính absorption khi:
> 1. Volume giảm dần đều qua ≥ 3 snapshot liên tiếp (không phải 1 lần giảm đột ngột rồi biến mất = spoof rút).
> 2. Kết hợp với `StabilityScore` — nếu wall đang ổn định (không resize) mà volume giảm nhẹ → nhiều khả năng là absorption thật.

### 2.4. StabilityScore — Độ Ổn Định (0-100)

Đo số lần tường thay đổi kích thước (resize) trong khoảng thời gian tồn tại.

```
resize_count = số lần wall_volume thay đổi > 5% trong 30 giây qua

if resize_count == 0 → 100  (cực ổn định)
if resize_count == 1 → 70
if resize_count == 2 → 40
if resize_count >= 3 → 10   (dao động liên tục = spoofer đang test thị trường)
```

> **Logic:** Tường thật thường được đặt 1 lần và giữ nguyên. Spoofer liên tục resize (tăng/giảm) để thử phản ứng thị trường trước khi rút.

### 2.5. ContextScore — Bối Cảnh Thị Trường (0-100)

Kiểm tra xem vị trí của tường có **hợp lý** với cấu trúc thị trường hiện tại không.

| Điều kiện | Điểm | Giải thích |
|---|---|---|
| Tường nằm ở **số tròn** (round number) | +30 | Ví dụ: 0.00500, 0.01000 — nơi tâm lý trader thường đặt lệnh |
| Tường nằm gần **mức hỗ trợ/kháng cự** trước đó | +30 | Trùng với vùng giá lịch sử có ý nghĩa |
| Spread hiện tại **hẹp** (< 0.3%) | +20 | Order book khỏe, dễ thoát lệnh |
| Tường nằm ở **giữa hư không** (không gần S/R, không số tròn) | -20 | Vị trí vô nghĩa, khả năng là bẫy |
| Cùng lúc có wall **cả 2 bên** Bid+Ask | -30 | MM đang kẹp giá, không phải signal |
| Volume 24h của coin **quá thấp** (< $50K) | -20 | Thanh khoản quá mỏng, rủi ro kẹt lệnh |

### 2.6. HistoricalScore — Lịch Sử Mức Giá (0-100)

Bot lưu lại **bộ nhớ ngắn hạn** (rolling window 1-4 giờ) về các tường đã xuất hiện và biến mất.

```
Tại mức giá P:
  - Nếu trong 1h qua đã có wall xuất hiện rồi bị rút ≥ 2 lần → Score = 0 (SPOOFING CONFIRMED)
  - Nếu trong 1h qua đã có wall và wall bị ăn hết (filled) → Score = 80 (có demand thật)
  - Nếu lần đầu xuất hiện wall tại P → Score = 50 (neutral)
```

---

## 3. Penalty — Điểm Phạt (Trừ Trực Tiếp)

Ngoài 6 yếu tố trên, áp dụng các penalty cứng:

| Tình huống | Penalty | Lý do |
|---|---|---|
| Wall xuất hiện cùng lúc có **Trade burst** lớn phía đối diện | -20 | Có thể là wash trading tạo illusion |
| Wall volume > 30% tổng OB volume một bên | -15 | Tập trung quá mức, bất thường |
| Coin đang trong giai đoạn **Pump rõ rệt** (giá tăng > 10% trong 1h) | -25 | Tường có thể là exit liquidity của pumper |
| Khoảng cách từ Wall đến Best Bid/Ask > 1% | -30 | Tường quá xa, không có tác dụng bảo vệ |

---

## 4. Ví Dụ Thực Tế

### Case 1: Tường Thật ✅
```
Coin: SHITUSDT — Giá hiện tại: 0.00512
Bid Wall phát hiện tại 0.00500 — Volume: 2,000,000 SHIT ($10,000)
Avg volume/level: 50,000 SHIT → ratio = 40x

AgeScore:        75  (tường đã tồn tại 25 giây)
SizeScore:       85  (40x — lớn nhưng không phi thường)
AbsorptionScore: 70  (đã hấp thụ 8% volume, vẫn đứng vững)
StabilityScore:  100 (chưa resize lần nào)
ContextScore:    60  (+30 số tròn, +20 spread hẹp, +10 gần S/R)
HistoricalScore: 50  (lần đầu xuất hiện)
Penalty:         0

TrustScore = 0.20*75 + 0.15*85 + 0.25*70 + 0.15*100 + 0.15*60 + 0.10*50
           = 15 + 12.75 + 17.5 + 15 + 9 + 5
           = 74.25 ✅ (>= 65 → JUMP!)
```

### Case 2: Tường Giả (Spoofing) ❌
```
Coin: SCAMUSDT — Giá hiện tại: 0.00320
Bid Wall phát hiện tại 0.00310 — Volume: 50,000,000 SCAM ($15,500)
Avg volume/level: 100,000 SCAM → ratio = 500x

AgeScore:        20  (mới xuất hiện 2 giây)
SizeScore:       70  (500x — phi thường, bị giảm điểm)
AbsorptionScore: 10  (chưa bị test)
StabilityScore:  40  (đã resize 2 lần trong 2 giây)
ContextScore:    -20 (không gần S/R, không số tròn, spread rộng)
HistoricalScore: 0   (mức giá này đã bị spoof 3 lần trong 1h qua!)
Penalty:         -15 (wall > 30% tổng OB)

TrustScore = 0.20*20 + 0.15*70 + 0.25*10 + 0.15*40 + 0.15*(-20) + 0.10*0 - 15
           = 4 + 10.5 + 2.5 + 6 + (-3) + 0 - 15
           = 5.0 ❌ (<< 65 → SKIP!)
```

---

## 5. Lưu Ý Implement

1. **Tất cả factor đều cần dữ liệu realtime WebSocket.** Không thể dùng REST API polling.
2. **Cần buffer lịch sử OB updates** (ring buffer ~60 giây) để tính Absorption và Stability.
3. **HistoricalScore cần persistent map** `map[symbol][priceLevel] → []WallEvent` rolling 1-4h.
4. **Xử lý Cold-Start tự nhiên (Self-healing):** Khi một cặp coin mới được subscribe, tường sẽ chưa có lịch sử nên `AgeScore` = 0. Thuật toán **tự động xử lý** vấn đề này mà không cần code thêm "Grace Period" phức tạp: Tường mới sẽ có điểm thấp (< 65) và bị bỏ qua. Chỉ khi tường tồn tại đủ lâu (vài chục giây) để lấy đủ data, `AgeScore` và `AbsorptionScore` tăng lên sẽ giúp `TrustScore` tự động vượt ngưỡng 65.
5. **Threshold 65 là khởi điểm.** Cần backtest và điều chỉnh theo từng sàn (MEXC vs Binance behavior khác nhau).
6. **Position sizing nên tỉ lệ thuận với TrustScore:** Score 65 → size nhỏ, Score 90 → size lớn hơn.

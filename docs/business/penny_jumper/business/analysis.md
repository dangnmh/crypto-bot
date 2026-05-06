# Phân Tích Chiến Thuật Penny Jumping (Chuyên Biệt Cho Altcoin / Low-cap / Shitcoin)

> **Mục tiêu cốt lõi:** Bỏ qua các chiến trường khốc liệt như BTC, ETH (nơi các quỹ HFT trị giá hàng triệu đô với hạ tầng Co-location cạnh tranh từng micro-giây). Tập trung rình rập ở khu vực "chợ đen" (các đồng coin rác, thanh khoản thấp, cap nhỏ) để nhặt lợi nhuận mà cá mập lớn không thèm ngó tới.
>
> **Tài liệu liên quan:** [Wall Trust Score — Thuật toán chấm điểm uy tín tường lệnh](./wall_trust_score.md)

---

## 1. Tại Sao Lại Chọn Altcoin / Shitcoin? (Lợi Thế Sân Sau)

Việc áp dụng Penny Jumping vào Low-cap/Shitcoin mang lại những lợi thế sinh tồn cực kỳ lớn cho hệ thống bot vốn nhỏ ($500 - $5,000):

1. **Không có HFT lớn (No Tier-1 Predators):** Các quỹ MM lớn và HFT firm bỏ qua coin rác vì Volume quá thấp, không đủ thanh khoản để họ chạy size. Bạn chỉ đang đấu với bot retail hoặc MM nội bộ dự án.
2. **Yêu cầu tốc độ (Latency) "dễ thở" hơn:** Ở shitcoin, phản hồi 50ms-100ms (VPS AWS thường) đã đủ để đi trước đám đông retail đánh bằng tay.
3. **Biên độ bước giá (Tick Size) có lợi:** Ở shitcoin, 1 tick giá tương đương 0.1%-0.5% giá trị. Nhảy trước 1 tick mang lại biên lợi nhuận đủ lớn để bù phí, không mỏng dính như coin Top.
4. **Market Inefficiency:** Coin rác thường xuyên có "Cá voi lạc" — đặt lệnh Limit cực lớn lộ liễu mà không dùng thuật toán Iceberg.

---

## 2. Logic Cốt Lõi (Được Tinh Chỉnh Cho Shitcoin)

### 2.1. Nguyên Lý Cốt Lõi

Dùng "Bức Tường" (Wall) của Cá Mập / MM làm **bệ phóng** hoặc **lưới an toàn**:

*   **Lưới An Toàn (Safety Net):** Bid Wall cực lớn → khả năng giá thủng mốc đó ngắn hạn thấp (trừ khi tường rút). Ta đặt Bid ngay trên nó 1 tick.
*   **Target lợi nhuận rộng:** Khi Bid khớp, lực đỡ từ Wall khiến Retail FOMO đẩy giá lên. Chốt lời ở 0.5%-1.5% thay vì 1 tick.
*   **Bailout ngay lập tức:** Wall bị rút/bị thủng → Market Sell ngay → chịu spread + phí để tháo chạy.

### 2.2. Hai Hướng Giao Dịch (Bidirectional)

Không chỉ đánh theo Bid Wall, bot phải hỗ trợ cả 2 chiều:

| Loại Wall | Hành động | Entry | Take Profit | Stop Loss |
|---|---|---|---|---|
| **Bid Wall** (Tường mua) | Long/Buy | Đặt Bid ở `Wall_Price + 1 tick` | Ask ở +0.5% → +1.5% | Market Sell nếu Wall biến mất |
| **Ask Wall** (Tường bán) | Short/Sell | Đặt Ask ở `Wall_Price - 1 tick` | Bid ở -0.5% → -1.5% | Market Buy nếu Wall biến mất |

> **Lưu ý quan trọng:** Khi Short shitcoin trên Futures, kiểm tra **Funding Rate** trước. Nếu Funding Rate đang rất âm (Short trả tiền cho Long), chi phí giữ vị thế có thể ăn hết lợi nhuận.

---

## 3. Flow Hoạt Động (Altcoin Edition)

```
┌─────────────────────────────────────────────────────────────┐
│                 EVENT PIPELINE (Push-based)                   │
│                                                               │
│  ┌──────────┐    ┌──────────────┐    ┌───────────────────┐   │
│  │ L2 Push  │───▶│ Wall Detected│───▶│ TRUST SCORE ≥ 65? │   │
│  │(OB Build)│    │(Size >= 20x) │ NO │ (wall_trust_score) │   │
│  └──────────┘    └──────┬───────┘    └────────┬──────────┘   │
│                         │                     │              │
│                      [Ignore]                 ▼              │
│                                      ┌──────────────┐       │
│                                      │ SPAWN FSM &  │       │
│                                      │ PLACE MAKER  │       │
│                                      └──────┬───────┘       │
│                                             │               │
│                                      ┌──────▼───────┐       │
│                                      │  MONITORING   │       │
│                                      │(Pub/Sub Event)│       │
│                                      └──┬────┬──┬───┘       │
│                  wall:disappeared event │    │  │ order:filled
│                                         ▼    │  ▼            │
│                                 ┌────────┐   │ ┌──────────┐ │
│                                 │ CANCEL  │   │ │TP / TRAIL│ │
│                                 │ ORDER   │   │ │ + MONITOR│ │
│                                 └────┬────┘   │ │ WALL     │ │
│                                      │        │ └─────┬────┘ │
│                                      ▼        │       │      │
│                                    [DONE] ◀───┘       │      │
│                                                       │      │
└───────────────────────────────────────────────────────┘      │
```

### Bước 1: Quét Rộng Diện (Wide-Net Scanning)
*   **Bộ lọc Coin (Pre-filter):** Không cần subscribe WebSocket tất cả các cặp. Chỉ listen những cặp đạt điều kiện thanh khoản cơ bản (VD: Volume 24h > $nM) để tiết kiệm tài nguyên và tránh "coin chết" (dead coins).
*   WebSocket L2 monitor **hàng trăm** cặp altcoin rác cùng lúc (sau khi đã qua bộ lọc).
*   **Wall Filter:** Tường ≥ **20x** volume trung bình tại 20 mức giá gần nhất.
*   **Khoảng cách tối đa:** Wall phải nằm trong phạm vi **≤ 1%** từ Best Bid/Ask. Xa hơn → vô nghĩa.

### Bước 2: Chấm Điểm Uy Tín (Wall Trust Score)
*   Áp dụng thuật toán **Wall Trust Score** (xem chi tiết tại [wall_trust_score.md](./wall_trust_score.md)).
*   Score ≥ 65 → Đủ điều kiện nhảy. Score < 65 → Bỏ qua, tiếp tục scan.

### Bước 3: Nhảy Bước Giá (The Penny Jump)
*   Đặt lệnh **Maker Post-Only** cách tường 1 tick.
*   **Position sizing:** Tỉ lệ thuận với TrustScore. Score 65 → 30% max size. Score 90 → 100% max size.

### Bước 4: Giám Sát Liên Tục (Live Monitoring)
Sau khi đặt lệnh, bot phải theo dõi 3 thứ đồng thời:

| Sự kiện | Hành động |
|---|---|
| Wall bị rút (Volume → 0) | **Hủy lệnh chờ ngay lập tức** (< 50ms) |
| Wall bị gặm > 50% | Hủy lệnh chờ (wall yếu đi) |
| Lệnh bot khớp (Filled) | Chuyển sang Bước 5 |
| Giá chạy xa khỏi wall > 0.5% | Hủy lệnh (cơ hội đã mất) |
| Timeout 60 giây chưa khớp | Hủy lệnh (tránh kẹt vốn) |

### Bước 5: Thoát Lệnh (Exit Strategy)
*   **Take Profit:** Đặt Maker TP ở +0.5% → +1.5% (tùy volatility của coin).
*   **Trailing Stop (nếu giá chạy mạnh):** Kích hoạt trailing khi đã lời > 0.3%.
*   **Wall Monitor (post-fill):** Tiếp tục theo dõi Wall gốc. Nếu Wall biến mất → **Lập tức Market exit** bất kể đang lời hay lỗ.
*   **Timeout TP:** Nếu 120 giây TP không khớp → Market exit chốt lời/lỗ bất kỳ.

---

## 4. Rủi Ro & Lưu Ý Chí Mạng (Shitcoin Edition)

### 4.1. Tường Ảo (Spoofing) & Bơm Xả (Pump & Dump)
*   MM shitcoin cực kỳ hay đặt tường to rồi **rút nhanh như chớp**.
*   Bot khớp lệnh → Tường biến mất → Giá rơi tự do (OB mỏng ngoài tường).
*   **Giải pháp:** Thuật toán [Wall Trust Score](./wall_trust_score.md) với 6 yếu tố chấm điểm.

### 4.2. Spread Rỗng (Wide Spread)
*   Cắt lỗ bằng Market Order trượt giá 1-2%, xóa sạch 10 lệnh thắng.
*   **Quy tắc:** Chỉ nhảy khi `Spread < 0.3%`. Nếu `Spread > 0.5%` → **Tuyệt đối không vào**.
*   **Pre-calculate Worst-case:** Trước khi nhảy, tính sẵn `chi_phi_bailout = spread + taker_fee`. Nếu > 0.5% → skip.

### 4.3. Cấu Trúc Phí (Fee Structure)
*   **Maker = 0.01%, Taker = 0.05%** (MEXC Futures). Entry nên luôn là Maker (Post-Only). Exit lý tưởng cũng Maker (TP Limit), nhưng Bailout sẽ là Taker (Market Order).
*   Mỗi trade chỉ lời vài tick → phí ăn sâu vào biên lợi nhuận. Cần tính kỹ breakeven bao gồm cả 2 chiều phí (xem Section 5.4 PnL Calculator).
*   Cancel/Fill ratio sẽ rất cao (95%+ cancel). Kiểm tra sàn không phạt cancel.

### 4.4. Rate Limit & API Ban
*   Theo dõi hàng trăm cặp + Cancel liên tục → dễ bị IP Ban.
*   **Giải pháp:** Chỉ gửi Place Order khi TrustScore ≥ 65 (giảm 90%+ request rác).

### 4.5. Partial Fill (Khớp Lệnh Một Phần) — THIẾU SÓT BẢN CŨ
*   Shitcoin volume thấp → lệnh hay bị khớp 1 phần (VD: đặt mua 1000 coin, chỉ khớp 200).
*   **Quy tắc:** Nếu fill < 30% size → Market exit ngay (phí thoát > lợi nhuận tiềm năng).
*   Nếu fill ≥ 30% → Giữ và chạy TP/SL bình thường.

### 4.6. Nhiều Bot Cùng Nhảy (Queue Competition) — THIẾU SÓT BẢN CŨ
*   Ở các coin "vừa đủ thanh khoản", có thể có 2-3 bot retail khác cũng đang penny jump.
*   **Dấu hiệu:** Nhìn thấy nhiều lệnh nhỏ liên tục xuất hiện ngay trên Wall, cách nhau 1-2 tick.
*   **Giải pháp:** Nếu phát hiện ≥ 3 lệnh nhỏ xếp chồng trên Wall → Skip (lợi nhuận bị chia sẻ, rủi ro cascading cancel khi Wall rút).

### 4.7. Wall Di Chuyển (Wall Migration) — THIẾU SÓT BẢN CŨ
*   Chủ tường có thể cancel Wall ở giá A rồi đặt lại ở giá B (thấp/cao hơn).
*   Bot cần phân biệt: **Wall bị rút (Bailout trigger)** vs **Wall di chuyển (cần update lệnh theo)**.
*   **Cách xử lý:** Nếu trong 500ms sau khi Wall biến mất, phát hiện Wall mới cùng size ±10% ở mức giá gần → coi như Wall di chuyển → Adjust lệnh thay vì Bailout.

### 4.8. Coin Đang Bị Delist/Halt — THIẾU SÓT BẢN CŨ
*   Shitcoin có thể bị sàn **halt trading** hoặc **delist** bất ngờ.
*   Bot phải có **blacklist** các coin đang có thông báo delist/warning từ sàn.
*   Kiểm tra danh sách này mỗi 5-10 phút qua REST API.

---

## 5. Position Sizing & Risk Management

### 5.1. Position Sizing (Tỉ lệ vốn mỗi lệnh)
```
max_position_per_trade = total_capital * 0.02   (2% vốn / trade)
actual_size = max_position * (TrustScore / 100)

Ví dụ: Vốn $2,000, TrustScore = 80
→ max = $40, actual = $40 * 0.80 = $32 / trade
```

### 5.2. Giới Hạn Đồng Thời
*   Tối đa **3-5 vị thế** mở cùng lúc (tránh correlated risk nếu toàn bộ thị trường shitcoin dump).
*   Tối đa **1 vị thế / symbol** (tránh doubling down).

### 5.3. Daily Loss Limit
*   **Dừng bot nếu lỗ > 5% vốn trong ngày.** Có thể đang bị đánh bẫy hệ thống.

### 5.4. PnL Calculator (Penny Jump)

```
# Phí giao dịch MEXC Futures
Maker Fee  = 0.01%
Taker Fee  = 0.05%

# Công thức PnL
Gross PnL% = (ExitPrice - EntryPrice) / EntryPrice × 100
Net PnL%   = Gross% - EntryFee% - ExitFee%

# Best Case: Cả Entry và Exit đều Maker (Post-Only)
Net PnL%   = Gross% - 0.01% - 0.01% = Gross% - 0.02%

# Worst Case: Entry Maker, Exit Taker (Bailout Market Order)
Net PnL%   = Gross% - 0.01% - 0.05% = Gross% - 0.06%
```

**Ví dụ cụ thể:**

| Scenario | Entry | Exit | Gross | Fees | Net PnL | Kết quả (Vốn $32, Lev 20x) |
|---|---|---|---|---|---|---|
| TP thành công (Maker) | 0.00501 | 0.00508 (+1.4%) | +1.40% | 0.02% | **+1.38%** | +$8.83 |
| TP nhỏ (Maker) | 0.00501 | 0.00504 (+0.6%) | +0.60% | 0.02% | **+0.58%** | +$3.71 |
| Bailout (Taker, spread 0.2%) | 0.00501 | 0.00498 (-0.6%) | -0.60% | 0.06% | **-0.66%** | -$4.22 |
| Bailout (Taker, spread 0.5%) | 0.00501 | 0.00496 (-1.0%) | -1.00% | 0.06% | **-1.06%** | -$6.78 |

> **Breakeven Win Rate:** Với TP trung bình +0.58% và SL trung bình -0.66%, cần win rate ≥ **53%** để hòa vốn. Nếu Spread rộng (bailout -1.06%), cần win rate ≥ **65%**.

---

## 6. Kiến Trúc Hệ Thống — Isolated Bot Architecture

> **Nguyên tắc thiết kế:** Penny Jumper là một bot **hoàn toàn độc lập** (`cmd/penny_jumper/main.go`). KHÔNG chia sẻ WebSocket connection hay Data Store với bất kỳ bot nào khác (bao gồm Funding Reversion). Mỗi bot tự quản lý vòng đời hạ tầng của riêng mình.

1. **Dedicated WebSocket Connections:** Bot tự mở và quản lý pool WS connections riêng, subscribe `sub.depth.step0` cho các cặp coin đã qua bộ lọc. Sử dụng Multiplexer để dàn trải tải khi vượt quá giới hạn pairs/connection.
2. **Dedicated Local Store:** Bot sở hữu `LocalStore` riêng biệt chứa dữ liệu Ticker, Contract, và L2 OrderBook cache. Không truy cập vào store của Funding Reversion.
3. **State Machine (FSM):**
   ```
   StateIdle → StateWallDetected → StateJumpPlaced → StateMonitoringWall
       → StateFilled → StateTakeProfit / StateBailout → StateIdle
   ```
4. **Cross-Pair Scanner:** Quét song song hàng trăm cặp coin rác trên MEXC qua các WS connection riêng.
5. **Wall Trust Score Engine:** Module chấm điểm tường lệnh, nằm trong `internal/bots/penny_jumper/application/scorer.go`.

# Backlog — Lý Thuyết Chưa Implement & Open Questions

> **Mục đích:** Tập trung tất cả các phân tích lý thuyết, ý tưởng chưa triển khai, và các vấn đề mở vào một file duy nhất. Tách biệt khỏi tài liệu mô tả logic đã implement trong code.
>
> **Nguồn gốc:** Nội dung được trích từ `depth.md`, `trap_flow.md`, `trap_strategy_guide.md` trong quá trình hệ thống hóa tài liệu.

---

## 1. Imbalance Ratio — Đo Lực Long/Short

> **Nguồn gốc:** `depth.md §2` — tag `[LÝ THUYẾT]`
>
> **Trạng thái:** Chưa implement. Code hiện tại không tính Imbalance Ratio.

### 1.1 Cách Đo

Quét tổng volume Bid vs Ask trong biên độ giá gần (2–3% quanh giá hiện tại):

```
Imbalance Ratio = Tổng Bid Volume / Tổng Ask Volume
```

### 1.2 Đọc Vị Kết Quả

| Imbalance Ratio | Ý nghĩa | Hành động |
|---|---|---|
| **> 2.0** | Bid áp đảo → "Sàn bê tông" bên dưới | Thuận lợi cho LONG. TP dãn rộng, SL bóp sát dưới Bid Wall |
| **1.5 – 2.0** | Bid nhỉnh hơn | Thuận lợi nhẹ cho LONG |
| **0.7 – 1.5** | Cân bằng → Giằng co | Trung lập. Dùng TP/SL tĩnh |
| **0.5 – 0.7** | Ask nhỉnh hơn | Thuận lợi nhẹ cho SHORT |
| **< 0.5** | Ask áp đảo → "Trần sắt" bên trên | Thuận lợi cho SHORT. TP dãn rộng |

### 1.3 Cảnh Báo Spoofing

Altcoin thanh khoản thấp trên MEXC Futures bị spoofing rất nặng. Nguyên tắc lọc:

| Quy tắc | Lý do |
|---|---|
| Chỉ tin 3–5 levels gần nhất | Sát giá → ít bị giả hơn |
| Wall phải tồn tại > 10–15 giây | Wall spoofing thường nhấp nháy |
| So sánh tương đối, không tuyệt đối | So với 24h volume |

---

## 2. Điều Chỉnh TP & SL Linh Hoạt Theo OB

> **Nguồn gốc:** `depth.md §3` — tag `[LÝ THUYẾT]`
>
> **Trạng thái:** Chưa implement. Code hiện tại chỉ dùng OB wall làm safety cap cho TP.

### 2.1 LONG — Tình huống A: Đường trống phía trên (Ask mỏng, Bid dày)

```
Giá: $18.00 | Ask Wall: KHÔNG CÓ | Bid Wall: $17.80
→ TP: Dãn rộng x1.5–x2 | SL: Đặt ngay dưới Bid Wall $17.78
```

### 2.2 LONG — Tình huống B: Tường Ask chặn ngay trên đầu

```
Giá: $18.00 | Ask Wall: $18.15 (15% 24h vol) | Bid Wall: Không rõ
→ TP: Bóp ngắn dưới tường $18.13 | SL: Giữ tĩnh từ config
```

### 2.3 SHORT — Áp dụng ngược lại hoàn toàn

### 2.4 Quy Tắc An Toàn

> **Hard limit: TP/SL điều chỉnh KHÔNG BAO GIỜ vượt quá giới hạn config.**

---

## 3. Chiến Lược Đặt Bẫy (Trap) Neo Tường OB

> **Nguồn gốc:** `depth.md §4` — tag `[LÝ THUYẾT]`
>
> **Trạng thái:** Tham khảo. Code dùng FR × multiplier làm primary. OB wall chỉ fallback.

### 3.1 Quy Trình Neo Tường

1. **Tìm Khoảng Trống (Liquidity Void):** Quét OB phía ngược hướng trade. Volume mỏng = khoảng trống.
2. **Xác Định Tường Dội:** Level có volume ≥ 3× average volume per level.
3. **Đặt Trap Trước Tường:** 1 tick trước tường dội.
4. **TP/SL:** TP = quay lại gần giá ban đầu (hồi 50-80%). SL = bên dưới/trên tường.

### 3.2 Khi KHÔNG NÊN Đặt Trap (OB-based)

| Tình huống | Hành động |
|---|---|
| Không tìm thấy tường rõ ràng | Skip hoặc fallback % cố định |
| Tường quá xa (> 5%) | Skip |
| Tường quá sát (< 0.5%) | Skip |
| Tường nhấp nháy (< 10s) | Skip — spoofing |
| Imbalance ∈ [0.7, 1.5] | Skip hoặc giảm size 50% |

### 3.3 Rủi Ro "Tường Giấy" (Paper Wall)

Kịch bản xấu: Wall bị rút lúc settle → giá rơi thẳng qua → Trap kẹt đáy.

**Biện pháp đề xuất (chưa implement):**
- Trailing SL trên Trap: fill mà giá không nảy 3-5s → cắt lỗ
- Position size Trap nhỏ hơn IOC
- Verify Wall tại T-2s: biến mất → cancel Trap

---

## 4. Multi-Snapshot Timing Protocol

> **Nguồn gốc:** `depth.md §5`
>
> **Trạng thái:** Chưa implement. Code chỉ đọc OB tại 2 thời điểm: fire_ioc và fire_trap.

Ý tưởng đọc OB tại nhiều thời điểm:

| Thời điểm | Hành động | Độ tin cậy |
|---|---|---|
| T-60s | Subscribe OB WS | Thấp |
| T-10s | Snapshot #1: Imbalance Ratio | ⚠️ Trung bình |
| T-5s | Snapshot #2: Xác nhận Wall | ⚠️ Trung bình |
| T-2s | Final Lock: Slippage IOC | ✅ Slippage / ⚠️ TP |

**Quy tắc freshness:** OB snapshot > 3 giây → stale → fallback config tĩnh.

---

## 5. Open Concerns — Rủi Ro Chưa Có Biện Pháp

### 5.1 Trap — Verify Wall Tại Runtime

Code hiện tại không verify wall vẫn tồn tại sau khi Trap fire. Chưa có cơ chế cancel Trap nếu wall biến mất.

### 5.2 Trap — Skip Dựa Trên Imbalance Ratio

Bảng skip conditions đề cập Imbalance filter nhưng chưa implement trong code.

### 5.3 Position Size Trap vs Reversion

Đề xuất Trap nên có size nhỏ hơn Reversion vì rủi ro cao hơn. Hiện tại code dùng cùng margin × leverage cho cả hai.

---

## Tài Liệu Liên Quan

| Tài liệu | Nội dung |
|-----------|---------|
| [Cycle Recorder Design](analyze.md) | Proposed design cho persistence layer (chưa implement) |
| [depth.md](depth.md) | Phần đã implement: Slippage IOC, Wall Detection, ATR, Trailing |
| [trap_flow.md](trap_flow.md) | Logic Trap đã implement |

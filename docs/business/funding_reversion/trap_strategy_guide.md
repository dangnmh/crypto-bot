# Hướng Dẫn Canh Bẫy (Trap Depth Strategy)

Tài liệu này dùng để tra cứu nhanh tỷ lệ đặt bẫy `TrapDepthPct` lý tưởng dựa trên độ lớn của Funding Rate (FR) tại thời điểm sắp thu phí. Quy luật vận hành dựa vào mức độ hoảng loạn xả hàng của đám đông sau giờ G (khi đã nhận/trả hụi xong).

## Bảng Tra Cứu Tỷ Lệ (Cheat Sheet)

| Khoảng Funding Rate (|FR|) | Mức TrapDepthPct Đề Xuất | Hành Vi Đám Đông (Tâm Lý Thị Trường) | Tần Suất Khớp Lệnh (% Fill) | Lưu Ý Cấu Hình (funding.jsonc) |
| :--- | :--- | :--- | :--- | :--- |
| **Dưới 0.3%** | Bỏ qua (Skip) | Không bõ dính răng, phí giao dịch sàn ăn hết. Lực xả gần như không có. | N/A | Nên để `minFundingRate = 0.003` (0.3%) |
| **0.3% - 0.6%** | `0.015 - 0.025` (1.5% - 2.5%) | Thanh khoản xả từ từ, trader chốt lời nhẹ nhàng. Giật râu nến ngắn gọn. | Cao (80%+) | Set bẫy gắt 5% khu vực này mút mùa không khớp nổi lệnh. |
| **0.6% - 1.2%** | `0.025 - 0.040` (2.5% - 4.0%) | Bắt đầu có dấu hiệu giẫm đạp chạy trốn (Dump/Pump tương đối mạnh). Râu nến giật sâu. | Trung Bình - Khá (60%+) | Đây là "vùng vàng". Ăn đậm và an toàn. Rủi ro trượt giá trung bình. |
| **1.2% - 2.0%** | `0.040 - 0.060` (4.0% - 6.0%) | Biến động mạnh, thanh khoản chênh lệch. Trader cháy tài khoản ở thời khắc Settle. | Hơi Hên Xui (40%+) | Bắt đầu cực kỳ hỗn loạn. Bắt ở mốc 5% sẽ chống được rủi ro kẹp đỉnh/đáy. |
| **2.0% Trở Lên** | `0.060 - 0.100+` (6.0% - 10.0%) | Cực độ Fomo/Fud. Coin xả không phanh, lệnh Market nối đuôi nhau trượt giá vô cực. | Phụ Thuộc Sàn | Biến động siêu nhạy, ăn 8% có thể diễn ra trong 2 giây. Rủi ro Margin cực lớn nếu vốn yếu. |

---

## 🎯 Quy Tắc Cốt Lõi (Rule of Thumb)

1. **Công thức ngầm định:** Độ sâu bẫy (D) thường được gài ở tỷ lệ **`[D = FR x 3] đến [FR x 5]`**
   * *Ví dụ: FR là `0.4%` -> Đặt bẫy `1.2%` đến `2.0%`.*
   * *Ngoại lệ: Nếu FR quá nhỏ, bớt tham lam lại kẻo không dính bẫy.*
2. **Kèo FR cực âm (Khách Long ăn tiền, Cả làng bị Short bẹp ruột):** Mức này giá thường **PUMP** ngược lên (Hồi giá cực nhạy), bắt Trap SHORT ở 3 - 5% rất hời.
3. **Kèo FR cực dương (Khách Short ăn tiền, Cả làng bị Long cháy):** Mức này giá thường **DUMP** (Sụp xả kinh dị). Bắt Trap LONG ở đáy có thể thả nới lỏng ra xíu để an toàn hơn.

> [!TIP]
> Hãy copy linh hoạt tỷ lệ rải mìn này vào tệp `funding.jsonc` cho từng Symbol cụ thể dựa theo dữ liệu soi quét trước giờ khớp lệnh khoảng 30 phút.

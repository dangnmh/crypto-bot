# AI CONTEXT - TÀI LIỆU DẪN HƯỚNG DỰ ÁN
> **LƯU Ý:** File này chứa trí nhớ cốt lõi (Core Memories) của dự án `funding-script`. Bất kỳ AI/Agent hay Lập trình viên nào khi tham gia vào dự án cần đọc toàn bộ file này trước khi chỉnh sửa một dòng code.
API Futures Trading Fees: Maker 0.01%, Taker 0.05%
Chỉ đánh coin nhỏ lẻ có Funding Rate cao
vốn max tầm 1000$ leverage 20x

## 1. MỤC TIÊU DỰ ÁN (PROJECT OVERVIEW)
- **Tên kỹ thuật:** Funding Rate Sniper Bot
- **Mục tiêu:** Canh me thời điểm T=0 (Lúc trừ phí Funding) của các đồng Xu cỏ (Shitcoins/Low-caps) có Funding Rate cực cao.
- **Phong cách đánh:** HFT (High-Frequency Trading) độ trễ thấp, kết hợp ăn nhịp xả rác và nhịp nảy đàn hồi (Catching wicks).

## 2. CHIẾN THUẬT LÕI: STRADDLE TRAPPING (GỌNG KÌM 2 LUỒNG)
Hệ thống không đánh một chiều mà tung 2 móc neo (Goroutines) độc lập để vắt kiệt sóng sập.
- **Lưới 1 (Giành Đỉnh): Lệnh IOC (Immediate-Or-Cancel)**
  - Bắn tại: `T - Offset` (Ví dụ `T - 100ms`).
  - Mục đích: Tranh giành vị thế với đám đông trước khi sổ lệnh bắt đầu rỗng. Lệnh sinh ra có chức năng triệt tiêu ngay lập tức nếu trượt giá quá mức không cướp được đỉnh.
- **Lưới 2 (Hứng Đáy): Lệnh Limit Giao Ngay (Standard Limit)**
  - Bắn tại: `T + Lùi giờ` (Ví dụ `T + 10ms` để lách giờ điểm danh Fee 100%).
  - Cơ chế Giá: `PeakPrice * (1 - trapDepthPct)` (Nằm sâu tận -5% hoặc -7%).
  - **TUYỆT ĐỐI KHÔNG DÙNG POST-ONLY:** Chấp nhận nộp phí Taker đắt hơn (0.05% thay vì 0.01%) để hệ thống cắn và lấp lỗ hổng bắt đáy khi giá trượt quá sâu và bạo lực xuyên qua Orderbook. (File `opener.go` -> `OrderTypeLimit`).

## 3. CẢNH BÁO QUẢN TRỊ TÀI KHOẢN (ACCOUNT RULES)
- **HEDGE MODE BẮT BUỘC:** Vì hệ thống cầm 2 đầu trái cực nhau trên cùng 1 Symbol. Nếu cấu hình là `"ONE_WAY"`, lệnh hốt đáy số 2 khi bung ra sẽ vô tình Đóng mẹ mất vị thế của lệnh ở Đỉnh số 1.
  - Setup trong `funding.jsonc`: `"positionMode": "HEDGE"`.
- **Đệm lót Margin (Margin Buffer):**
  - Khối lượng của bot dùng biến `marginUSDT * Leverage`. Cả 2 luồng đều gọi khối lượng này giống nhau.
  - **Không bao giờ set `marginUSDT` chiếm tới 50% quỹ tổng.** Sàn luôn phong tỏa một lượng Tiền Phí Ảo (Taker fee lock) lúc đặt Limit. Nếu Tổng Vốn là 100$, chỉ nạp `marginUSDT = 45`, chừa 10$ làm khoản không khí đệm, nếu không lệnh ném Bẫy sẽ bị Sàn chặn đứng mỏ vì lỗi `Insufficient Margin`.

## 4. QUẢN TRỊ HIỆU LỰC GIAO DỊCH (EXECUTION & LIFECYCLE)
- **Tách Luồng Kép:** `phaseFire` trong `sniper.go` sử dụng `sync.WaitGroup` tách 2 routines độc lập. Tốc độ bắn không bị block lẫn nhau.
- **No Auto-Close (Đóng tay hoàn toàn):**
  - Mọi logic hú lệnh rút lui, Take Profit, Stop Loss hay Cancel mồi bẫy dư thừa bên trong `phaseHold` đã bị dỡ bỏ.
  - Sự an toàn của hệ thống bây giờ nằm ở việc Chốt Lời / Cắt Lỗ THỦ CÔNG trên giao diện màn hình (Ví dụ: App MEXC) sau khi khói súng tan. Bot kết thúc cycle cực nhanh và an phận.
- **Xử lý Precision (Độ chính xác Tick size):**
  - Thuật toán `strategy.CalculateTrapPrice` và `CalculateIOCPrice` (`pricing.go`) đóng vai trò ép chuẩn Price Unit và Cắt tầng thập phân. Không bao giờ quăng giá tính chay Float lên API nếu không sẽ gặp lỗi `2015 Precision Error`.

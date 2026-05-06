# Algorithmic and High-Frequency Trading - Álvaro Cartea, Sebastian Jaimungal, José Penalva

## Tổng quan (Core Concept)
- **Cốt lõi:** Đây là cuốn giáo trình Toán học định lượng (Quantitative Mathematics) tối thượng dành cho các Quants. Khác hoàn toàn với các cuốn sách nặng tính lý thuyết hàn lâm hay kể chuyện, cuốn sách này sử dụng trực tiếp **Phương trình Vi phân Ngẫu nhiên (Stochastic Calculus)** và **Điều khiển Tối ưu (Optimal Control)** để viết ra các phương trình thuật toán cho bot giao dịch.
- **Vấn đề:** Thay vì đặt câu hỏi định tính "Chúng ta nên chia nhỏ lệnh như thế nào?", các tác giả đặt câu hỏi định lượng "Công thức toán học chính xác để tối ưu hóa lợi nhuận trong khi vẫn kiểm soát rủi ro tồn kho (Inventory) ở từng mili-giây là gì?". 
- **Giải pháp:** Sử dụng công cụ toán học tối ưu hóa động (Hamilton-Jacobi-Bellman - HJB equations) để thiết kế các thuật toán tự hành, cho phép bot tự động tính toán được chính xác lúc nào nên ném lệnh Limit, lúc nào nên "cướp" bằng lệnh Market tùy theo tình trạng tức thời của sổ lệnh.

---

## Tóm tắt chi tiết, Đúc kết và Thông tin quan trọng (Nhóm theo Phần/Chương)

### Phần I: Microstructure and Empirical Facts (Cấu trúc vi mô và Bằng chứng thực nghiệm)
- **Các chương:** Chương 1 - 4 (Thị trường điện tử & LOB, Kiến thức cơ bản, và Bằng chứng thống kê về giá, khối lượng).
- **Tóm tắt & Đúc kết:** Tác giả hệ thống hóa lại cấu trúc Limit Order Book (LOB) nhưng dưới lăng kính xác suất thống kê. Quá trình đến của các lệnh (order arrival) được mô hình hóa bằng các quá trình ngẫu nhiên (như quá trình Poisson).
- **Thông tin quan trọng cho Bot:** Cung cấp framework toán học để bạn lập trình các module phân tích LOB. Chẳng hạn, đo lường sự chênh lệch áp lực mua/bán và xây dựng bộ đếm thời gian (arrival rate) của các lệnh Market.

### Phần II: Mathematical Foundation (Nền tảng Toán học)
- **Các chương:** Chương 5 (Stochastic Optimal Control and Stopping).
- **Tóm tắt & Đúc kết:** Đây là xương sống toán học của sách. Hướng dẫn cách giải quyết bài toán tối ưu động (Dynamic Programming) theo thời gian liên tục.
- **Thông tin quan trọng:** Yêu cầu người đọc phải có nền tảng Toán giải tích cực tốt. Nếu bạn là Developer chuyển sang làm Quant, bạn cần nắm vững đạo hàm, tích phân ngẫu nhiên (Ito Calculus) và phương trình HJB để có thể chuyển đổi công thức toán trong Phần III thành code C++/Golang/Python.

### Phần III: Trading Strategies (Các chiến lược giao dịch định lượng)
Đây là phần lõi giá trị nhất, nơi toàn bộ toán học được áp dụng vào thực chiến.

- **Chương 6 - 9: Optimal Execution (Thực thi lệnh tối ưu)**
  - Tóm tắt: Xây dựng phương trình cho bài toán thanh lý/mua một lượng lớn tài sản. Giải quyết triệt để sự đánh đổi (trade-off) giữa "Market Impact" (tác động làm giá xấu đi khi đi lệnh nhanh) và "Price Risk" (rủi ro giá quay đầu khi rải lệnh chậm).
  - Ứng dụng: Xây dựng hàm **"Make or Take"**. Thuật toán sẽ tự giải phương trình để quyết định xem ở giây thứ *t*, nó nên "Make" (đặt Limit order chờ khớp) hay "Take" (đặt Market order lấy thanh khoản luôn) là có lợi nhất.

- **Chương 10: Market Making (Tạo lập thị trường)**
  - Tóm tắt: Mở rộng và hoàn thiện mô hình Market Making kinh điển của *Avellaneda-Stoikov (1998)*.
  - Ứng dụng: **(Cực kỳ quan trọng)**. Cuốn sách cung cấp công thức chính xác để tính toán sự lệch giá (Skewness) của Bid/Ask spread dựa trên: Mức độ e ngại rủi ro (Risk Aversion), Biến động thị trường (Volatility) và **Hàng tồn kho (Inventory)**. Nhờ công thức này, Bot Market Making sẽ không bao giờ bị "kẹt hàng" khi thị trường sập đột ngột.

- **Chương 11: Pairs Trading and Statistical Arbitrage (Giao dịch Cặp và Chênh lệch thống kê)**
  - Tóm tắt: Mô hình hóa sự đảo chiều về giá trị trung bình (Mean Reversion) của 2 tài sản có tính đồng tích hợp (Cointegration).
  - Ứng dụng: Viết bot trade Spread (chênh lệch) giữa các cặp tài sản tương quan mạnh (VD: BTC/USDT và BTC/USDC, hoặc BTC phái sinh và BTC Spot).

- **Chương 12: Order Imbalance (Mất cân bằng Sổ lệnh)**
  - Tóm tắt: Sử dụng sự mất cân bằng khối lượng (Imbalance) giữa phe Bid và Ask ở các level đầu tiên để tạo tín hiệu (Signal) dự đoán hướng đi siêu ngắn hạn.
  - Ứng dụng: **(Bản lề cho bot Penny Jumper)**. Áp dụng trực tiếp toán học của chương này, bot của bạn sẽ nhận diện được các bức tường giá (Walls) đang làm mất cân bằng sổ lệnh, từ đó tính toán được xác suất giá bật lên/đập xuống và thực thi lệnh Front-run (chạy trước) trong mili-giây.

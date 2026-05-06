# High-Frequency Trading: A Practical Guide to Algorithmic Strategies and Trading Systems - Irene Aldridge

## Tổng quan (Core Concept)
- **Cốt lõi:** Nếu "Trading and Exchanges" dạy lý thuyết sổ lệnh, "Algorithmic Trading & DMA" dạy cách chia nhỏ lệnh thực thi, thì cuốn sách này là **cẩm nang từ A-Z để xây dựng một hệ thống bot HFT hoàn chỉnh**.
- **Vấn đề:** Để có một bot HFT tạo ra lợi nhuận, bạn không chỉ cần một thuật toán thông minh, mà còn cần kiến trúc hệ thống xử lý dữ liệu tốc độ cao, khả năng quản trị rủi ro tồn kho (inventory) trong vài phần nghìn giây và phương pháp đo lường hiệu suất chính xác.
- **Giải pháp:** Sách cung cấp một quy trình thực tế: từ việc nạp dữ liệu tick-by-tick, tính toán chi phí trượt giá, cho đến việc thiết kế các chiến lược cốt lõi của HFT như Market Making, Statistical Arbitrage và Event-Driven Trading.

---

## Tóm tắt chi tiết, Đúc kết và Thông tin quan trọng (Nhóm theo Phần/Chương)

### Phần 1: Cơ sở hạ tầng, Công nghệ và Sổ lệnh (Chương 1 - 3)
- **Nội dung:** Tổng quan về sự tiến hóa của thị trường điện tử. Sách giải phẫu các đổi mới công nghệ phần cứng và phần mềm đằng sau HFT (như máy chủ Colocation, FPGA) và cơ chế vận hành của Limit Order Book.
- **Đúc kết & Thông tin quan trọng:** Trong HFT, độ trễ (Latency) không chỉ do mạng lưới internet, mà còn do thời gian hệ thống đọc dữ liệu từ RAM/Ổ cứng, hoặc thời gian Garbage Collector (GC) của ngôn ngữ lập trình hoạt động. Việc thiết kế phần mềm bot phải cực kỳ tối ưu về mặt cấp phát bộ nhớ.

### Phần 2: Dữ liệu cao tần, Chi phí và Đánh giá hiệu suất (Chương 4 - 6)
- **Nội dung:** Cách xử lý dữ liệu siêu tần số (High-Frequency Data), phân tích chi phí giao dịch siêu nhỏ và đánh giá năng lực của thuật toán.
- **Đúc kết & Thông tin quan trọng cho Bot:**
  - **Xử lý dữ liệu (Tick Data):** Sổ lệnh biến động hàng nghìn lần mỗi giây. Việc nạp, lưu trữ và parse (phân tích cú pháp) luồng websocket này là một thách thức lớn. Bot cần cấu trúc dữ liệu vòng (ring buffer) hoặc mảng tuyến tính tối ưu để tránh ngẽn cổ chai.
  - **Sharpe Ratio trong HFT:** Không thể dùng công thức đo lường rủi ro/lợi nhuận truyền thống (đo theo ngày/tháng). Tác giả cung cấp cách điều chỉnh các chỉ số để đo lường hiệu suất bot theo từng phút/giây.

### Phần 3: Vận hành doanh nghiệp HFT (Chương 7)
- **Nội dung:** Phân tích mô hình kinh doanh, yêu cầu nguồn vốn và các chi phí liên quan đến việc vận hành một quỹ HFT chuyên nghiệp so với quỹ truyền thống. (Phần này mang tính tham khảo tổng quan vĩ mô).

### Phần 4: Các chiến lược HFT Cốt lõi (Chương 8 - 11)
Đây là phần giá trị nhất dành cho kỹ sư phát triển chiến lược bot (Quants/Devs).

- **Chương 8: Statistical Arbitrage (Kinh doanh chênh lệch giá thống kê)**
  - Tóm tắt: Sử dụng toán thống kê để tìm sự sai lệch giá tạm thời giữa các tài sản có tính tương quan (Ví dụ: ETH và BTC, hoặc LUNA và ANC). 
  - Ứng dụng: Bot phải tính toán ma trận tương quan (co-integration) liên tục trong thời gian thực. Khi spread giữa 2 đồng coin dãn ra vượt ngưỡng chuẩn (Z-score), bot sẽ Long con yếu và Short con mạnh, chờ giá tụ hội lại.

- **Chương 9: Directional Trading Around Events (Giao dịch có hướng dựa trên sự kiện vi mô)**
  - Tóm tắt: Các chiến lược đánh theo động lượng cực ngắn dựa trên thông tin từ Order Book.
  - Ứng dụng: **(Rất liên quan đến bot Penny Jumper)**. Bot đọc luồng Order Book và phát hiện một "bức tường mua" (Buy Wall) cực lớn vừa được đặt. Đoán rằng giá sắp bật lên, thuật toán liền đặt một lệnh mua ngay trên tường mua đó (Penny Jump) để hưởng lợi từ nhịp đẩy giá, sau đó chốt lời vài tick ngay lập tức.

- **Chương 10 & 11: Automated Market Making (Tạo lập thị trường tự động)**
  - Tóm tắt: Chiến lược cốt lõi nhất của HFT. Bot đặt lệnh ở cả hai bên (Bid và Ask) để ăn chênh lệch (Spread).
  - Ứng dụng: Vấn đề lớn nhất của Market Making là **Inventory Risk (Rủi ro ôm hàng)**. Nếu thị trường sập mạnh, lệnh Buy bị khớp liên tục khiến bot ôm đầy hàng đang mất giá. Tác giả hướng dẫn cách xây dựng các **Mô hình Quản lý Hàng tồn kho (Naïve Inventory Models)**. Khi bot ôm quá nhiều coin, nó phải tự động hạ giá Bid (để không mua thêm nữa) và hạ giá Ask (để kích thích người khác mua giúp xả hàng), mục tiêu là luôn giữ trạng thái vị thế (position) xoay quanh mức 0. Mọi bot HFT Market Making đều phải có module tự động dời giá (skewing) dựa trên hàng tồn kho này.

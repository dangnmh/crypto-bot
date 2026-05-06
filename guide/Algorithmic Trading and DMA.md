# Algorithmic Trading and DMA: An introduction to direct access trading strategies - Barry Johnson

## Tổng quan (Core Concept)
- **Cốt lõi:** Không giống như các sách dạy tìm tín hiệu mua/bán (Alpha generation), cuốn sách này hoàn toàn tập trung vào **Execution Algorithms** (Thuật toán thực thi lệnh) và **Direct Market Access** (DMA - Truy cập thị trường trực tiếp). 
- **Vấn đề:** Khi một quỹ lớn muốn mua 10,000 BTC, họ không thể "Market Buy" một lần vì sẽ làm cháy sổ lệnh (Order Book) và tự đẩy giá lên rất cao (Market Impact / Slippage). Hơn nữa, việc để lộ ý định mua khối lượng lớn sẽ kích thích các bot HFT "Penny Jump" (nhảy giá ăn chặn).
- **Giải pháp:** Sách hướng dẫn chi tiết cách chế tạo các thuật toán chia nhỏ lệnh lớn thành hàng ngàn lệnh nhỏ giọt, sử dụng các chiến thuật giấu lệnh (Iceberg, Hidden) và định tuyến thông minh (Smart Order Routing) để gom hàng với giá tốt nhất mà không ai hay biết.

---

## Tóm tắt chi tiết, Đúc kết và Thông tin quan trọng (Nhóm theo Phần)

### Phần I: An overview of trading and markets (Tổng quan về Giao dịch và Thị trường)
- **Các chương:** Chương 1 (Tổng quan), Chương 2 (Cấu trúc vi mô thị trường), Chương 3 (Thị trường thế giới).
- **Tóm tắt & Đúc kết:** DMA là gì? Đó là việc các tổ chức không gọi điện thoại cho môi giới nữa, mà dùng phần mềm nối thẳng API trực tiếp vào lõi khớp lệnh của sàn giao dịch (Exchange) để giảm tối đa độ trễ (latency). Chương này cũng ôn tập lại các nguyên lý của Market Microstructure (tương tự như sách của Larry Harris).
- **Thông tin quan trọng:** Độ trễ trong DMA là sinh tử. Bất kỳ một node trung gian nào cũng làm giảm tính hiệu quả của thuật toán thực thi.

### Phần II: Algorithmic trading and DMA strategies (Chiến lược Algo Trading và DMA)
- **Các chương:** Chương 4 (Các loại lệnh), Chương 5 (Tổng quan các loại thuật toán), Chương 6 (Chi phí giao dịch), Chương 7 (Chiến lược giao dịch tối ưu).
- **Tóm tắt & Đúc kết:** Trái tim của cuốn sách nằm ở phần này. Nó mổ xẻ các thuật toán mà các quỹ Institutional (Tổ chức) sử dụng hằng ngày.
- **Thông tin quan trọng cho Bot:** 
  - **Các thuật toán Lịch trình (Schedule-driven algos):** 
    - **TWAP (Time-Weighted Average Price):** Băm đều lệnh theo thời gian (VD: cứ 1 phút mua 1 BTC).
    - **VWAP (Volume-Weighted Average Price):** Băm lệnh bám sát theo khối lượng quá khứ của thị trường (Lúc thị trường sôi động mua nhiều, lúc ảm đạm mua ít).
    - **POV (Percentage of Volume):** Tham gia linh động (Ví dụ: thuật toán luôn giữ tỷ lệ đóng góp là 10% trên tổng volume đang khớp của thị trường).
  - **TCA (Transaction Cost Analysis):** Một khái niệm bắt buộc phải biết. Mọi lệnh thực thi của bot phải được đo đếm xem bị "Slippage" (trượt giá) bao nhiêu phần trăm so với giá lúc ra quyết định (Arrival Price).

### Phần III: Implementing trading strategies (Triển khai chiến lược giao dịch vào thực tế)
- **Các chương:** Chương 8 (Đặt lệnh), Chương 9 (Chiến thuật thực thi), Chương 10 (Nâng cao chiến lược), Chương 11 (Yêu cầu hạ tầng phần cứng/phần mềm).
- **Tóm tắt & Đúc kết:** Khi đã có thuật toán (như VWAP), bước tiếp theo là ném lệnh vào Order Book như thế nào để không bị "bắt bài".
- **Thông tin quan trọng cho Bot:**
  - **Smart Order Routing (SOR):** Nếu bạn code bot giao dịch đa sàn (Binance, OKX, Bybit...), bạn cần viết module SOR để thuật toán tự quyết định nên "vét" bao nhiêu thanh khoản ở sàn nào để có trung bình giá tốt nhất.
  - **Iceberg Orders & Hidden Orders (Lệnh Tảng Băng & Lệnh Ẩn):** Cách lợi dụng các tính năng của sàn để giấu size (khối lượng) thực sự. 
  - **Thuật toán Dò mìn (Sniffing algos):** Thuật toán chuyên ném các lệnh cực nhỏ (1-2 đô la) vào sổ lệnh để "Ping" xem đằng sau một mức giá có Iceberg Order khủng nào đang ẩn mình không. (Rất hữu ích cho thuật toán HFT phá bĩnh).

### Phần IV: Advanced trading strategies (Chiến lược giao dịch nâng cao)
- **Các chương:** Chương 12 (Giao dịch theo danh mục/Rổ tài sản), Chương 13 (Giao dịch đa tài sản), Chương 14 (Tin tức).
- **Tóm tắt & Đúc kết:** Mở rộng từ việc giao dịch 1 đồng coin sang giao dịch một lúc cả danh mục (Index) hoặc phòng vệ rủi ro chéo.
- **Thông tin quan trọng cho Bot:**
  - Giao dịch theo cặp (Pairs Trading) và Arbitrage không gian/thời gian.
  - Cách thiết kế thuật toán **Event-driven** (Lái theo sự kiện). Tức là bot lắng nghe một luồng dữ liệu tin tức (như News feed, Tweet), xử lý NLP trong mili-giây và bắn lệnh DMA mua/bán ngay lập tức trước khi thị trường kịp hấp thụ tin tức đó.

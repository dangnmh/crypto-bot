# Lộ trình đọc sách HFT (High-Frequency Trading) từ Beginner đến Master

Tài liệu này tổng hợp các cuốn sách cốt lõi nhất về Giao dịch Tần suất Cao (HFT), cấu trúc thị trường (Market Microstructure) và Algorithmic Trading. Các sách được chọn lọc kỹ lưỡng, ưu tiên những nền tảng không bị lỗi thời và các kỹ thuật mô hình hóa hiện đại có thể áp dụng cho cả thị trường truyền thống lẫn Crypto.

---

## Thứ tự đọc khuyến nghị

### Giai đoạn 1: Nhập môn & Nền tảng (Beginner)
1. **Flash Boys: A Wall Street Revolt** - Michael Lewis (2014)
2. **Trading and Exchanges: Market Microstructure for Practitioners** - Larry Harris (2003)

### Giai đoạn 2: Cấu trúc thuật toán & Thực chiến (Intermediate)
3. **Algorithmic Trading and DMA: An introduction to direct access trading strategies** - Barry Johnson (2010)
4. **High-Frequency Trading: A Practical Guide to Algorithmic Strategies and Trading Systems** - Irene Aldridge (2013)

### Giai đoạn 3: Toán học, Mô hình hóa & Chuyên gia (Master)
5. **Algorithmic and High-Frequency Trading** - Álvaro Cartea, Sebastian Jaimungal, José Penalva (2015)
6. **Machine Learning for Algorithmic Trading** - Stefan Jansen (2020)

---

## Tóm tắt chi tiết từng cuốn sách

### 1. Flash Boys: A Wall Street Revolt (Michael Lewis)
- **Cấp độ:** Nhập môn (Không yêu cầu nền tảng kỹ thuật/toán học).
- **Nội dung:** Dựa trên câu chuyện có thật, cuốn sách phơi bày lịch sử hình thành HFT, cuộc chạy đua vũ trang về cáp quang để giảm thiểu độ trễ mạng (latency) tính bằng mili-giây, và cách các quỹ lợi dụng tốc độ để "front-running" lệnh của nhà đầu tư khác.
- **Tại sao nên đọc:** Giúp bạn có cái nhìn tổng quan (bird-eye view) về "vũ trụ HFT", hiểu được tại sao cơ sở hạ tầng (infrastructure) và tốc độ lại là vũ khí tối thượng trước khi đi sâu vào nghiên cứu thuật toán.

### 2. Trading and Exchanges: Market Microstructure for Practitioners (Larry Harris)
- **Cấp độ:** Nhập môn / Trung cấp.
- **Nội dung:** Được giới chuyên môn vinh danh là "Kinh thánh" về cấu trúc thị trường vi mô (Market Microstructure). Sách giải thích cặn kẽ về Order Book (sổ lệnh), sự chênh lệch giá (bid-ask spread), các loại lệnh nâng cao, và bản chất của tính thanh khoản.
- **Tại sao nên đọc:** Tuy xuất bản năm 2003, nhưng các nguyên lý cốt lõi của Order Book không hề thay đổi (đặc biệt đúng với các sàn Crypto hiện tại). Muốn viết bot HFT thành công, việc hiểu cặn kẽ cách sổ lệnh khớp lệnh và các rủi ro liên quan là yếu tố tiên quyết.

### 3. Algorithmic Trading and DMA (Barry Johnson)
- **Cấp độ:** Trung cấp.
- **Nội dung:** Cuốn sách tập trung vào các thuật toán thực thi lệnh (Execution Algorithms) như VWAP, TWAP, POV... Nó hướng dẫn cách các tổ chức đo lường, phân chia và thực thi các lệnh có khối lượng lớn thông qua Direct Market Access (DMA) để giảm thiểu tối đa tác động lên giá (Market Impact).
- **Tại sao nên đọc:** Cung cấp cho bạn tư duy về cách quản lý order, chia nhỏ lệnh và kỹ thuật giấu lệnh trên sàn. Rất hữu ích cho các chiến lược trading muốn che giấu ý đồ giao dịch.

### 4. High-Frequency Trading (Irene Aldridge)
- **Cấp độ:** Trung cấp.
- **Nội dung:** Một cuốn cẩm nang toàn diện kết nối giữa lý thuyết và việc phát triển phần mềm HFT. Sách đề cập đến kiến trúc hệ thống, đánh giá hiệu suất, cũng như các chiến lược HFT phổ biến (Tạo lập thị trường - Market Making, Kinh doanh chênh lệch giá thống kê - Statistical Arbitrage, và giao dịch theo xu hướng vi mô).
- **Tại sao nên đọc:** Đây là bước chuyển tiếp hoàn hảo giúp bạn biết cách thiết kế kiến trúc phần mềm cho bot giao dịch tốc độ cao và cách đánh giá, quản trị rủi ro chuyên biệt trong HFT.

### 5. Algorithmic and High-Frequency Trading (Álvaro Cartea et al.)
- **Cấp độ:** Chuyên gia (Master - Yêu cầu nền tảng Toán học tốt).
- **Nội dung:** Đi sâu vào toán học và định lượng (Quantitative). Sách sử dụng phương trình vi phân ngẫu nhiên (Stochastic Calculus) và điều khiển tối ưu để tạo ra các mô hình toán học cho Order Book. Giải phẫu chi tiết các mô hình Market Making kinh điển (như mô hình Avellaneda-Stoikov) và tối ưu hóa quản lý vị thế (Inventory Management).
- **Tại sao nên đọc:** Đây là tài liệu cốt lõi và không bị lỗi thời nếu bạn muốn xây dựng các chiến lược HFT bằng Toán học định lượng. Nó giúp bạn vượt qua giai đoạn "đoán mò" và chuyển sang dùng toán học để kiếm lời từ nhiễu động giá vi mô.

### 6. Machine Learning for Algorithmic Trading (Stefan Jansen)
- **Cấp độ:** Chuyên gia / Hiện đại (Modern Master).
- **Nội dung:** Sách tập trung vào việc áp dụng Machine Learning (ML) và Deep Learning vào dữ liệu tài chính (đặc biệt là dữ liệu tick-by-tick và Order Book siêu tần số). Bao gồm các thuật toán hiện đại để trích xuất feature từ Order Book nhằm dự báo biến động giá ngắn hạn.
- **Tại sao nên đọc:** Đây là cuốn sách mang tính thời đại (2020), cập nhật xu hướng HFT mới nhất: Kết hợp Tốc độ với Trí tuệ Nhân tạo. Rất phù hợp nếu bạn muốn phát triển các hệ thống HFT dựa trên tín hiệu AI.

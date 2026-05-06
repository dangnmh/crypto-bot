# Trading and Exchanges: Market Microstructure for Practitioners - Larry Harris

## Tổng quan (Core Concept)
- **Cốt lõi:** Cuốn sách là "Kinh thánh" định hình lại cách chúng ta nhìn nhận về thị trường tài chính. Nó không dạy bạn "mua đỉnh bán đáy" dựa trên phân tích kỹ thuật hay cơ bản, mà dạy bạn **cơ chế vận hành thực sự bên dưới của sàn giao dịch** (Market Microstructure).
- **Vấn đề:** Mọi giao dịch đều là một "zero-sum game" (trò chơi có tổng bằng không) về mặt thông tin và tốc độ. Để chiến thắng, bạn phải biết mình đang giao dịch với ai, tại sao họ lại đặt lệnh, và luật chơi (cơ chế khớp lệnh) của sàn là gì.
- **Giải pháp:** Sách phân loại chi tiết các thành phần tham gia thị trường (từ nhà cái, quỹ hưu trí, đến kẻ thao túng), phân tích rủi ro của từng loại lệnh (limit, market, stop), và giải phẫu tính thanh khoản (liquidity) cũng như chênh lệch giá (bid/ask spread).

---

## Tóm tắt chi tiết, Đúc kết và Thông tin quan trọng (Nhóm theo Phần)
*Do cuốn sách mang tính hàn lâm giáo trình với gần 30 chương, nội dung dưới đây được nhóm theo các Phần (Parts) cấu trúc lõi của tác giả.*

### Phần I: The Structure of Trading (Cơ cấu của Giao dịch - Chương 3 đến Chương 7)
- **Các chương chính:** Ngành công nghiệp Giao dịch (Ch.3), Các loại lệnh và Đặc tính (Ch.4), Cấu trúc thị trường (Ch.5), Cơ chế thị trường điều khiển bởi lệnh - Order-Driven (Ch.6), Môi giới (Ch.7).
- **Tóm tắt & Đúc kết:** Đây là nền tảng của toàn bộ cuốn sách. Tác giả định nghĩa rõ ràng sự khác biệt giữa thị trường **Quote-driven** (Thị trường qua tay nhà cái/Dealer - như OTC, Forex) và **Order-driven** (Thị trường đấu giá qua sổ lệnh/Order Book - như chứng khoán hiện đại, Crypto).
- **Thông tin quan trọng cho Bot HFT:** 
  - Hiểu sâu sắc về ưu/nhược điểm của **Limit Order** (có rủi ro không được khớp - execution risk) và **Market Order** (có rủi ro trượt giá - price impact/slippage).
  - Quy tắc ưu tiên của Order Book: **Price-Time Priority** (Ưu tiên Giá trước, Thời gian sau). Đây là lý do tại sao các bot HFT phải đua tốc độ để đứng đầu hàng đợi (queue) tại một mức giá (tick).

### Phần II: The Benefits of Trade (Tại sao người ta giao dịch? - Chương 8 đến Chương 9)
- **Các chương chính:** Tại sao mọi người giao dịch (Ch.8), Thế nào là một thị trường tốt (Ch.9).
- **Tóm tắt & Đúc kết:** Tác giả chia người giao dịch thành 2 nhóm lớn: **Utilitarian Traders** (Giao dịch vì nhu cầu thực tế: phòng vệ rủi ro, cần tiền mặt, tái cơ cấu quỹ) và **Profit-motivated Traders** (Giao dịch để kiếm lời từ giá).
- **Thông tin quan trọng:** Lợi nhuận của các trader HFT/Bot (nhóm 2) thực chất là "thuế" đánh lên sự thiếu kiên nhẫn hoặc nhu cầu thanh khoản của nhóm 1. Một "thị trường tốt" là thị trường có thanh khoản cao, chi phí giao dịch thấp và phản ánh thông tin nhanh chóng.

### Phần III: Speculators (Nhà đầu cơ và Những kẻ săn mồi - Chương 10 đến Chương 12)
- **Các chương chính:** Informed Traders (Ch.10), Order Anticipators (Ch.11), Bluffing & Manipulation (Ch.12).
- **Tóm tắt & Đúc kết:** Phân tích cách những người có thông tin (Informed Traders) kiếm tiền và cách những **Order Anticipators** (Kẻ đi trước lệnh - front-runners, thuật toán HFT) trục lợi bằng cách dự đoán dòng lệnh của người khác.
- **Thông tin quan trọng cho Bot HFT:** 
  - **Order Anticipators (Penny Jumpers/Front-runners):** Đây chính là bản chất của các bot HFT. Chúng không dự đoán giá lên hay xuống theo tin tức, chúng dự đoán *sẽ có một lệnh lớn được ném vào thị trường* và chúng nhảy vào trước để ăn chênh lệch.
  - **Bluffing (Spoofing):** Hành vi đặt lệnh giả (fake limit orders) để lừa các bot khác tưởng rằng có lực mua/bán lớn, sau đó rút lệnh ngay lập tức. Cần cực kỳ cẩn thận với Spoofing khi viết thuật toán phân tích Order Book.

### Phần IV: Liquidity Suppliers (Những người cung cấp thanh khoản - Chương 13 đến Chương 18)
- **Các chương chính:** Dealers (Ch.13), Bid/Ask Spreads (Ch.14), Block Trading (Ch.15), Value-Motivated Traders (Ch.16), Arbitrageurs (Ch.17).
- **Tóm tắt & Đúc kết:** Đây là nhóm kiếm tiền bằng cách cung cấp thanh khoản (Market Makers). Họ ăn chênh lệch Bid-Ask. Tuy nhiên, họ đối mặt với rủi ro lớn nhất là **Adverse Selection** (Lựa chọn đối nghịch) - tức là họ mua của người có tin xấu thật sự và bán cho người có tin tốt thật sự.
- **Thông tin quan trọng cho Bot HFT:** 
  - **Cấu trúc của Spread:** Bid/Ask spread không tự nhiên sinh ra. Nó bao gồm: *Chi phí xử lý lệnh + Rủi ro Adverse Selection + Lợi nhuận độc quyền*.
  - **Arbitrageurs (Chênh lệch giá):** Định tuyến rủi ro không gian (giữa 2 sàn) hoặc thời gian (giữa phái sinh và spot). Arbitrage giúp đồng nhất giá trên toàn thị trường nhưng đòi hỏi hạ tầng siêu tốc.

### Phần V & VI: Origins of Liquidity, Volatility & Transaction Costs (Nguồn gốc của Thanh khoản, Biến động và Chi phí)
- **Tóm tắt & Đúc kết:** Biến động giá (Volatility) được chia làm hai loại: **Fundamental Volatility** (do tin tức thực tế thay đổi giá trị tài sản) và **Transitory Volatility** (do sự mất cân bằng vi mô giữa cung-cầu ngắn hạn trên sổ lệnh).
- **Thông tin quan trọng:** Các chiến lược Mean-Reversion (Giao dịch phục hồi) của HFT hoạt động cực tốt trong môi trường có *Transitory Volatility* cao. Nghĩa là giá bị đẩy đi quá xa do một lệnh Market order quá lớn ăn hết thanh khoản, và HFT bot sẽ đặt cược giá bật ngược trở lại mức cân bằng ngay sau đó.

### Phần VII & VIII: Evaluation and Market Structures (Đánh giá hiệu suất và Kiến trúc vĩ mô)
- **Tóm tắt & Đúc kết:** Đánh giá độ hiệu quả của việc thực thi lệnh. Đo lường "Slippage" (Trượt giá) và "Market Impact" (Tác động thị trường).
- **Thông tin quan trọng:** Tác giả nhấn mạnh tầm quan trọng của **Tick Size** (Bước giá tối thiểu). Tick size càng nhỏ, các HFT bot càng dễ "Penny Jump" (nhảy giá). Ví dụ: Đặt lệnh mua giá 100.01 để hớt tay trên người đặt mua giá 100.00. (Chiến thuật cốt lõi của bài toán Penny Jumper mà bạn đang giải quyết).

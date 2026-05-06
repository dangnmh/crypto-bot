# Bảng Quản Trị Thanh Khoản & Vốn (Liquidity & Capital Management)

Bảng này cung cấp tiêu chuẩn quản trị rủi ro trượt giá (Slippage / Market Impact) dành cho chiến thuật HFT Reversion (Bắn lệnh IOC chớp nhoáng lấy râu nến).

**Nguyên lý (Rule of Thumb):** Lệnh của bạn (Quy mô/Position Size) tuyệt đối **không được vượt quá 10% tổng Volume giao dịch của nến 1 phút** để tránh tự bạn vét sạch Orderbook và làm giá trượt đi khúc xa trước khi thanh khoản sàn được bơm lại.

*(Bảng dưới đây quy đổi từ Quy mô lệnh sang Tiền Gốc thật dựa trên mức giả định **Đòn Bẩy 20x**)*

| Volume 24h Của Đồng Coin (USDT) | Thanh Khoản Nến 1 Phút (Ước Tính) | Quy Mô Lệnh Tối Đa Mức An Toàn (Position Size) | TIỀN GỐC TỐI ĐA ĐƯỢC CHƠI (Margin X20) | Mức Độ Rủi Ro Trượt Giá (Slippage Risk) & Đánh Giá Chi Tiết |
| :--- | :--- | :--- | :--- | :--- |
| **Dưới 1 Triệu $** *(Coin rác cực nhỏ)* | ~$690 | **< $500** | **Dưới $25 USDT** | ⛔ **Tự Sát.** Orderbook mỏng như tờ giấy, bot market maker yếu. Quăng lệnh $20,000 vào đây nến sẽ bay thẳng 5-10% chỉ do lực mua/bán của bạn. |
| **1 Triệu - 5 Triệu $** | ~$2,000 - $3,500 | **$1,000 - $2,000** | **$50 - $100 USDT** | ⚠️ **Cực Rủi Ro.** Spread (Khoảng cách Bid/Ask) giãn rất xa, thanh khoản thưa thớt. Đánh ở mức vốn này vẫn có nguy cơ trượt giá 0.5% - 1%. |
| **5 Triệu - 10 Triệu $** | ~$3,500 - $7,000 | **$3,000 - $5,000** | **$150 - $250 USDT** | 🟡 **Trung Bình - Rủi Ro.** Chấp nhận được nếu MaxPriceDiffPercent cài thật sát (khoảng 0.5%). Mức vốn phù hợp làm mồi câu rải rác nhiều token. |
| **10 Triệu - 50 Triệu $** | ~$7,000 - $35,000 | **$10,000 - $25,000** | **$500 - $1,250 USDT** | 🟢 **An Toàn.** Đủ số lượng orderbook đối ứng để hấp thụ mượt mà các vạch lệnh $10,000. Lượng vốn lý tưởng để cày Funding Rate hằng ngày. |
| **50 Triệu - 100 Triệu $** | ~$35,000 - $70,000 | **$25,000 - $50,000** | **$1,250 - $2,500 USDT** | 🚀 **Rất Tốt.** Thanh khoản dày, spread sát sàn sạt, Slippage vào/ra chỉ tốn cỡ ~0.1%. Tha hồ bung vốn lớn nhưng không lo bị tự làm vỡ giá. |
| **Hơn 100 Triệu $** *(Coin Tóp: BTC, SOL..)*| > $70,000 | **Vô tư** | **Vô tư** | 🛡️ **Tuyệt Đối Ổn Định.** Thanh khoản Vô cực so với vốn cá nhân, không trượt giá nổi. (Tuy nhiên Funding Rate thường rất bé và hiếm khi giật râu mạnh lấp phí). |

---

### Khuyến nghị Quản trị vốn thực tế (Tài khoản 1000$):
Nếu chiến thuật của bạn là đánh Funding bằng số vốn nhàn rỗi **1000 USDT** và dùng đòn bẩy **20x** (Sức mua = 20,000$).
**KHÔNG BAO GIỜ ALL-IN $1000 GỐC VÀO 1 ĐỒNG COIN SHITCOIN ĐƠN LẺ!**

*Nếu một lệnh IOC mang $20,000 Position Size rót thẳng vào 1 đồng xu rác có Volume 24h = 1 Triệu $, lệnh trượt giá 2% ngay khi vào. Chốt Funding bạn dính thêm 1.5% án phí. Tới rạng sáng mai thức dậy, bạn thấy cháy rụi cả $1000 gốc mà chẳng hiểu tại sao.*

**👉 Chiến thuật bắn tỉa khôn ngoan (Diversification Split):** Chọn ra 4 đồng coin có Funding siêu cao trên hệ thống `scanner`. Chia nhỏ vốn ném mỗi đồng vỏn vẹn **250$** Gốc x 20x (= Position size an toàn $5,000). Vừa kiểm soát được tỷ lệ trượt giá (Slippage) của sổ lệnh MEXC, vừa tránh việc chết dính vào 1 dự án pump/dump lừa đảo không chịu lật râu.

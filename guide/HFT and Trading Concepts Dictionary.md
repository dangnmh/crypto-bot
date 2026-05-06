# Từ điển Khái niệm và Định lý Cốt lõi trong Trading & HFT

Tài liệu này tổng hợp các khái niệm (Concepts), định nghĩa (Definitions) và định lý/mô hình (Theorems/Models) mang tính sống còn mà bất kỳ Quants, Trader hay Kỹ sư Bot HFT nào cũng bắt buộc phải nắm vững.

---

## 1. Cơ sở hạ tầng Sổ lệnh (Market Microstructure & Order Book)

- **Limit Order Book (LOB - Sổ lệnh giới hạn):** Nơi tập hợp tất cả các lệnh chờ Mua (Bid) và chờ Bán (Ask) của thị trường tại một thời điểm. HFT chủ yếu là cuộc chiến khai thác thông tin từ LOB.
- **Bid, Ask, Spread:**
  - **Bid:** Giá chào mua cao nhất.
  - **Ask:** Giá chào bán thấp nhất.
  - **Spread (Chênh lệch Bid/Ask):** Khoảng cách giữa Bid và Ask. Spread càng hẹp, thị trường càng thanh khoản. Lợi nhuận của Market Maker chính là ăn Spread này.
- **Mid-price & Micro-price:**
  - **Mid-price:** Giá trung bình giữa Bid và Ask `(Bid + Ask) / 2`.
  - **Micro-price:** Giá trung bình được tính theo trọng số khối lượng (Volume-weighted) của Bid và Ask ở level 1. Nó phản ánh chính xác hơn áp lực mua/bán so với Mid-price.
- **Tick Size & Lot Size:**
  - **Tick Size:** Bước giá tối thiểu mà sàn cho phép đặt lệnh (VD: 0.01 USD). HFT cực kỳ quan tâm đến Tick Size vì nó quyết định việc "Penny Jump" có dễ dàng hay không.
  - **Lot Size:** Khối lượng tối thiểu của một lệnh.
- **Order Imbalance (Sự mất cân bằng Sổ lệnh):** Sự chênh lệch giữa tổng khối lượng chờ mua và khối lượng chờ bán ở các mức giá tốt nhất. Đây là tín hiệu (Signal) quan trọng nhất để các bot dự đoán giá sẽ giật lên hay cắm xuống trong vài mili-giây tới.

---

## 2. Các loại Lệnh (Order Types) và Đặc tính

- **Maker vs Taker:**
  - **Maker:** Người đặt lệnh Limit chờ sẵn trên LOB, cung cấp thanh khoản. Thường được sàn hoàn phí (rebate) hoặc thu phí rất thấp.
  - **Taker:** Người đặt lệnh Market khớp ngay lập tức, lấy đi thanh khoản. Thường phải chịu phí giao dịch cao hơn.
- **Limit Order (Lệnh giới hạn):** Chỉ khớp ở một mức giá cụ thể hoặc tốt hơn. *Rủi ro: Không được khớp lệnh (Execution Risk).*
- **Market Order (Lệnh thị trường):** Khớp ngay lập tức với bất kỳ giá nào có trên sổ lệnh. *Rủi ro: Trượt giá (Slippage).*
- **Các điều kiện thời gian (Time-in-Force):**
  - **FOK (Fill or Kill):** Phải khớp toàn bộ khối lượng ngay lập tức, nếu không sẽ bị hủy.
  - **IOC (Immediate or Cancel):** Khớp được bao nhiêu thì khớp ngay lập tức, phần dư bị hủy.
  - **GTC (Good 'til Canceled):** Tồn tại trên LOB cho đến khi bị hủy hoặc khớp hết.
- **Iceberg & Hidden Orders (Lệnh tảng băng & Lệnh ẩn):** Lệnh khối lượng cực lớn nhưng chỉ hiển thị một phần nhỏ trên LOB (phần nổi của tảng băng) để tránh làm thị trường hoảng loạn và giấu ý đồ của cá mập.

---

## 3. Rủi ro, Chi phí và Hiệu suất (Execution & Impact)

- **Latency (Độ trễ):** Thời gian tính từ khi bot ra quyết định đến khi lệnh được đẩy vào lõi khớp lệnh của sàn. Trong HFT, nó được tính bằng micro-giây (1/1.000.000 giây) hoặc nano-giây.
- **Slippage (Trượt giá):** Sự chênh lệch giữa giá bạn dự định khớp và giá thực tế bạn bị khớp (do trong khoảng thời gian delay, giá đã chạy mất).
- **Market Impact (Tác động thị trường):** Khi bạn ném một lệnh Market quá lớn, bạn "ăn" hết các mức giá trên LOB, tự bạn đẩy giá đi lên (hoặc xuống), làm cho mức giá trung bình bạn khớp bị đắt hơn rất nhiều.
- **Adverse Selection (Lựa chọn đối nghịch):** Cơn ác mộng của Market Makers. Đây là rủi ro khi lệnh Limit của bạn bị khớp bởi một người "biết trước thông tin" (Informed Trader). Ví dụ: Có tin xấu, giá sắp sập, bot của bạn vẫn đang để lệnh Bid chờ mua và bị "xả ngập đầu". 
- **TCA (Transaction Cost Analysis):** Phân tích đo lường chi phí giao dịch ẩn (Slippage + Market Impact) so với mức giá ban đầu (Arrival Price).

---

## 4. Các chiến lược và Chiến thuật HFT (HFT Strategies)

- **Market Making (Tạo lập thị trường):** Liên tục đặt lệnh ở cả hai bên Bid và Ask để thu lợi nhuận từ Spread. Yêu cầu tính toán xác suất khớp lệnh và kiểm soát hàng tồn kho cực kỳ gắt gao.
- **Inventory Risk & Skewing (Rủi ro Tồn kho & Lệch giá):** 
  - *Inventory Risk:* Rủi ro ôm quá nhiều Coin khi thị trường sập hoặc bay mạnh.
  - *Skewing:* Kỹ thuật dời giá (dời Bid/Ask). Khi bot ôm quá nhiều Long (tồn kho dương), nó lập tức dời giá Ask thấp xuống để dụ người khác mua, và dời Bid thấp xuống để tránh mua thêm.
- **Front-running / Penny Jumping:** Đánh hơi thấy một lệnh Limit cực lớn sắp đẩy vào (hoặc đang nằm chờ), bot HFT nhanh tay đặt lệnh mua cao hơn 1 tick (penny) để được ưu tiên khớp trước, chờ giá bị lệnh lớn kia ủn lên rồi chốt lời.
- **Statistical Arbitrage (Chênh lệch giá thống kê):** Giao dịch dựa trên sự chênh lệch giá trị tạm thời của 2 hoặc nhiều tài sản có tính tương quan (Cointegration). (VD: Spread giữa Spot và Futures dãn ra quá mức).
- **Spoofing / Layering (Thao túng LOB):** (Bất hợp pháp ở thị trường truyền thống, nhưng phổ biến ở Crypto). Đặt một lệnh mua khối lượng "khổng lồ" nhưng không có ý định khớp, nhằm dụ các bot khác tưởng là có lực mua mạnh để đẩy giá lên, sau đó lập tức hủy lệnh "khổng lồ" này.

---

## 5. Các Định lý và Mô hình Toán học Cốt lõi (Quantitative Models)

- **Mô hình Avellaneda-Stoikov (1998):**
  - *Định nghĩa:* Định lý nền tảng cho mọi bot Market Making. Cung cấp công thức toán học chính xác để tính Khoảng cách dự trữ (Reservation Price) và Độ rộng Spread tối ưu dựa trên biến động thị trường (Volatility) và Hàng tồn kho (Inventory).
  - *Cốt lõi:* Càng ôm nhiều hàng, giá Reservation càng lệch xa giá Mid-price hiện tại.
- **Poisson Process (Quá trình Poisson):** 
  - *Định nghĩa:* Mô hình xác suất thường được dùng để lập mô hình "tốc độ đến" (arrival rate) của các lệnh Market đập vào sổ lệnh. Dùng để dự đoán xác suất lệnh Limit của bạn có được khớp trong X giây tới hay không.
- **Hamilton-Jacobi-Bellman (HJB) Equation:**
  - *Định nghĩa:* Phương trình toán học trong Điều khiển tối ưu ngẫu nhiên (Stochastic Optimal Control).
  - *Ứng dụng:* Dùng để tìm ra "chính sách tối ưu" (Optimal Policy) trong mọi chiến lược HFT (như bao giờ nên ném lệnh Market, bao giờ nên ném lệnh Limit để tối đa hóa lợi nhuận mà rủi ro thấp nhất).
- **Các thuật toán thực thi theo lịch trình (Execution Algorithms):**
  - **TWAP (Time-Weighted Average Price):** Thuật toán chia nhỏ một lệnh khổng lồ và thực thi đều đặn theo từng khoảng thời gian (VD: 1 phút 1 lần).
  - **VWAP (Volume-Weighted Average Price):** Thuật toán chia nhỏ lệnh và thực thi thuận theo khối lượng giao dịch thật của thị trường (Thị trường volume to thì mua nhiều, volume nhỏ thì mua ít) để giấu vết tối đa.
- **Mean Reversion (Đảo chiều trung bình):** Định lý giả định rằng giá cả hoặc spread giữa các tài sản sẽ luôn có xu hướng bị kéo về đường trung bình lịch sử của nó sau một cú sốc ngắn hạn. Đa số các bot Arbitrage đều dựa trên nguyên lý này.
- **Almgren-Chriss Model (2001):**
  - *Định nghĩa:* Mô hình tối ưu hóa thực thi lệnh. Giải quyết bài toán: "Thanh lý X coin trong T giây, hàm chi phí tối thiểu là gì?".
  - *Cốt lõi:* Chia Market Impact thành **Temporary Impact** (tạm thời, giá bật lại) và **Permanent Impact** (vĩnh viễn, giá không bao giờ quay lại). Trader phải cân bằng giữa rủi ro giá (chậm) và Market Impact (nhanh).
- **Kyle's Lambda (Kyle, 1985):**
  - *Định nghĩa:* Đo lường **độ nhạy giá đối với dòng lệnh** (Price Impact per unit of order flow). Lambda cao = thị trường kém thanh khoản, một lệnh nhỏ cũng đẩy giá mạnh.
  - *Ứng dụng:* Bot dùng Kyle's Lambda để quyết định kích cỡ lệnh tối ưu tại mỗi thời điểm.
- **Geometric Brownian Motion (GBM - Chuyển động Brown hình học):**
  - *Định nghĩa:* Mô hình toán học giả định giá tài sản biến động theo một quá trình ngẫu nhiên liên tục, với phần drift (xu hướng) và phần biến động ngẫu nhiên (diffusion).
  - *Ứng dụng:* Nền tảng cho mọi mô hình định giá quyền chọn (Black-Scholes) và mô phỏng Monte Carlo trong HFT.
- **Ornstein-Uhlenbeck Process (OU Process):**
  - *Định nghĩa:* Quá trình ngẫu nhiên có tính Mean-Reverting (tự quay về trung bình). Khác với GBM (đi lang thang), OU bị "kéo" về giá trị trung bình bởi một lực hồi phục.
  - *Ứng dụng:* Mô hình hóa Spread giữa 2 tài sản trong Pairs Trading/Statistical Arbitrage. Khi spread lệch quá xa, OU Process dự đoán nó sẽ co lại.

---

## 6. Thanh khoản (Liquidity Concepts)

- **Liquidity (Thanh khoản):** Khả năng mua/bán một tài sản với khối lượng lớn mà không làm thay đổi đáng kể giá. Thanh khoản cao = Spread hẹp, sổ lệnh dày, trượt giá thấp.
- **Depth (Độ sâu sổ lệnh):** Tổng khối lượng lệnh Limit đang chờ ở các mức giá xung quanh Best Bid/Ask. Depth càng dày, Market Impact càng nhỏ.
- **Resilience (Khả năng phục hồi):** Tốc độ mà sổ lệnh "lấp đầy" lại sau khi bị một lệnh Market lớn quét sạch. Thị trường Resilient = giá bật lại nhanh sau cú sốc.
- **Tightness (Độ chặt):** Spread càng hẹp = Tightness càng cao. Đây là thước đo chi phí tức thời cho việc chuyển đổi vị thế.
- **Liquidity Provider vs Liquidity Consumer:**
  - *Provider (Maker):* Bot Market Making cung cấp thanh khoản, đặt lệnh 2 bên.
  - *Consumer (Taker):* Bot Aggressive (như Penny Jumper khi cướp thanh khoản) lấy đi thanh khoản.
- **Dark Pool (Bể giao dịch ngầm):** Sàn giao dịch tư nhân nơi các lệnh lớn được khớp ẩn danh, không hiển thị trên LOB công khai. Tránh bị front-run. (Chủ yếu ở thị trường truyền thống, Crypto chưa phổ biến).
- **Toxicity / VPIN (Volume-Synchronized Probability of Informed Trading):**
  - *Định nghĩa:* Chỉ số đo lường xác suất dòng lệnh đang bị chi phối bởi Informed Traders.
  - *Ứng dụng:* Khi VPIN tăng cao, Market Maker nên mở rộng Spread hoặc rút lui khỏi thị trường để tránh bị Adverse Selection.

---

## 7. Hạ tầng Kỹ thuật HFT (Infrastructure & Technology)

- **Colocation (Đặt máy chủ cùng vị trí):** Thuê đặt máy chủ bot ngay bên trong Data Center của sàn giao dịch để giảm Latency xuống micro-giây. Đây là khoản đầu tư cơ bản nhất trong HFT truyền thống.
- **FPGA (Field-Programmable Gate Array):** Chip phần cứng lập trình được, xử lý dữ liệu nhanh hơn CPU thông thường hàng chục lần vì bỏ qua cả hệ điều hành. Các quỹ HFT hàng đầu dùng FPGA để parse dữ liệu Order Book.
- **Kernel Bypass / DPDK:** Kỹ thuật truyền nhận gói tin mạng trực tiếp từ card mạng vào ứng dụng, bỏ qua lớp TCP/IP stack của hệ điều hành để giảm thêm vài micro-giây latency.
- **Lock-free Data Structures (Cấu trúc dữ liệu không khóa):** Dùng Ring Buffer, Atomic Operations thay cho Mutex/Lock truyền thống để tránh các goroutine/thread phải chờ đợi nhau khi cập nhật trạng thái sổ lệnh.
- **Garbage Collection (GC) Pause:** Trong các ngôn ngữ có GC (Go, Java, C#), GC có thể tạo ra "stop-the-world" pause (tạm dừng toàn bộ chương trình) từ vài trăm micro-giây đến vài mili-giây. Đây là kẻ thù ngầm của bot HFT viết bằng Go. Giải pháp: tối ưu cấp phát bộ nhớ (zero-alloc), dùng sync.Pool, hoặc pre-allocate buffers.
- **Smart Order Router (SOR):** Module phần mềm tự động phân bổ một lệnh lớn sang nhiều sàn/venue khác nhau để tìm giá tốt nhất và giảm Market Impact.
- **Event-Driven Architecture (Kiến trúc hướng sự kiện):** Thiết kế hệ thống bot theo mô hình Pub/Sub. Mỗi module (OB Builder, Wall Detector, Strategy) hoạt động như một subscriber lắng nghe events, giúp tách biệt logic và giảm latency pipeline.

---

## 8. Khái niệm Đặc thù Crypto (Crypto-Specific Concepts)

- **Funding Rate (Tỷ lệ tài trợ):** Khoản phí định kỳ (thường 8 giờ/lần) mà bên Long trả cho bên Short (hoặc ngược lại) trên thị trường Futures Perpetual. Khi Funding Rate cực dương (> 0.1%), có cơ hội Funding Arbitrage (Short Futures + Long Spot để ăn chênh lệch).
- **Perpetual Futures vs Quarterly Futures:**
  - *Perpetual:* Hợp đồng tương lai không có ngày hết hạn. Giá được neo sát Spot thông qua cơ chế Funding Rate.
  - *Quarterly:* Có ngày đáo hạn cụ thể. Thường chênh lệch giá (Basis) so với Spot, tạo cơ hội Basis Trading.
- **Basis & Cash-and-Carry Arbitrage:**
  - *Basis:* Chênh lệch giữa giá Futures và giá Spot. `Basis = Futures - Spot`.
  - *Cash-and-Carry:* Mua Spot + Short Futures khi Basis dương (contango), chờ Basis co lại về 0 khi gần đáo hạn.
- **Liquidation Cascade (Hiệu ứng domino thanh lý):** Khi giá giảm mạnh, các vị thế Long có đòn bẩy cao bị thanh lý hàng loạt, tạo thêm áp lực bán, kéo giá giảm sâu hơn, thanh lý thêm nhiều vị thế khác. Đây là đặc trưng cực kỳ nguy hiểm của Crypto.
- **MEV (Maximal Extractable Value):** (Chủ yếu DeFi/On-chain). Giá trị mà validator/miner có thể trích xuất bằng cách sắp xếp lại, chèn thêm hoặc loại bỏ giao dịch trong một block. Bao gồm: Sandwich Attack, Front-running on-chain, Back-running.
- **Maker/Taker Fee Tiers:** Các sàn Crypto (Binance, OKX, Bybit) có hệ thống phí bậc thang. Volume giao dịch 30 ngày càng cao, phí càng giảm. Một số sàn hoàn phí (negative fee / rebate) cho Makers ở tier cao nhất. Điều này ảnh hưởng trực tiếp đến khả năng sinh lời của bot Market Making.
- **Websocket Depth Stream:** Luồng dữ liệu real-time mà sàn Crypto cung cấp qua Websocket. Bao gồm Top-of-Book (BBO), Depth snapshot, và Incremental updates (diff). Bot HFT phải tự xây dựng Local Order Book từ luồng này.

---

## 9. Quản trị Rủi ro (Risk Management)

- **Position Limit (Giới hạn vị thế):** Quy định số lượng coin/contract tối đa mà bot được phép nắm giữ tại một thời điểm. Đây là "cầu chì" an toàn số 1 cho mọi bot HFT.
- **Max Drawdown (Mức sụt giảm tối đa):** Khoảng sụt giảm lớn nhất từ đỉnh lợi nhuận đến đáy lỗ. Dùng làm tiêu chí "kill switch" (tắt bot khẩn cấp).
- **Kill Switch / Circuit Breaker:**
  - *Kill Switch:* Cơ chế tự động tắt bot ngay lập tức khi phát hiện lỗ vượt ngưỡng, hoặc khi hệ thống gặp sự cố kết nối.
  - *Circuit Breaker:* Tạm dừng giao dịch khi biến động vượt ngưỡng bất thường (VD: giá giảm 10% trong 1 phút). Tránh bot giao dịch trong môi trường hỗn loạn.
- **Sharpe Ratio:** Tỷ lệ lợi nhuận vượt trội (so với lãi suất phi rủi ro) chia cho độ lệch chuẩn. Sharpe Ratio > 2 được coi là tốt. Trong HFT, do tần suất giao dịch cao, Sharpe Ratio thường được điều chỉnh theo đơn vị thời gian siêu nhỏ (phút/giờ thay vì ngày).
- **Value at Risk (VaR - Giá trị chịu rủi ro):** Ước tính số tiền tối đa có thể mất trong một khoảng thời gian với một mức xác suất cho trước (VD: "VaR 95% = $500" nghĩa là 95% khả năng bạn sẽ không lỗ quá $500 trong ngày).
- **PnL (Profit and Loss - Lãi Lỗ):**
  - *Realized PnL:* Lãi/lỗ thực tế đã chốt khi đóng vị thế.
  - *Unrealized PnL:* Lãi/lỗ tạm tính trên các vị thế đang mở.
  - *Net PnL:* Lãi/lỗ ròng sau khi trừ tất cả phí giao dịch, funding fee, slippage.
- **Fat Finger / Rogue Algorithm:** Sự cố khi bot gửi nhầm lệnh với khối lượng hoặc giá sai lệch kinh hoàng (VD: mua BTC giá $1 hoặc bán giá $1.000.000). Giải pháp: Luôn có lớp validation (kiểm tra tính hợp lý) trước khi gửi lệnh ra sàn.

---

## 10. Tín hiệu và Phân tích Order Book (Order Book Signals & Patterns)

- **Book Pressure (Áp lực sổ lệnh):** Tỷ lệ giữa tổng khối lượng Bid và tổng khối lượng Ask ở N mức giá đầu tiên. `Pressure = Σ(Bid Volume) / Σ(Ask Volume)`. Pressure > 1 = Áp lực mua mạnh hơn.
- **Trade Flow Imbalance (Mất cân bằng dòng lệnh khớp):** Tỷ lệ giữa khối lượng mua chủ động (Market Buy) và bán chủ động (Market Sell) trong một cửa sổ thời gian ngắn. Dùng kết hợp với Order Imbalance để tăng độ chính xác tín hiệu.
- **Wall (Tường lệnh):** Một lệnh Limit có khối lượng cực lớn (gấp hàng chục lần khối lượng trung bình ở các mức giá khác) đứng chặn trên sổ lệnh. Wall có thể là thật (institutional order) hoặc giả (spoofing).
- **Wall Trust Score (Điểm tin cậy tường lệnh):** Chỉ số đánh giá xem một Wall có đáng tin cậy hay không dựa trên: tuổi thọ tồn tại, tần suất bị sửa đổi/hủy, kích thước so với trung bình. (Khái niệm đã được phát triển trong bot Penny Jumper).
- **Queue Position (Vị trí hàng đợi):** Vị trí ưu tiên của lệnh Limit trong hàng đợi tại một mức giá. Lệnh đến trước được khớp trước (FIFO). Bot HFT phải ước lượng Queue Position để dự đoán xác suất khớp lệnh.
- **Sweep Detection (Phát hiện quét sổ lệnh):** Nhận diện thời điểm một lệnh Market cực lớn đang "quét" qua nhiều mức giá liên tiếp trên LOB. Đây là tín hiệu momentum mạnh, thường kèm theo sự dịch chuyển giá đáng kể.
- **Tick Rule (Quy tắc xác định hướng giao dịch):** Cách xác định một giao dịch vừa khớp là "mua chủ động" hay "bán chủ động" dựa trên việc giá khớp gần Bid (Sell) hay gần Ask (Buy). Cần thiết khi sàn không cung cấp trực tiếp thông tin aggressor side.
- **Volume Profile / VPVR (Volume Profile Visible Range):** Biểu đồ phân bổ khối lượng giao dịch theo từng mức giá (thay vì theo thời gian). Giúp xác định các vùng giá có nhiều giao dịch nhất (High Volume Node - HVN) và vùng ít giao dịch (Low Volume Node - LVN). Giá có xu hướng "dừng lại" tại HVN và "bay nhanh" qua LVN.

---

## 11. Biến động và Nhận diện Chế độ Thị trường (Volatility & Regime Detection)

- **Realized Volatility (Biến động thực tế):** Đo biến động thực sự bằng cách tính độ lệch chuẩn (standard deviation) của log-returns trên dữ liệu tần suất cao (tick/phút). Khác với Implied Volatility (biến động kỳ vọng từ giá quyền chọn).
- **Volatility Clustering (Hiện tượng biến động tụ nhóm):** Biến động mạnh sinh ra biến động mạnh, biến động yếu theo sau biến động yếu. Đây là hiện tượng thực nghiệm phổ quát trên mọi thị trường tài chính. Bot HFT cần nhận ra trạng thái "high-vol" để mở rộng Spread hoặc giảm size.
- **GARCH (Generalized Autoregressive Conditional Heteroskedasticity):**
  - *Định nghĩa:* Mô hình kinh tế lượng dự đoán biến động tương lai dựa trên biến động quá khứ và lỗi dự báo quá khứ.
  - *Ứng dụng:* Dùng để ước lượng Volatility cho mô hình Avellaneda-Stoikov (Market Making), giúp bot tự động co giãn Spread theo biến động thị trường.
- **Regime Detection (Nhận diện chế độ thị trường):**
  - *Định nghĩa:* Phân loại thị trường đang ở chế độ nào: **Trending** (xu hướng mạnh), **Mean-Reverting** (dao động quanh trung bình), hay **High-Volatility/Crisis** (biến động cực đoan).
  - *Ứng dụng:* Bot HFT Market Making chỉ nên hoạt động trong chế độ Mean-Reverting. Khi phát hiện chuyển sang Trending hoặc Crisis, bot cần tắt ngay hoặc chuyển sang chiến lược phòng thủ.
- **Hidden Markov Model (HMM - Mô hình Markov ẩn):**
  - *Định nghĩa:* Mô hình xác suất trong đó trạng thái thị trường (regime) là "ẩn" và không quan sát trực tiếp được; chỉ có thể suy ra từ dữ liệu giá/volume quan sát được.
  - *Ứng dụng:* Dùng để xây dựng module Regime Detection tự động. HMM ước lượng xác suất thị trường đang ở trạng thái nào và xác suất chuyển đổi giữa các trạng thái.
- **Intraday Seasonality / U-Shape Pattern:** Biến động và Volume giao dịch thường cao nhất vào đầu phiên (open) và cuối phiên (close), tạo thành hình chữ U trong ngày. Trong Crypto (24/7), pattern này tương ứng với giờ mở cửa của các thị trường lớn (US, Asia, EU). Bot cần điều chỉnh tham số (Spread, aggressiveness) theo khung giờ.

---

## 12. Mô hình Dòng lệnh Nâng cao (Advanced Order Flow Models)

- **Hawkes Process (Quá trình Hawkes):**
  - *Định nghĩa:* Mô hình xác suất tự kích thích (self-exciting). Mỗi sự kiện (một lệnh Market đến) làm tăng xác suất các sự kiện tiếp theo xảy ra. Khác với Poisson (tốc độ đến cố định), Hawkes cho phép mô hình hóa hiện tượng "bùng nổ" lệnh (bursts).
  - *Ứng dụng:* Mô hình hóa Liquidation Cascade trong Crypto. Khi một lệnh thanh lý lớn xảy ra, nó kích hoạt thêm nhiều lệnh thanh lý khác, tạo thành chuỗi phản ứng dây chuyền.
- **PIN Model (Probability of Informed Trading):**
  - *Định nghĩa:* Ước tính tỷ lệ giao dịch trong thị trường đến từ Informed Traders (người có thông tin) so với Noise Traders (người giao dịch ngẫu nhiên).
  - *Ứng dụng:* Khi PIN cao, Market Maker nên rút lui hoặc mở rộng Spread vì rủi ro Adverse Selection tăng vọt.
- **Glosten-Milgrom Model (1985):**
  - *Định nghĩa:* Mô hình nền tảng giải thích tại sao Bid-Ask Spread tồn tại. Market Maker đối mặt với 2 loại đối tác: Informed (biết tin) và Uninformed (không biết tin). Để bù đắp khoản lỗ khi giao dịch với Informed Traders, Market Maker buộc phải mở rộng Spread.
  - *Cốt lõi:* Spread = Phí bảo hiểm chống lại Adverse Selection. Informed Traders càng nhiều → Spread càng rộng.
- **Trade Aggressor Classification (Phân loại bên chủ động):** Xác định mỗi giao dịch khớp là do bên Mua hay bên Bán chủ động tấn công. Phương pháp: **Lee-Ready Algorithm** (so sánh giá khớp với Mid-price + Tick Rule). Cần thiết để tính chính xác Order Flow Imbalance.
- **Order Flow Toxicity (Độ độc hại của dòng lệnh):** Khi dòng lệnh bị chi phối bởi Informed Traders, nó trở nên "độc hại" cho Market Makers. Đo bằng VPIN hoặc PIN. Khi toxicity vượt ngưỡng → Kill Switch cho module Market Making.

---

## 13. Backtest và Mô phỏng (Backtesting & Simulation)

- **Backtesting (Kiểm tra ngược):** Chạy thuật toán trên dữ liệu lịch sử để đánh giá hiệu suất. Đây là bước bắt buộc trước khi triển khai bot với tiền thật.
- **Look-Ahead Bias (Thiên lệch nhìn trước):** Lỗi nghiêm trọng khi backtest vô tình sử dụng thông tin từ tương lai (VD: dùng giá đóng cửa để ra quyết định mua lúc mở cửa). Kết quả backtest sẽ đẹp giả tạo.
- **Survivorship Bias (Thiên lệch sống sót):** Backtest chỉ trên các coin/cổ phiếu còn tồn tại đến hôm nay, bỏ qua những đồng đã bị hủy niêm yết (delisted). Kết quả bị tô hồng vì chỉ test trên "kẻ thắng cuộc".
- **Overfitting (Quá khớp):** Thuật toán bị tối ưu quá mức cho dữ liệu quá khứ cụ thể, mất khả năng tổng quát hóa cho dữ liệu tương lai. Dấu hiệu: Backtest lãi cực đẹp nhưng live trading lỗ ngay.
- **Walk-Forward Analysis (Phân tích tiến về phía trước):** Phương pháp chống Overfitting. Chia dữ liệu thành nhiều đoạn: tối ưu tham số trên đoạn (In-Sample), kiểm tra trên đoạn kế tiếp (Out-of-Sample), rồi trượt cửa sổ về phía trước.
- **Paper Trading / Simulated Trading:** Chạy bot với dữ liệu thực thời gian thực (real-time) nhưng không gửi lệnh thật ra sàn. Dùng để kiểm tra logic và hạ tầng trước khi dùng tiền thật.
- **Monte Carlo Simulation:** Tạo hàng nghìn kịch bản giá ngẫu nhiên dựa trên phân phối thống kê để stress-test thuật toán trong nhiều điều kiện thị trường khác nhau.
- **Latency Simulation:** Khi backtest HFT, cần mô phỏng cả độ trễ mạng, thời gian xử lý và GC pause. Nếu không, backtest sẽ giả định bot phản hồi tức thời (0 latency) — điều không bao giờ đúng trong thực tế.

---

## 14. Chỉ số Đo lường Hiệu suất (Performance Metrics)

- **Win Rate (Tỷ lệ thắng):** Phần trăm giao dịch có lãi trên tổng số giao dịch. Bot HFT Market Making thường có Win Rate rất cao (>60%) nhưng lãi mỗi lệnh rất nhỏ.
- **Profit Factor:** `Tổng lãi / Tổng lỗ`. Profit Factor > 1.5 được coi là tốt. Dưới 1.0 = bot đang lỗ ròng.
- **Sortino Ratio:** Tương tự Sharpe Ratio nhưng chỉ tính biến động của phần lỗ (downside deviation) thay vì toàn bộ biến động. Phù hợp hơn để đánh giá bot vì trader chỉ lo biến động hướng xuống.
- **Calmar Ratio:** `Lợi nhuận trung bình hàng năm / Max Drawdown`. Đo lường lợi nhuận trên đơn vị rủi ro sụt giảm. Calmar > 3 là xuất sắc.
- **Implementation Shortfall (Chi phí triển khai):** Chênh lệch giữa lợi nhuận lý thuyết (nếu khớp ngay tại giá quyết định) và lợi nhuận thực tế (sau slippage, market impact, phí). Đây là thước đo chính xác nhất cho chất lượng thực thi lệnh của bot.
- **Fill Rate (Tỷ lệ khớp lệnh):** Phần trăm lệnh Limit được khớp thành công. Fill Rate thấp = bot đặt lệnh quá xa giá hiện tại hoặc thanh khoản thị trường quá thấp.
- **Cancel-to-Trade Ratio:** Tỷ lệ giữa số lệnh bị hủy và số lệnh thực sự được khớp. Tỷ lệ quá cao có thể bị sàn đánh dấu là Spoofing. Cần giữ ở mức hợp lý.
- **Holding Period (Thời gian nắm giữ trung bình):** Bot HFT thường có Holding Period từ vài giây đến vài phút. Nếu Holding Period kéo dài hàng giờ, bot có thể đang bị "kẹt hàng" (Inventory stuck).

# Tổng Hợp Chiến Thuật Trading — Sắp Xếp Theo Vốn

> Ước tính vốn tối thiểu để chiến thuật **có ý nghĩa thực tế** (không tính phí infra/server).
> Bối cảnh: Crypto perpetual futures + spot, chủ yếu trên CEX (MEXC, Binance, Bybit).
> **Lợi nhuận**: Ước tính % lợi nhuận trên vốn mỗi tháng trong điều kiện thị trường bình thường, giả định chiến thuật được thực thi đúng.

---

## Tier 1 — Vốn Cực Thấp ($0 – $500)

| # | Chiến thuật | Mô tả | Vốn tối thiểu | Lợi nhuận/tháng | Độ khó | Rủi ro |
|---|---|---|---|---|---|---|
| 1 | **Airdrop Farming** | Tích lũy hoạt động on-chain để nhận airdrop, hedge rủi ro | $0 – $100 | 🎰 Không cố định (0% hoặc 1000%+) | ⭐⭐ | Thấp |
| 2 | **Social Sentiment Trading** | Trade dựa trên sentiment Twitter/Telegram/Reddit | $50+ | 10–30% | ⭐⭐ | Trung bình |
| 3 | **Fear & Greed Index** | Entry khi index ở extreme fear/greed | $50+ | 5–15% | ⭐ | Trung bình |
| 4 | **Influencer Front-Running** | Mua trước khi KOL shill token *(gray zone)* | $50+ | 20–100%+ | ⭐⭐ | Cao |
| 5 | **DCA (Dollar Cost Averaging)** | Mua đều đặn theo thời gian để giảm impact của timing | $100+ | 3–10% (dài hạn) | ⭐ | Thấp |
| 6 | **Trend Following** | Entry theo hướng trend, dùng MA/EMA crossover | $100+ | 5–20% | ⭐⭐ | Trung bình |
| 7 | **Mean Reversion** | Entry ngược trend khi giá lệch xa khỏi mean (Bollinger, RSI) | $100+ | 5–15% | ⭐⭐ | Trung bình |
| 8 | **Breakout Trading** | Entry khi giá phá vỡ support/resistance với volume | $100+ | 10–30% | ⭐⭐ | Trung bình |
| 9 | **Grid Trading** | Đặt lưới lệnh mua/bán cách đều nhau trong range | $200+ | 3–10% (sideways) | ⭐⭐ | Trung bình |
| 10 | **Martingale / Anti-Martingale** | Tăng/giảm size sau mỗi lần thua/thắng | $300+ | 10–50% hoặc -100% | ⭐⭐ | **Rất cao** |

---

## Tier 2 — Vốn Thấp ($500 – $5,000)

| # | Chiến thuật | Mô tả | Vốn tối thiểu | Lợi nhuận/tháng | Độ khó | Rủi ro |
|---|---|---|---|---|---|---|
| 11 | **Funding Sniping** ⚡ | Giữ vị thế chỉ qua snapshot funding (mỗi 8h) rồi đóng ngay | $500+ | 2–8% (ổn định) | ⭐⭐⭐ | Trung bình |
| 12 | **Funding Reversion** ⚡ | Entry ngược hướng khi funding rate cực đoan, đánh cược giá revert | $500+ | 10–40% | ⭐⭐⭐⭐ | Trung bình–Cao |
| 13 | **Listing/Delisting Sniping** ⚡ | Trade khi Binance announce listing hoặc delist token | $500+ | 🎰 Event-based (20–50%/event) | ⭐⭐⭐⭐ | Cao |
| 14 | **Fork/Upgrade Trading** | Trade quanh sự kiện hard fork hoặc network upgrade | $500+ | 🎰 Event-based (5–20%/event) | ⭐⭐⭐ | Trung bình |
| 15 | **Momentum Ignition** | Phát hiện momentum burst sớm qua volume spike + price acceleration | $500+ | 10–30% | ⭐⭐⭐⭐ | Cao |
| 16 | **Stop Hunt / Stop Run** | Đẩy giá qua vùng stop-loss phổ biến rồi đảo chiều | $500+ | 10–25% | ⭐⭐⭐ | Cao |
| 17 | **Order Book Imbalance** | Đo chênh lệch volume bid/ask để dự đoán hướng giá ngắn hạn | $500+ | 5–15% | ⭐⭐⭐⭐ | Trung bình |
| 18 | **Front Running / Penny Jumping** | Đặt lệnh trước wall lớn trên order book | $500+ | 5–15% | ⭐⭐⭐ | Trung bình |
| 19 | **Absorption Detection** | Phát hiện wall đang hấp thụ volume → tín hiệu đảo chiều | $500+ | 5–20% | ⭐⭐⭐⭐ | Trung bình |
| 20 | **VWAP Reversion** | Trade quanh VWAP, mua dưới / bán trên | $500+ | 3–12% | ⭐⭐⭐ | Trung bình |
| 21 | **CPI/FOMC News Trading** | Entry trước/sau announcement macro data | $1,000+ | 🎰 Event-based (5–30%/event) | ⭐⭐⭐ | Cao |
| 22 | **Token Unlock / Vesting** ⚡ | Short trước khi lượng lớn token được unlock (supply shock) | $1,000+ | 🎰 Event-based (10–30%/event) | ⭐⭐⭐ | Trung bình–Cao |
| 23 | **Squeeze Trading** | Exploit short/long squeeze khi funding + OI cực đoan | $1,000+ | 15–50% | ⭐⭐⭐⭐ | Cao |
| 24 | **Liquidation Heatmap** | Dùng OI + leverage data để dự đoán vùng liquidation | $1,000+ | 5–20% | ⭐⭐⭐⭐ | Trung bình |
| 25 | **Whale Wallet Tracking** | Copy trade theo ví của smart money / whale | $1,000+ | 5–20% | ⭐⭐⭐ | Trung bình |
| 26 | **Iceberg Detection** | Phát hiện lệnh ẩn qua pattern khớp lệnh bất thường | $2,000+ | 5–15% | ⭐⭐⭐⭐⭐ | Trung bình |
| 27 | **Pairs Trading** | Long token A + Short token B khi correlation lệch | $2,000+ | 3–10% (ổn định) | ⭐⭐⭐⭐ | Trung bình |

---

## Tier 3 — Vốn Trung Bình ($5,000 – $30,000)

| # | Chiến thuật | Mô tả | Vốn tối thiểu | Lợi nhuận/tháng | Độ khó | Rủi ro |
|---|---|---|---|---|---|---|
| 28 | **DEX-CEX Arb** | Exploit chênh lệch giá giữa sàn phi tập trung và tập trung | $5,000+ | 3–10% | ⭐⭐⭐⭐ | Trung bình |
| 29 | **Triangular Arb** | Exploit chênh lệch giữa 3 cặp (BTC→ETH→USDT) | $5,000+ | 1–5% (high freq) | ⭐⭐⭐⭐⭐ | Thấp–Trung bình |
| 30 | **Spot-Futures Basis (Cash & Carry)** | Exploit chênh lệch giữa giá spot và futures | $5,000+ | 1–3% (rất ổn định) | ⭐⭐⭐ | Thấp |
| 31 | **Funding Rate Arbitrage** | Long spot + Short perp để ăn funding fee (delta-neutral) | $5,000+ | 1–4% (rất ổn định) | ⭐⭐⭐ | Thấp |
| 32 | **Volatility Trading** | Trade options/perp dựa trên implied vs realized vol | $5,000+ | 5–20% | ⭐⭐⭐⭐⭐ | Trung bình–Cao |
| 33 | **Launchpool Sniping** | Farm token mới trên Binance Launchpool, hedge bằng short | $5,000+ | 2–8% | ⭐⭐⭐ | Thấp–Trung bình |
| 34 | **Liquidation Arb** | Mua tài sản bị thanh lý trên lending protocol với giá chiết khấu | $10,000+ | 3–15% (event-based) | ⭐⭐⭐⭐ | Trung bình |

---

## Tier 4 — Vốn Cao ($30,000 – $100,000)

| # | Chiến thuật | Mô tả | Vốn tối thiểu | Lợi nhuận/tháng | Độ khó | Rủi ro |
|---|---|---|---|---|---|---|
| 35 | **Passive Market Making** | Đặt lệnh cả 2 bên bid/ask, ăn spread | $30,000+ | 2–8% | ⭐⭐⭐⭐⭐ | Trung bình |
| 36 | **Cross-Exchange Arb** | Mua sàn A, bán sàn B khi có chênh lệch giá | $30,000+ | 1–5% (ổn định) | ⭐⭐⭐⭐ | Thấp–Trung bình |
| 37 | **Statistical Arb** | Multi-asset mean reversion dựa trên mô hình thống kê | $50,000+ | 3–10% | ⭐⭐⭐⭐⭐ | Trung bình |
| 38 | **Flash Loan Attack** | Dùng flash loan để exploit chênh lệch hoặc bug protocol | $50,000+ (hoặc $0) | 🎰 Không cố định (0% hoặc 500%+) | ⭐⭐⭐⭐⭐ | **Rất cao** |

---

## Tier 5 — Vốn Rất Cao ($100,000+)

| # | Chiến thuật | Mô tả | Vốn tối thiểu | Lợi nhuận/tháng | Độ khó | Rủi ro |
|---|---|---|---|---|---|---|
| 39 | **Inventory-Adjusted MM** | Market making với điều chỉnh quote theo inventory | $100,000+ | 2–6% (ổn định) | ⭐⭐⭐⭐⭐ | Trung bình |
| 40 | **Avellaneda-Stoikov MM** | Model toán học tối ưu spread + vị thế theo vol + inventory | $100,000+ | 3–8% (ổn định) | ⭐⭐⭐⭐⭐ | Trung bình |
| 41 | **MEV (Maximal Extractable Value)** | Front-run, back-run, sandwich attack trên blockchain | $100,000+ (infra + capital) | 5–30% | ⭐⭐⭐⭐⭐ | Trung bình–Cao |
| 42 | **Sandwich Attack** | Front-run + back-run một swap lớn trên DEX | $100,000+ | 5–20% | ⭐⭐⭐⭐⭐ | Trung bình |
| 43 | **Mempool Sniping** | Monitor mempool để front-run transaction chưa confirm | $100,000+ | 5–25% | ⭐⭐⭐⭐⭐ | Cao |
| 44 | **Liquidation Hunting** | Đẩy giá về vùng cluster liquidation để trigger cascade | $200,000+ | 10–50% | ⭐⭐⭐⭐⭐ | **Rất cao** |
| 45 | **Spoofing / Layering** | Đặt wall giả để lừa trader khác *(bất hợp pháp)* | $500,000+ | 5–20% | ⭐⭐⭐⭐⭐ | **Rất cao + Pháp lý** |

---

## Bảng Xếp Hạng Theo Lợi Nhuận / Rủi Ro (Risk-Adjusted)

> Chiến thuật có **Sharpe ratio cao nhất** — lợi nhuận ổn định, drawdown thấp.

| Hạng | Chiến thuật | Lợi nhuận/tháng | Rủi ro | Vốn | Đánh giá |
|---|---|---|---|---|---|
| 🥇 | **Funding Rate Arbitrage** | 1–4% | Thấp | $5K+ | Delta-neutral, gần như risk-free |
| 🥈 | **Spot-Futures Basis** | 1–3% | Thấp | $5K+ | Cash-and-carry cổ điển |
| 🥉 | **Pairs Trading** | 3–10% | Trung bình | $2K+ | Market-neutral, ổn định |
| 4 | **Launchpool Sniping** | 2–8% | Thấp–TB | $5K+ | Hedged farming |
| 5 | **Cross-Exchange Arb** | 1–5% | Thấp–TB | $30K+ | Cần vốn lớn nhưng an toàn |
| 6 | **Funding Sniping** | 2–8% | Trung bình | $500+ | Tốt cho vốn nhỏ |
| 7 | **Funding Reversion** ⚡ | 10–40% | TB–Cao | $500+ | **Best bang for buck ở vốn nhỏ** |
| 8 | **Grid Trading** | 3–10% | Trung bình | $200+ | Chỉ hiệu quả sideways |
| 9 | **Avellaneda-Stoikov MM** | 3–8% | Trung bình | $100K+ | Cần quant background |
| 10 | **Statistical Arb** | 3–10% | Trung bình | $50K+ | Cần research nặng |

---

## 🎯 Bảng Lọc Đặc Biệt: Vốn Thấp & Cực Thấp ($0 – $5,000)
> Sắp xếp theo thứ tự ưu tiên: **Lợi nhuận cao ⬇️** → **Độ khó thấp ⬆️** → **Khả năng Auto cao 🟢** → **Rủi ro thấp ⬆️**
> **Mức độ Auto:** 🟢 Full-Auto (Bot tự động 100%) | 🟡 Semi-Auto (Bot báo signal + Người click) | 🔴 Manual (Làm thủ công)

### Nhóm 1: Lợi Nhuận Đột Biến / Rất Cao (>30%)
| Lợi nhuận | Chiến thuật | Độ khó | Auto | Rủi ro | Vốn | Ghi chú |
|---|---|---|---|---|---|---|
| **20–100%+** | **Influencer Front-Running** | ⭐⭐ | 🟡 Semi | Cao | $50+ | Theo dõi KOLs shill, rủi ro đu đỉnh |
| **20–50%** | **Listing/Delisting Sniping** ⚡ | ⭐⭐⭐⭐ | 🟢 Full | Cao | $500+ | Event-driven, cần infra tốc độ cao |
| **15–50%** | **Squeeze Trading** | ⭐⭐⭐⭐ | 🟢 Full | Cao | $1,000+ | Exploit funding rate & OI cực đoan |
| **10–40%** | **Funding Reversion** ⚡ | ⭐⭐⭐⭐ | 🟢 Full | TB–Cao | $500+ | **Đang build bot - Tối ưu nhất tier này** |
| **0–1000%+**| **Airdrop Farming** | ⭐⭐ | 🔴 Manual| Thấp | $0+ | Tốn thời gian cày cuốc, lợi nhuận hên xui |

### Nhóm 2: Lợi Nhuận Cao (10–30%)
| Lợi nhuận | Chiến thuật | Độ khó | Auto | Rủi ro | Vốn | Ghi chú |
|---|---|---|---|---|---|---|
| **10–30%** | **Breakout Trading** | ⭐⭐ | 🟢 Full | Trung bình | $100+ | Bot trade breakout kháng cự/hỗ trợ, dễ code |
| **10–30%** | **Social Sentiment** | ⭐⭐ | 🟡 Semi | Trung bình | $50+ | Phân tích sentiment NLP từ Twitter/Tele |
| **10–30%** | **Token Unlock / Vesting** ⚡| ⭐⭐⭐ | 🟡 Semi | TB–Cao | $1,000+| Mật phục lịch trả token để short |
| **10–30%** | **Momentum Ignition** | ⭐⭐⭐⭐ | 🟢 Full | Cao | $500+ | Phân tích volume spike đột biến |
| **10–25%** | **Stop Hunt / Stop Run** | ⭐⭐⭐ | 🟢 Full | Cao | $500+ | Đánh ngược vùng thanh lý của retail |
| **5–30%** | **CPI/FOMC News** | ⭐⭐⭐ | 🟡 Semi | Cao | $1,000+| Trade theo tin tức kinh tế vĩ mô |

### Nhóm 3: Lợi Nhuận Khá (5–20%)
| Lợi nhuận | Chiến thuật | Độ khó | Auto | Rủi ro | Vốn | Ghi chú |
|---|---|---|---|---|---|---|
| **5–20%** | **Trend Following** | ⭐⭐ | 🟢 Full | Trung bình | $100+ | Dùng bot MA/EMA chéo, dễ implement |
| **5–15%** | **Mean Reversion** | ⭐⭐ | 🟢 Full | Trung bình | $100+ | RSI, Bollinger Bands, bắt dao rơi ngắn hạn |
| **5–15%** | **Fear & Greed Index** | ⭐ | 🟡 Semi | Trung bình | $50+ | Đánh ngược tâm lý đám đông |
| **5–20%** | **Whale Wallet Tracking** | ⭐⭐⭐ | 🟢 Full | Trung bình | $1,000+| Bot copy trade theo ví cá mập on-chain |
| **5–15%** | **Front Running / Penny Jump**| ⭐⭐⭐ | 🟢 Full | Trung bình | $500+ | Đặt trước wall lệnh trên sổ lệnh |
| **5–20%** | **Absorption Detection** | ⭐⭐⭐⭐ | 🟢 Full | Trung bình | $500+ | Phân tích wall hấp thụ volume |
| **5–15%** | **Order Book Imbalance** | ⭐⭐⭐⭐ | 🟢 Full | Trung bình | $500+ | Cân bằng áp lực mua/bán từ Orderbook |
| **5–20%** | **Liquidation Heatmap** | ⭐⭐⭐⭐ | 🟡 Semi | Trung bình | $1,000+| Đánh theo map thanh lý của Coinglass |
| **5–15%** | **Iceberg Detection** | ⭐⭐⭐⭐⭐| 🟢 Full | Trung bình | $2,000+| Phát hiện lệnh ẩn (rất khó code) |
| **5–20%** | **Fork/Upgrade Trading** | ⭐⭐⭐ | 🔴 Manual| Trung bình | $500+ | Mua trước sự kiện nâng cấp mạng |

### Nhóm 4: Lợi Nhuận Ổn Định / Thấp (2–12%)
| Lợi nhuận | Chiến thuật | Độ khó | Auto | Rủi ro | Vốn | Ghi chú |
|---|---|---|---|---|---|---|
| **3–10%** | **DCA (Dollar Cost Average)**| ⭐ | 🟢 Full | Thấp | $100+ | Bot tự động mua theo định kỳ |
| **3–10%** | **Grid Trading** | ⭐⭐ | 🟢 Full | Trung bình | $200+ | Rải lưới lệnh ăn spread lúc sideways |
| **2–8%** | **Funding Sniping** ⚡ | ⭐⭐⭐ | 🟢 Full | Trung bình | $500+ | Ăn phí funding lúc snapshot, an toàn cao |
| **3–12%** | **VWAP Reversion** | ⭐⭐⭐ | 🟢 Full | Trung bình | $500+ | Mua bán quanh đường VWAP |
| **3–10%** | **Pairs Trading** | ⭐⭐⭐⭐ | 🟢 Full | Trung bình | $2,000+| Long coin mạnh, Short coin yếu |

### Nhóm 5: Cờ Bạc (Rủi ro hủy diệt)
| Lợi nhuận | Chiến thuật | Độ khó | Auto | Rủi ro | Vốn | Ghi chú |
|---|---|---|---|---|---|---|
| **10–50%** | **Martingale** | ⭐⭐ | 🟢 Full | **Rất cao** | $300+ | Gấp thếp vol, dễ cháy khét lẹt khi có trend dài |

---

## Ma Trận Tổng Quan

```
Vốn thấp                                              Vốn cao
$0 ──────────────────────────────────────────────────── $500K+
│                                                        │
│  Airdrop    Funding     Basis Arb    Market Making      │
│  Sentiment  Reversion   Tri-Arb      Cross-Ex Arb       │
│  DCA        Listing     DEX-CEX      Stat Arb           │
│  Grid       Squeeze     Vol Trading  MEV/Sandwich       │
│  Trend      Pairs       Launchpool   Liquidation Hunt   │
│                                                        │
└── Manual ─── Semi-Auto ─── Full Auto ─── HFT Infra ──┘
     Độ tự động hóa tăng dần →
```

```
Lợi nhuận thấp (ổn định)                    Lợi nhuận cao (biến động)
1%/mo ─────────────────────────────────────── 50%+/mo
│                                                │
│  Basis Arb     Grid       Funding Rev    Squeeze     │
│  Funding Arb   DCA        Listing        Liquidation │
│  Cross-Ex      Pairs      Momentum       Martingale  │
│  Tri-Arb       VWAP       News Trading   Flash Loan  │
│                                                │
└── Low Risk ──── Medium Risk ──── High Risk ────┘
```

---

## Chiến Thuật Bạn Đang Triển Khai

| Chiến thuật | Tier | Lợi nhuận/tháng | Trạng thái |
|---|---|---|---|
| **Funding Reversion** | Tier 2 ($500+) | 10–40% | ✅ Đang build bot |
| **Listing/Delisting Sniping** | Tier 2 ($500+) | 20–50%/event | 📋 Đã research |
| **Token Unlock / Vesting** | Tier 2 ($1,000+) | 10–30%/event | 📋 Đã research data source |

---

## Gợi Ý Mở Rộng Theo Lộ Trình

```
Phase 1 (hiện tại)     Phase 2                Phase 3
───────────────────    ──────────────────     ──────────────────
Funding Reversion  →   Listing Sniping    →   Funding Rate Arb
                       Token Unlock           Pairs Trading
                       Squeeze Trading        Spot-Futures Basis
                       Liquidation Heatmap    DEX-CEX Arb
```

> [!TIP]
> Với vốn $500–$5,000, **Tier 2** là vùng sweet spot — đủ edge mà không cần infra phức tạp hay vốn lớn. Funding Reversion là lựa chọn tối ưu nhất ở tier này vì edge đến từ **tốc độ + automation**, không phải vốn.

> [!WARNING]
> Các con số lợi nhuận là **ước tính lạc quan** khi chiến thuật được thực thi đúng. Thực tế phụ thuộc vào: điều kiện thị trường, execution quality, slippage, fee, và kỷ luật quản lý rủi ro. Nhiều trader retail không đạt được số liệu trên.

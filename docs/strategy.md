# Cẩm Nang Toàn Bộ Chiến Dịch Biến Động Có Thể Dự Đoán

> Tổng hợp **11 kịch bản thị trường** mà thời điểm biến động được biết trước hoặc có tín hiệu rõ ràng, có thể Long hoặc Short chủ động.

---

## Bảng Tổng Quan — Sắp Theo Tần Suất

| # | Chiến Dịch | Loại Coin | Hướng Đánh | Biết Giờ | Biết Hướng | Biên Lời | Tần Suất |
|:---:|---|---|:---:|:---:|:---:|:---:|:---:|
| 4 | Funding Rate Settle | Shitcoin FR cao | Ngược FR | ✅ | ✅ | 2-7% | ~90/tháng (mỗi 8h) |
| 5 | Token Unlock / Vesting | Mid-cap có lịch | SHORT | ✅ | ✅ | 5-15% | 5-10/tháng |
| 2 | Listing/Delisting Sàn Lớn | Coin có sẵn trên MEXC | LONG / SHORT | ⚠️ | ✅✅ | 20-50% | 3-8/tháng |
| 6 | Tin Vĩ Mô US | BTC/ETH + Altcoin | STRADDLE | ✅ | ❌ | 3-10% | 3-5/tháng |
| 7 | Options Expiry Gamma Release | BTC/ETH + Altcoin | Đọc Max Pain | ✅ | ⚠️ | 5-15% | 4/tháng |
| 10 | CME Gap Fill | BTC only | Ngược gap | ✅ | ✅ | 1-3% | 4/tháng |
| 8 | OI Squeeze Chốt Phiên | Shitcoin OI cao | STRADDLE | ⚠️ | ⚠️ | 5-20% | 2-5/tháng |
| 1 | Airdrop Claim Date | Shitcoin mới | SHORT | ✅ | ✅✅ | 10-50% | 2-4/tháng |
| 9 | Launchpool Kết Thúc | BNB + Token mới | SHORT | ✅ | ✅ | 3-8% | 2-3/tháng |
| 3 | Sàn Giảm Đòn Bẩy | Meme, Shitcoin | SHORT → LONG | ✅ | ✅✅ | 5-20% | 1-3/tháng |
| 11 | Token Burn Định Kỳ | BNB, BIT... | SHORT sau burn | ✅ | ⚠️ | 2-5% | 1/quý |

---

## P1. 🪂 Airdrop Claim Date — SHORT Ngày Mở Nhận Token

**Coin mục tiêu:** Token mới có airdrop lớn — ARB, JUP, STRK, ZK, W, EIGEN, BLAST, ZRO, LAYER, KAITO...
**Nguồn tín hiệu:** Blog dự án, Twitter, earndrop.io, airdrops.io

- **Hành Vi Giá:** 88% token dump trong 15 ngày đầu sau claim. Farmer nhận free → xả lập tức. FDV bị thổi phồng, thanh khoản mỏng → cú bán nhỏ cũng gây sóng lớn.
- **Chiến Thuật:**
  - **SHORT IOC** ngay khi giờ claim mở — thanh khoản mỏng nên IOC xuyên orderbook hiệu quả.
  - **Trap LONG** sâu -15% phòng nảy đàn hồi quá đà.
  - SL chặt 5% phòng 12% trường hợp token pump ngược (dự án quá mạnh).
- **Tại sao P1:** Hướng rõ nhất (88% SHORT win), biên lời lớn nhất (10-50%), biết trước 1-7 ngày, coin shitcoin = đúng phân khúc mục tiêu.

---

## P2. 📢 Listing/Delisting Trên Sàn Lớn (Binance)

**Coin mục tiêu:** Bất kỳ coin nào **đã có sẵn trên MEXC/Gate** mà Binance thông báo niêm yết hoặc huỷ niêm yết.
**Ví dụ gần đây:** ONDO, PIXEL, MANTA, JUP — pump 20-50% trên MEXC khi Binance announce listing.
**Nguồn tín hiệu:** Binance Announcement RSS, Telegram @binaborsa, CryptoPanic API

- **Hành Vi Giá:**
  - **Listing:** Pump 20-50% trong vài giây. Sàn Tier 2/3 (MEXC, Gate) phản ứng trễ → cửa sổ ăn sóng.
  - **Delisting:** Dump -30% đến -50% chớp nhoáng.
- **Chiến Thuật:**
  - **Listing → LONG IOC** trên MEXC ngay khi phát hiện tin. Tốc độ quyết định tất cả (< 30 giây).
  - **Delisting → SHORT IOC** bắn xuyên orderbook.
  - Cần Trigger Module quét Telegram/RSS real-time.
- **Rủi ro:** Trễ > 1 phút = ăn đỉnh/đáy. Cần hạ tầng low-latency.

---

## P3. ⚖️ Sàn Giảm Đòn Bẩy — Liquidation Domino

**Coin mục tiêu:** Meme coin, shitcoin bị sàn nhắm: DOGE, SHIB, PEPE, WIF, BONK, FLOKI, ORDI...
**Nguồn tín hiệu:** Binance/OKX Announcement — keyword "leverage adjustment", "risk control", "trading rule update"

- **Hành Vi Giá:** Sàn thông báo giảm max leverage (VD: 50x → 20x) → vị thế leverage cao bị ép margin → forced liquidation hàng loạt → Liquidation Cascade → DUMP. Sau cascade: V-shape recovery khi thanh khoản lấp đầy.
- **Chiến Thuật:**
  - **Phase 1 — SHORT:** Ngay khi tin ra (trước giờ hiệu lực 24-48h). Đám đông panic sell trước.
  - **Phase 2 — LONG:** Trap bắt V-shape recovery sau khi cascade kết thúc.
  - Straddle hoàn hảo: 2 pha ngược chiều nối tiếp.
- **Rủi ro:** Timing Phase 2 khó xác định — V-shape có thể delay 1-24h.

---

## P4. 💰 Funding Rate Settle — Đánh Ngược Dòng FR *(Đã triển khai)*

**Coin mục tiêu:** Shitcoin FR cực cao ≥ 0.3%: SUPER, MANTA, ONDO, PIXEL, ALT, MAVIA, MYRO...
**Nguồn tín hiệu:** MEXC Funding Rate API — settle mỗi 8h (00:00, 08:00, 16:00 UTC)

- **Hành Vi Giá:** FR > 0 → đám đông đang Short → đến giờ settle phải trả phí → mua lại để đóng → giá Pump. Ngược lại FR < 0.
- **Chiến Thuật:**
  - FR > 0 → **LONG IOC** tại T-100ms (cướp trước đám đông). **Trap SHORT** sâu +5% bắt nến quá đà.
  - FR < 0 → **SHORT IOC** + **Trap LONG** sâu -5%.
  - Cơ chế Straddle Trapping (Gọng Kìm 2 Luồng) trên Hedge Mode.
- **Trạng thái:** ✅ **ĐANG CHẠY PRODUCTION**

---

## P5. 🔓 Token Unlock / Vesting Schedule — SHORT Ngày Xả Hàng

**Coin mục tiêu:** Mid-cap có lịch vesting rõ: ARB, OP, APT, SUI, SEI, TIA, PYTH, JTO, DYDX, ID...
**Nguồn tín hiệu:** TokenUnlocks.app — lịch chính xác đến từng giờ, free API.

- **Hành Vi Giá:** Đám đông biết trước lịch unlock → panic short trước giờ. Sau unlock: VC/team xả token thật → dump tiếp. Hiệu ứng mạnh nhất khi unlock ≥ 2% circulating supply.
- **Chiến Thuật:**
  - **SHORT IOC** trước giờ unlock 1-4h (đi cùng crowd panic).
  - **Hoặc Trap LONG** sâu sau dump — bắt nảy đàn hồi khi selling exhaustion.
  - Chỉ đánh unlock lượng lớn (≥ 2% supply). Unlock nhỏ < 1% = noise, bỏ qua.
- **Rủi ro:** Một số dự án mạnh (OP, ARB) có thể absorb được selling pressure → giá không dump đáng kể.

---

## P6. 📰 Tin Tức Vĩ Mô US (NFP / CPI / FOMC)

**Coin mục tiêu:** BTC, ETH chịu ảnh hưởng trực tiếp. Shitcoin biên độ lớn hơn nhưng cần thanh khoản.
**Nguồn tín hiệu:** ForexFactory.com, Investing.com — lịch tháng.
**Khung giờ:** Thường 19h30 hoặc 01h00 đêm (giờ Việt Nam).

- **Hành Vi Giá:** Tin ra bùng nổ chính xác từng mili-giây. Giật giá 2 đầu cực mạnh (Kill both sides). Bóng nến rất dài.
- **Chiến Thuật:**
  - **Straddle — Bẫy Lưới Giãn:** Không đoán hướng, mở 2 Trap chờ quét sâu.
  - Trap Buy tại -10% (bắt đáy) + Trap Sell tại +10% (bắt đỉnh).
  - Bắt bóng nến dài, TP nhanh khi nến phục hồi về thân.
- **Chỉ số quan trọng nhất:** NFP (Non-Farm Payrolls), CPI (Lạm phát), FOMC (Lãi suất) — sắp theo mức tàn phá.

---

## P7. 🎯 Options Expiry — Gamma Release

**Coin mục tiêu:** BTC, ETH (trực tiếp). Altcoin tương quan cao chạy theo.
**Nguồn tín hiệu:** Deribit Expiry Calendar, Coinglass Max Pain Tracker.
**Lịch:** Weekly Thứ 6 23:00 VN. Monthly cuối tháng. Quarterly cuối quý (mạnh nhất).

- **Hành Vi Giá — 2 Pha:**
  - **Phase 1 "Nén" (T-24h → T=0):** Market Maker delta-hedge → giá bị "pin" vào Max Pain → volatility cực thấp. **KHÔNG ĐÁNH.**
  - **Phase 2 "Gamma Release" (T+0 → T+12h):** MM xả hết hedge → volatility nổ. Sóng 5-15% altcoin.
- **Chiến Thuật:**
  - Chỉ đánh **SAU expiry** (Phase 2).
  - Nếu giá nằm **dưới** Max Pain lúc expiry → **LONG** (giá bị nén xuống sẽ nảy lên).
  - Nếu giá nằm **trên** Max Pain → **SHORT**.
  - Straddle Trap rải ±5% quanh Max Pain để bắt cú giật đầu tiên.
- **Lưu ý:** Quarterly expiry hiệu ứng cực mạnh (OI hàng tỷ USD). Weekly thường yếu hơn.

---

## P8. 📊 OI Squeeze — Chốt Phiên Thanh Lý

**Coin mục tiêu:** Bất kỳ shitcoin nào có Open Interest / Market Cap ratio bất thường (> 30%).
**Nguồn tín hiệu:** Coinglass OI Tracker, HyblockCapital Liquidation Heatmap.
**Thời điểm:** Đóng nến ngày ~07:00 VN.

- **Hành Vi Giá:** OI rướn trần kỷ lục trong khi giá phẳng lỳ = "Lò xo nén". Một cú trigger nhỏ → Liquidation Cascade domino. Hướng đi có thể lên hoặc xuống, nhưng biên độ cực lớn.
- **Chiến Thuật:**
  - **Straddle:** Rải Trap Limit tại các mốc kháng cự/hỗ trợ quan trọng liền kề vùng thanh khoản yếu.
  - Dùng Liquidation Heatmap xác định vùng tập trung Stop Loss → đặt Trap ngay sau vùng đó.
  - Dấu hiệu nhận dạng: OI tăng > 20% trong 24h + Giá sideway < 2%.

---

## P9. 🚀 Binance Launchpool Kết Thúc

**Coin mục tiêu:** BNB (chính) + Token mới được farm.
**Nguồn tín hiệu:** Binance Launchpool page — thông báo trước 1-2 tuần.

- **Hành Vi Giá:**
  - **BNB:** Pump trước farming (demand stake) → **Dump** khi farming kết thúc (unstake hàng loạt).
  - **Token mới:** Pump ban đầu khi listing (hype) → **Dump** khi farmer bán token farm được.
- **Chiến Thuật:**
  - BNB: **SHORT IOC** ngay khi farming kết thúc.
  - Token mới: Chờ pump đầu 5-15 phút → **SHORT Trap** bắt sóng dump xuống.

---

## P10. 📊 CME Gap Fill — Sóng Thứ Hai Hàng Tuần

**Coin mục tiêu:** BTC only (CME chỉ có BTC/ETH Futures).
**Nguồn tín hiệu:** TradingView CME:BTC1! chart, CME website.

- **Hành Vi Giá:** CME đóng Thứ 6 22:00 UTC, mở Chủ Nhật 23:00 UTC. Crypto trade 24/7 nên tạo gap. Thống kê: ~70-80% gap được fill trong 1-3 ngày.
- **Chiến Thuật:**
  - Gap Up (giá hiện tại > giá đóng Thứ 6) → **SHORT** chờ fill về.
  - Gap Down → **LONG** chờ phục hồi.
  - Chỉ đánh gap ≥ 1%. Gap nhỏ = noise.
  - TP = giá đóng cửa CME Thứ 6.
- **Lưu ý:** Biên lời nhỏ (1-3%), nhưng tần suất đều và xác suất cao.

---

## P11. 🔥 Token Burn Định Kỳ — Sell the News

**Coin mục tiêu:** BNB (quarterly burn), BIT (BitDAO), LUNA classic...
**Nguồn tín hiệu:** Blog Binance, lịch burn công bố trước.

- **Hành Vi Giá:** Trước burn: pump kỳ vọng (giảm supply). Sau burn: "Sell the news" dump.
- **Chiến Thuật:**
  - **LONG** trước burn 3-7 ngày (ride the hype).
  - **SHORT** ngay sau khi burn hoàn tất (sell the news).
- **Lưu ý:** Hiệu ứng burn ngày càng yếu dần theo thời gian vì thị trường đã "price in".

# Funding Concerns

File này ghi các logic đang tồn tại nhưng chưa ổn, dễ gây hiểu nhầm, hoặc có thể vô ích nếu không được journal chứng minh. Concern không nhất thiết là bug, nhưng phải được nhìn thấy trước khi tăng size.

## High Severity

| Concern | Impact | Suggested response |
|---|---|---|
| Reversion và Trap có thể cộng exposure cùng cycle | Notional thực tế có thể cao hơn config người dùng nghĩ | Add `trapSizeRatio` và `maxCycleNotionalUSDT` |
| Missing/partial MFE-MAE làm tuning mù | TP/SL/trailing có thể bị chỉnh theo cảm giác | Implement Cycle Recorder P0 |
| Mixed percent units trong docs/config/journal | Có thể sai 100x ở TP/SL/depth | Percent-unit audit |
| TrackOrder/close failure có thể để unmanaged position | Rủi ro live trading nghiêm trọng | Journal critical event + fallback close + symbol disable |

## Medium Severity

| Concern | Impact | Suggested response |
|---|---|---|
| OB wall quanh settlement là unstable | TP/Trap dựa vào wall có thể vô ích hoặc hại | OB chỉ cap risk, journal `wall_distance_pct` và outcome |
| Trap chạy độc lập với Reversion outcome | Trap có thể thành trade riêng ngoài thesis ban đầu | Define risk rule: allow/skip when Reversion no-fill |
| Generic cycle topics dễ làm lẫn flow | Handler Trap/Reversion có thể coupling qua event chung | Split topic namespace by flow |
| Trap outcome bị che bởi cycle-level PnL | Reversion lời có thể giấu Trap lỗ | Journal leg-level outcome |

## Low Severity / Watchlist

| Concern | Impact | Suggested response |
|---|---|---|
| Imbalance Ratio dễ bị spoof | False confidence trên altcoin mỏng | Chỉ dùng filter phụ hoặc analysis feature |
| Trap depth cheat sheet là heuristic | Dễ overfit hoặc áp dụng sai symbol | Tune theo journal by symbol/FR bucket |
| Pre-Funding conflict với Reversion | Hai flow ngược chiều nhau | Force close before settle hoặc Hedge mode |
| Long docs dễ lẫn design với production | Người đọc tưởng chưa làm là đã làm | Mỗi file có status và link backlog/question |

## Current Rule

Không tăng size, không enable Pre-Funding, và không aggressive Trap sizing trước khi:

1. Cycle Recorder có MFE/MAE.
2. Journal report tách Reversion và Trap.
3. Percent-unit audit hoàn tất.
4. Có cycle-level exposure cap hoặc quyết định explicit chấp nhận risk.

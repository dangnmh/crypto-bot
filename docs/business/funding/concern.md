# Funding Concerns

File này ghi các logic đang tồn tại nhưng chưa ổn, dễ gây hiểu nhầm, hoặc có thể vô ích nếu không được journal chứng minh. Concern không nhất thiết là bug, nhưng phải được nhìn thấy trước khi tăng size.

## High Severity

| Concern | Impact | Suggested response |
|---|---|---|
| Reversion và Trap có thể cộng exposure cùng cycle | Notional thực tế có thể cao hơn config người dùng nghĩ | Add `trapSizeRatio` và `maxCycleNotionalUSDT` |
| Missing/partial MFE-MAE làm tuning mù | TP/SL/trailing có thể bị chỉnh theo cảm giác | Implement Cycle Recorder P0 |

## Medium Severity

| Concern | Impact | Suggested response |
|---|---|---|
| OB wall quanh settlement là unstable | TP/Trap dựa vào wall có thể vô ích hoặc hại | OB chỉ cap risk; Trap verifies fresh wall before placement and journals `wall_verified`, `wall_distance_pct`, outcome |
| Trap chạy độc lập với Reversion outcome | Trap có thể thành trade riêng ngoài thesis ban đầu | Define risk rule: allow/skip when Reversion no-fill |
| Generic cycle topics dễ làm lẫn flow | Handler Trap/Reversion có thể coupling qua event chung | Split topic namespace by flow |

## Low Severity / Watchlist

| Concern | Impact | Suggested response |
|---|---|---|
| Imbalance Ratio dễ bị spoof | False confidence trên altcoin mỏng | Chỉ dùng filter phụ hoặc analysis feature |
| Trap depth cheat sheet là heuristic | Dễ overfit hoặc áp dụng sai symbol | Tune theo journal by symbol/FR bucket |
| Pre-Funding conflict với Reversion | Hai flow ngược chiều nhau | Force close before settle hoặc Hedge mode |
| Long docs dễ lẫn design với production | Người đọc tưởng chưa làm là đã làm | Mỗi file có status và link backlog/question |

## Resolved / Guarded

| Concern | Status | Current behavior | Remaining refinement |
|---|---|---|---|
| TrackOrder/close failure có thể để unmanaged position | Guarded in code | Nếu TrackOrder placement fail, bot gọi fallback `CloseAllPositions(symbol)` bằng context mới không bị cancel. Nếu fallback close cũng fail, journal ghi `critical_close_failed`, publish flow error, và abort cycle thay vì publish `position_closed` giả. | Có thể thêm exact-leg close API để close đúng `close_side + volume`; giữ `CloseAllPositions` làm last-resort fallback. |

## Current Rule

Không tăng size, không enable Pre-Funding, và không aggressive Trap sizing trước khi:

1. Cycle Recorder có MFE/MAE.
2. Journal report tách Reversion và Trap.
3. Có cycle-level exposure cap hoặc quyết định explicit chấp nhận risk.

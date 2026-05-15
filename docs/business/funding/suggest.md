# Funding Suggestions

File này chứa suggestion cải thiện hệ thống và tài liệu. Suggestion chưa phải backlog bắt buộc. Khi chốt làm, chuyển sang [backlog.md](backlog.md).

## Documentation Standard

| Suggestion | Benefit |
|---|---|
| Mỗi flow doc chỉ chứa pipeline riêng của flow đó | Dễ biết logic nào thuộc flow nào |
| Mỗi file có `Status` rõ ràng: implemented/design/backlog | Tránh nhầm thiết kế với production behavior |
| Mọi percent field ghi ví dụ cả user-facing và internal unit | Giảm lỗi `3` vs `0.03` |
| Dùng cùng topic namespace trong docs và code | Journal/replay dễ map |
| Tách `question`, `suggest`, `concern`, `backlog` | Giảm tài liệu dạng “bãi đỗ ý tưởng” |

## Event & Domain Model

| Suggestion | Benefit |
|---|---|
| Tạo immutable `FundingCandidate` từ shared scan | Một input thống nhất cho ba flow |
| Thêm `FlowLegID` hoặc `flow` vào mọi event/journal record | Phân biệt Reversion/Trap/Pre-Funding trong cùng cycle |
| Dùng `symbol + settle_time + flow` làm logical key | Truy vết dễ hơn `req_id` đơn lẻ |
| Tách fill watcher theo flow leg | Tránh Trap fill bị lẫn IOC fill |
| Tách timeout theo flow | Trap wick window và Reversion no-fill window khác nhau |

## Risk & Safety

| Suggestion | Benefit |
|---|---|
| Thêm cycle-level notional cap | Ngăn nhiều flow cộng exposure ngoài ý muốn |
| Thêm `trapSizeRatio` mặc định thấp | Trap rủi ro hơn Reversion |
| Journal tất cả close/trailing failures | Dễ phát hiện unmanaged-position risk |
| Add stale-market-data guard cho OB/Ticker | Tránh đặt lệnh dựa trên snapshot cũ |

## Journal

| Suggestion | Benefit |
|---|---|
| Phase 1 dùng JSONL append-only | Ít dependency, inspect nhanh |
| Thêm MFE/MAE ngay từ bản đầu | Đây là dữ liệu tuning quan trọng nhất |
| Record dynamic và static baseline cùng lúc | Biết dynamic pricing có thật sự hơn static không |
| Report theo FR bucket và symbol | Tránh average toàn cục che mất behavior từng coin |

## Suggested Flow Doc Template

```text
# <Flow Name>

Status:

Purpose:

Input candidate:

Pipeline:

Phase contract:

Pricing/risk rules:

Journal fields:

Known concerns:
```

# Funding Questions

File này chứa câu hỏi mở cần trả lời trước khi đổi design hoặc implement. Khi câu hỏi biến thành việc cụ thể, move sang [backlog.md](backlog.md). Khi thành rủi ro của logic hiện tại, move sang [concern.md](concern.md).

## Architecture

| Question | Why it matters | Decision needed |
|---|---|---|
| Candidate shared scan nên là một component riêng hay nằm trong Sniper hiện tại? | Ảnh hưởng wiring, topic naming, test scope | Chọn owner module |
| Có cần một `CycleRiskController` riêng không? | Ba flow độc lập nhưng exposure chung | Chọn interface và lifecycle |
| Topic naming nên dùng `funding.<flow>.<event>` hay giữ `cycle.<event>`? | Ảnh hưởng migration và journal timeline | Chọn convention trước khi refactor |

## Reversion

| Question | Why it matters |
|---|---|
| Fire offset nên target T+0, T- nhỏ, hay T+ nhỏ? | Quá sớm có funding risk, quá muộn mất edge |
| TP safety cap từ OB nên disable khi OB stale bao lâu? | OB quanh settle dễ sai |
| TrackOrder fail thì fallback close ngay hay đặt TP/SL khác? | Ảnh hưởng unmanaged-position risk |

## Trap

| Question | Why it matters |
|---|---|
| Trap mặc định nên chạy độc lập dù Reversion no-fill không? | Có thể biến Trap thành trade riêng không có hedge context |
| OB-assisted Trap có thật sự tốt hơn static FR-depth không? | Cần journal so sánh `ob_monitor` vs `static_limit` |
| Trap timeout nên theo cycle timeout hay riêng theo wick window? | Wick bounce thường ngắn, timeout dài có thể giữ risk vô ích |

## Pre-Funding

| Question | Why it matters |
|---|---|
| Pre-Funding nên là bot/module riêng hay flow trong funding bot? | Clean lifecycle vs shared scanner tiện hơn |
| Entry cần price momentum + volume hay thêm OB shift? | Signal quá ít dễ false breakout, quá nhiều dễ miss trade |
| Có bao giờ nên hold qua settlement để nhận funding không? | Có thể conflict với Reversion và bị reversal |
| Position sizing khi Pre-Funding và Reversion cùng enabled là gì? | Tránh stacked exposure |

## Journal & Analysis

| Question | Why it matters |
|---|---|
| Phase 1 nên chỉ JSONL hay thêm SQLite ngay? | JSONL nhanh hơn, SQLite query tốt hơn |
| Percent fields trong existing journal cần migration không? | Mixed units làm report sai |
| Có cần record raw timeline trong mỗi cycle record hay chỉ pointer? | Trade-off giữa audit đầy đủ và file size |

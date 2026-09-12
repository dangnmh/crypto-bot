# Reversion Production Plan

Status: production readiness plan.

Mục tiêu của plan này là đưa Reversion lên production theo từng bước có kiểm soát, giảm blast radius, và đảm bảo mọi lỗi có side effect lên exchange đều có journal, alert, và manual action rõ ràng.

## Scope

| In scope | Out of scope |
|---|---|
| Reversion IOC quanh settlement | Tune strategy bằng cảm tính |
| Static TP/SL exit và timeout force-close | Thay đổi core signal hoặc pricing model |
| Key rotation, runtime safety, monitoring, notification | Bật full capital ngay từ đầu |
| Journal/reconciliation sau order side effect | Thêm flow mới |

## Production Principles

| Principle | Rule |
|---|---|
| Fail closed | Thiếu config, secret, metadata, hoặc exchange permission thì không chạy |
| Least privilege | API key production không có withdrawal permission |
| Bounded exposure | Mỗi rollout có symbol/account/notional/action limit |
| Observable terminal state | Mọi cycle phải kết thúc bằng closed, timeout, abort, hoặc error có journal |
| No silent side effect | Mọi order, cancel, close, fallback close đều có correlation id và audit trail |
| Manual recoverability | Critical alert phải nói rõ cần làm gì tiếp theo |

## Phase 0: Code And Config Freeze

Không bắt đầu rollout nếu các điều kiện này chưa đạt.

| Gate | Requirement | Evidence |
|---|---|---|
| Quality gates | `make lint`, `make test`, `make cover` pass | CI log hoặc local output |
| Config source | Prod config load từ expected path/project, không fallback dev | startup log + config snapshot redacted |
| Strategy defaults | `fundingReversion` đọc từ `configs/funding/reversion.jsonc` hoặc prod override tương đương | normalized config journal |
| Percent units | TP/SL/max price diff được normalize đúng convention | config validation/test |
| Time sync | Exchange clock drift và RTT dưới ngưỡng | sync metric/journal |
| Journal | JSONL schema v2 ghi đủ Reversion fields | sample dry-run/live paper cycle |
| Cleanup | Whole-cycle cleanup settle Trap branch còn mở trước journal | lifecycle test hoặc journal sample |

## Phase 1: Secret And Key Rotation

### Production Key Policy

| Item | Requirement |
|---|---|
| Key owner | Dùng service account/key riêng cho production bot |
| Permission | Chỉ futures/order permissions cần thiết; không withdrawal |
| Scope | Tách prod/staging/dev key; không reuse |
| IP allowlist | Bật nếu exchange hỗ trợ |
| Storage | Bitwarden project production, không `.env` plaintext |
| Logging | Không log key, secret, passphrase, signature, raw signed payload |

### Rotation Procedure

| Step | Action | Expected result |
|---|---|---|
| 1 | Tạo key mới với quyền tối thiểu | Key chưa được bot dùng |
| 2 | Lưu key mới vào Bitwarden prod project | Loader đọc được secret mới |
| 3 | Restart staging/prod dry-run bằng key mới | Auth, signing, metadata calls pass |
| 4 | Chạy một settlement cycle giới hạn hoặc paper/live-zero-size nếu hỗ trợ | Không có auth/signature error |
| 5 | Disable key cũ sau khi xác nhận key mới ổn | Không còn dependency key cũ |
| 6 | Ghi rotation record | Có timestamp, operator, key alias, validation evidence |

### Emergency Rotation

Kích hoạt khi có nghi ngờ leak, unexpected auth behavior, hoặc exchange báo key compromise.

1. Bật kill switch hoặc disable Reversion trong config.
2. Revoke key hiện tại trên exchange.
3. Tạo key mới với permission tối thiểu.
4. Update Bitwarden prod project.
5. Restart bot ở dry-run hoặc limited mode.
6. Chỉ enable live sau khi auth/signing và account state reconciliation pass.

## Phase 2: Runtime Safety Guards

| Guard | Initial production setting | Failure behavior |
|---|---|---|
| Kill switch | Reversion can be disabled without code deploy | Stop accepting new candidates |
| Symbol allowlist | 1-3 high-liquidity symbols only | Skip non-allowlisted candidates |
| Max active cycles | 1 Reversion cycle at a time for first rollout | Abort or skip additional candidates |
| Max notional | Small fixed notional per symbol/account | Reject candidate before order |
| Max latency | Respect `maxLatency` | Abort before IOC |
| Fire offset | Use configured `latencyRTT/2 + bufferTime` | Journal offset for tuning |
| Post-settle timeout | Keep bounded and explicit | Exact-leg close, then fallback close all |
| Retry budget | Bounded retry/backoff for close/cancel paths | Critical alert after retry exhaustion |
| Symbol disable | Disable symbol after critical close/cancel failure | Require manual review to re-enable |

## Phase 3: Error Handling Matrix

| Error class | Examples | Bot action | Alert |
|---|---|---|---|
| Config/secret missing | Bitwarden item missing, invalid JSONC | Fail startup | Critical |
| Permission/auth | invalid key, signature error, permission denied | Stop live trading, disable Reversion | Critical |
| Time sync | RTT/drift above threshold | Abort candidate before IOC | Warning if isolated, critical if repeated |
| Exchange validation | stop-limit price invalid, tick/scale mismatch | Abort cycle, record exchange error | Warning, critical if repeated |
| Rate limit | HTTP 429, exchange throttle | Retry bounded backoff; skip if unsafe | Warning |
| Network timeout before side effect | request did not reach exchange | Retry if idempotent/safe | Warning |
| Network timeout after possible side effect | order status unknown | Reconcile order/position before next action | Critical until resolved |
| IOC no fill | order fired but no fill | Publish timeout/no-fill path if no exposure | Info/Warning |
| TP/SL not closed by timeout | position still open | Exact-leg close, fallback close all | Warning |
| Exact close failed | exact-leg close rejected | Fallback close all | Critical if fallback required |
| Fallback close failed | exposure may remain live | Publish error, disable symbol, manual intervention | Critical |
| Journal write failed | terminal cannot be persisted | Stop new cycles; preserve local evidence | Critical |

## Phase 4: Monitoring

### Required Metrics

| Metric | Type | Purpose |
|---|---|---|
| `funding_reversion_candidates_total` | counter | Candidate volume |
| `funding_reversion_ioc_fired_total` | counter | Live order attempts |
| `funding_reversion_filled_total` | counter | Fill rate |
| `funding_reversion_closed_total` | counter | Normal terminal close |
| `funding_reversion_timeout_total` | counter | Timeout path frequency |
| `funding_reversion_abort_total` | counter | Abort path frequency |
| `funding_reversion_error_total` | counter | Critical error count |
| `funding_reversion_close_retry_total` | counter | Close retry pressure |
| `funding_reversion_fallback_close_total` | counter | Last-resort close usage |
| `exchange_auth_errors_total` | counter | Key/signature failures |
| `exchange_rate_limit_errors_total` | counter | Throttle pressure |
| `exchange_order_validation_errors_total` | counter | Tick/scale/TP/SL errors |
| `exchange_clock_drift_ms` | gauge | Time sync health |
| `funding_reversion_settle_offset_ms` | histogram | Timing quality |
| `funding_reversion_ioc_slippage_pct` | histogram | Execution quality |
| `funding_reversion_hold_duration_ms` | histogram | Exit behavior |
| `funding_reversion_last_success_timestamp` | gauge | Liveness |

### Alert Rules

| Severity | Condition | Action |
|---|---|---|
| Critical | Any auth/signature/permission error in prod | Disable Reversion, rotate key if needed |
| Critical | Fallback close failed | Manual close on exchange, disable symbol |
| Critical | Unknown order/position state after timeout | Reconcile exchange state before next cycle |
| Critical | Journal terminal write failed | Stop new cycles until persistence restored |
| Critical | No successful cycle or heartbeat for configured window during active schedule | Check process, exchange, config |
| Warning | RTT/drift exceeds threshold for one cycle | Skip candidate, monitor recurrence |
| Warning | Exchange validation error after confirmation | Inspect price/tick/TP/SL construction |
| Warning | Rate limit errors above threshold | Reduce symbol count or request rate |
| Warning | Fallback close used successfully | Review exact close failure and hedge-mode exposure |
| Info | Cycle closed normally | Record summary only |

## Phase 5: Notification Contract

Thông báo không được chứa secret hoặc raw signed payload.

| Field | Requirement |
|---|---|
| `severity` | info, warning, critical |
| `environment` | prod, staging, dev |
| `flow` | `reversion` |
| `run_id` / `req_id` | Correlate log, journal, alert |
| `symbol` | Affected symbol |
| `account_alias` | Alias only, no key |
| `topic` | Current event topic |
| `action` | fired_ioc, close_position, fallback_close, disabled_symbol |
| `result` | success, skipped, failed, unknown |
| `next_action` | retrying, stopped, manual_close_required, manual_review_required |

Critical notification examples:

| Scenario | Message intent |
|---|---|
| Auth failure | "Production Reversion auth failed; live trading disabled; rotate or verify key." |
| Fallback close failed | "Exposure may remain live; manually close symbol/account and keep symbol disabled." |
| Unknown state | "Order request outcome unknown; reconcile exchange orders/positions before re-enable." |
| Journal failure | "Terminal state may be missing from journal; stop new cycles and preserve logs." |

## Phase 6: Rollout Stages

| Stage | Mode | Limits | Exit criteria |
|---|---|---|---|
| 1 | Prod dry-run/config validation | No live order | Config, secret, time sync, metadata pass |
| 2 | Prod shadow observation | No live order; journal decisions only | At least 3 settlement windows without critical errors |
| 3 | Live canary | 1 account, 1 symbol, minimum notional, max 1 active cycle | 5-10 cycles, no critical close/fallback failure |
| 4 | Limited live | 2-3 symbols, conservative notional | Stable fill/close/journal metrics across 30 comparable cycles |
| 5 | Controlled scale | Increase notional/symbols gradually | Error rate and execution quality remain inside thresholds |

Không tăng stage nếu có critical alert chưa được đóng bằng root cause hoặc manual sign-off.

## Phase 7: Incident Runbook

### Exposure May Be Live

1. Disable Reversion or stop new candidates.
2. Check exchange open orders and positions for affected account/symbol.
3. Cancel stale TP/SL/order if needed.
4. Manually close position if bot fallback failed.
5. Record final exchange state, order ids, close price, and operator action.
6. Keep symbol disabled until root cause is fixed and tested.

### Unknown Order State

1. Do not retry blind.
2. Query order by client/order id if available.
3. Query open positions for affected symbol/account.
4. Reconstruct state from exchange first, then journal.
5. If position exists, close using manual or exact-leg bot path.
6. Only re-enable after journal and exchange state agree.

### Exchange Validation Error

1. Capture intended price, TP, SL, tick size, scale, side, position mode.
2. Compare against current exchange contract metadata.
3. Treat as order construction bug until proven otherwise.
4. Add regression test before changing strategy tuning.

## Phase 8: Go/No-Go Checklist

| Check | Go condition |
|---|---|
| Quality gates | `make lint`, `make test`, `make cover` pass |
| Key scope | Prod key has no withdrawal and no dev reuse |
| Rotation | Normal and emergency rotation path tested |
| Config | Prod config redacted snapshot reviewed |
| Time sync | RTT/drift within threshold |
| Dry-run | Prod dry-run passes startup and candidate decisions |
| Journal | Terminal events persist with required Reversion fields |
| Close safety | Exact close, fallback close, and failure path are tested or manually rehearsed |
| Alerts | Critical alerts reach the responsible operator |
| Notification | Message contains run id, symbol, action, result, next action |
| Rollback | Kill switch and symbol disable confirmed |
| Manual runbook | Operator can close exposure and reconcile state |

## Ownership

| Area | Owner role |
|---|---|
| Key rotation | Operator with exchange and Bitwarden access |
| Runtime config | Bot operator |
| Monitoring/alerts | Infrastructure owner |
| Strategy tuning | Strategy owner, using journal evidence only |
| Incident command | On-call operator for live trading window |

## Production Defaults For First Live Window

| Setting | Initial stance |
|---|---|
| Enabled symbols | 1 highly liquid symbol |
| Active cycles | 1 |
| Notional | Minimum practical live notional |
| Duration | One settlement window at a time |
| Alert channel | Critical channel watched live during rollout |
| Post-window review | Required before next live window |


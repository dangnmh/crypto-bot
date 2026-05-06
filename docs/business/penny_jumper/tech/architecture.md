# Technical Architecture: Penny Jump Bot

Tài liệu này mô tả kiến trúc kỹ thuật (Technical Architecture) cho hệ thống bot giao dịch theo chiến thuật Penny Jump (Front Running các Tường lệnh Altcoin/Shitcoin). Hệ thống là một **bot độc lập** (`cmd/penny_jumper/main.go`) với dedicated WebSocket connections và Local Store riêng biệt — **KHÔNG chia sẻ** runtime state với bất kỳ bot nào khác. Thiết kế dựa trên ngôn ngữ Golang với trọng tâm là **HFT cơ bản, độ trễ thấp (low-latency) và quản lý bộ nhớ nghiêm ngặt**.

---

## 1. Tổng Quan Kiến Trúc (High-Level Architecture)

Kiến trúc theo mô hình **Event-Driven Pipeline** — mỗi L2 update từ WebSocket tự động chảy qua 4 stage xử lý thay vì polling định kỳ:

1. **Background Data Jobs** — Thu thập và lọc dữ liệu thị trường qua REST API (định kỳ).
2. **Dynamic WS Layer** — Subscribe/Unsubscribe L2 OrderBook, build dữ liệu realtime.
3. **Event Pipeline** — `OB Builder → Wall Detector → Wall Scorer → Workflow Manager` — mỗi stage chỉ xử lý khi nhận event, emit event tiếp nếu có kết quả đáng chú ý.
4. **Per-Symbol Workflow (FSM)** — Nhận `WallChanged` / `WallDisappeared` events trực tiếp, phản ứng trong ~1ms.

![High-Level Architecture](./diagrams/high_level_architecture.mmd)

---

## 2. Các Module Cốt Lõi (Core Components)

### 2.1. Background Data Jobs (Thu thập dữ liệu định kỳ)

Hai background goroutine chạy định kỳ qua REST API, ghi kết quả vào **Local Store**:

| Job | REST Endpoint | Interval | Ghi vào Store | Mục đích |
|---|---|---|---|---|
| **Ticker24h Job** | `GET /api/v1/contract/ticker` | Mỗi `N` phút (VD: 15-30 min) | `TickerStore` (Vol24h, LastPrice) | Lọc coin có `Volume24h >= nM` |
| **Contract Job** | `GET /api/v1/contract/detail` | Mỗi `N` phút (VD: 60 min) | `ContractStore` (PriceUnit, VolUnit, ContractSize) | Lấy tick size, lot size cho tính toán giá |

**Flow khởi tạo (Init):**
1. Bot start → gọi REST lấy Ticker24h + Contracts → ghi vào Store.
2. `WaitReady()` — chờ cả 2 store có dữ liệu → tiếp tục.
3. Khởi động background jobs chạy định kỳ.

### 2.2. Pre-Filter & Dynamic Subscribe Manager

Mỗi khi **Ticker24h Job** hoàn thành một cycle, trigger bước lọc:

```
currentPairs = filter(TickerStore, vol24h >= config.minVolume)
removedPairs = previousPairs - currentPairs
newPairs     = currentPairs - previousPairs

for each pair in removedPairs:
    if WorkflowManager.HasActive(pair):
        → markPendingRemoval(pair)             // ⚠️ KHÔNG xóa khi FSM đang chạy
    else:
        → WS Multiplexer.Unsubscribe(pair)
        → DepthStore.Delete(pair)
        → WallHistoryStore.Delete(pair)

for each pair in newPairs:
    → WS Multiplexer.Subscribe(pair, "sub.depth.step0")
    → DepthStore.Init(pair)

// Khi FSM đạt DONE → callback: nếu pair có pendingRemoval → cleanup lúc này
previousPairs = currentPairs
```

> **⚠️ Safety Rule:** KHÔNG BAO GIỜ unsubscribe hoặc xóa DepthStore khi pair có workflow đang chạy. FSM cần dữ liệu wall để bailout an toàn.

### 2.3. WebSocket Layer (Realtime Data)

*   **WS Multiplexer:** Quản lý pool WS connections. Mỗi connection tối đa ~30 pairs (MEXC limit). Tự động mở connection mới khi cần.
*   **OrderBook Builder:** Nhận raw `depth.step0` events từ WS → parse → ghi `DepthStore` → **emit `DepthUpdated` event** vào pipeline.

### 2.4. Event Pipeline via In-Memory Pub/Sub Bus

Toàn bộ event pipeline sử dụng **in-memory Pub/Sub Bus** (`github.com/cskr/pubsub` — đã có sẵn trong project qua `engine.Bus`) thay vì direct Go channels. Mỗi stage publish/subscribe qua **topic names**, không cần reference trực tiếp lẫn nhau.

#### Topic Naming Convention

```
depth:{pair}                 // L2 OrderBook updated
wall:detected:{pair}         // Wall mới xuất hiện
wall:changed:{pair}          // Wall thay đổi volume
wall:disappeared:{pair}      // Wall biến mất
wall:qualified:{pair}        // Wall đạt Trust Score >= 65
order:filled:{pair}          // Lệnh khớp (từ personal WS)
```

#### Event Definitions

| Event | Topic Pattern | Publisher | Subscriber(s) |
|---|---|---|---|
| `DepthUpdated` | `depth:{pair}` | OB Builder | Wall Detector |
| `WallDetected` | `wall:detected:{pair}` | Wall Detector | Wall Scorer |
| `WallChanged` | `wall:changed:{pair}` | Wall Detector | Active FSM |
| `WallDisappeared` | `wall:disappeared:{pair}` | Wall Detector | Active FSM |
| `WallQualified` | `wall:qualified:{pair}` | Wall Scorer | Workflow Manager |
| `OrderFilled` | `order:filled:{pair}` | Order Watcher | Active FSM |

#### Lợi thế của Pub/Sub Bus so với Direct Channels

| Tiêu chí | Direct Channels | Pub/Sub Bus |
|---|---|---|
| **Coupling** | Stages cần reference trực tiếp lẫn nhau | Stages chỉ biết topic names — zero coupling |
| **Fan-out** | Phải tự build `map[pair]chan` + routing | Built-in — nhiều subscriber cùng topic |
| **Dynamic subscribe** | FSM cần nhận channel lúc spawn | FSM tự `bus.Sub("wall:changed:BTC")` lúc init |
| **Observability** | Khó log event flow | Thêm subscriber "logger" cho bất kỳ topic |
| **Cleanup** | Phải tự close channels | `bus.Unsub(ch)` khi FSM done |
| **Đã có trong project** | ❌ | ✅ `engine.Bus = pubsub.New(100)` |

#### Stage Flow qua Bus

![Pub/Sub Bus Flow](./diagrams/pubsub_bus_flow.mmd)

#### Stage 1: OB Builder

```go
func (b *OBBuilder) OnWSMessage(pair string, raw []byte) {
    ob := b.parse(raw)
    b.depthStore.Update(pair, ob)
    b.bus.Pub(DepthUpdated{Pair: pair, OB: ob}, "depth:"+pair)
}
```

#### Stage 2: Wall Detector

```go
func (d *WallDetector) Start(ctx context.Context, pairs []string) {
    // Subscribe tất cả active pairs
    for _, pair := range pairs {
        ch := d.bus.Sub("depth:" + pair)
        go d.processDepth(ctx, pair, ch)
    }
}

func (d *WallDetector) processDepth(ctx context.Context, pair string, ch chan interface{}) {
    defer d.bus.Unsub(ch)
    for msg := range ch {
        event := msg.(DepthUpdated)
        wall := d.scan(event.OB)
        prev := d.activeWalls[pair]

        if prev == nil && wall != nil {
            d.bus.Pub(WallDetected{...}, "wall:detected:"+pair)
        } else if prev != nil && wall != nil {
            d.bus.Pub(WallChanged{...}, "wall:changed:"+pair)
        } else if prev != nil && wall == nil {
            d.bus.Pub(WallDisappeared{...}, "wall:disappeared:"+pair)
        }
        d.activeWalls[pair] = wall
    }
}
```

#### Stage 3: Wall Scorer

```go
func (s *WallScorer) Start(ctx context.Context) {
    // Subscribe tất cả "wall:detected:*" — cskr/pubsub hỗ trợ multi-topic
    ch := s.bus.Sub("wall:detected:*")   // hoặc subscribe từng pair
    defer s.bus.Unsub(ch)

    for msg := range ch {
        wall := msg.(WallDetected)
        score := s.calculate(wall)
        if score >= s.threshold {
            s.bus.Pub(WallQualified{Wall: wall, Score: score}, "wall:qualified:"+wall.Pair)
        }
    }
}
```

#### Stage 4: Workflow Manager

```go
func (m *WorkflowManager) Start(ctx context.Context) {
    ch := m.bus.Sub("wall:qualified:*")
    defer m.bus.Unsub(ch)

    for msg := range ch {
        q := msg.(WallQualified)
        if m.HasActive(q.Pair) || m.ActiveCount() >= m.maxConcurrent {
            continue
        }
        m.Spawn(ctx, q)
    }
}
```

### 2.5. Per-Symbol Workflow (FSM)

Khi Workflow Manager spawn FSM, FSM tự **subscribe các topics** cần thiết cho pair của nó:

![FSM State Diagram](./diagrams/fsm_states.mmd)

**FSM Event Loop via Bus:**
```go
func (f *FSMWorkflow) Run(ctx context.Context, pair string) {
    // Subscribe đúng topics cho pair này
    wallCh := f.bus.Sub("wall:changed:"+pair, "wall:disappeared:"+pair)
    fillCh := f.bus.Sub("order:filled:"+pair)
    defer f.bus.Unsub(wallCh, fillCh)   // cleanup khi workflow kết thúc

    for {
        select {
        case msg := <-wallCh:
            switch e := msg.(type) {
            case WallDisappeared:
                f.fsm.Event("wall_gone")     // → BAILOUT
            case WallChanged:
                if e.NewVol < f.initialWallVol*0.5 {
                    f.fsm.Event("wall_weak") // → CANCEL
                }
            }
        case msg := <-fillCh:
            fill := msg.(OrderFill)
            f.fsm.Event("filled", fill)      // → EXIT_STRATEGY
        case <-f.timeoutTimer.C:
            f.fsm.Event("timeout")           // → CANCEL + DONE
        case <-ctx.Done():
            f.cleanup(); return
        }
    }
}
```

> **Lifecycle:** FSM tự `bus.Sub()` khi spawn → tự `bus.Unsub()` khi DONE. Không cần Workflow Manager quản lý channel map.

### 2.6. Execution Layer (Thực thi Lệnh)
*   **Order Manager:** Priority Queue — `CANCEL` (Bailout) luôn ưu tiên cao nhất. Token Bucket rate limiter.
*   **Order Watcher:** Subscribe `push.personal.order` WS → publish `order:filled:{pair}` vào Bus.
*   **Risk Manager:** Max Position Size, Daily Drawdown, Concurrent Positions.

### 2.7. Backpressure & Safety Rules

**Bus buffer:** `pubsub.New(100)` — mỗi subscriber channel có buffer 100 events. Nếu subscriber chậm:
- `cskr/pubsub` sẽ **block publisher** khi buffer đầy.
- Đây là acceptable cho pipeline này vì OB Builder emit ~3000 events/sec, mỗi event xử lý <1ms → subscriber không bao giờ đầy.

**Safety Rules:**
1. `WallDisappeared` event **KHÔNG BAO GIỜ được bỏ qua** — đây là bailout signal.
2. FSM **PHẢI `bus.Unsub()` khi kết thúc** — tránh memory leak (orphan subscribers).
3. Subscribe Manager **KHÔNG unsubscribe pair khi FSM đang chạy** — dùng `pendingRemoval` flag.

---

## 3. Tối Ưu Hiệu Năng (Performance Optimization)

Do đặc thù của Front Running là chạy đua với bot của Retail và MM khác, ứng dụng cần các chiến thuật tối ưu sau trong Golang:

1.  **Zero-Allocation & Sync.Pool:** 
    *   Tránh tạo mới object (struct) rác liên tục trong mỗi tick của WebSocket. 
    *   Sử dụng `sync.Pool` để tái sử dụng các object event JSON parser và mảng OrderBook, giúp giảm tải Garbage Collection (GC Pause) - yếu tố chí mạng gây giật lag (latency spike).
2.  **Lock-Free / Granular Locking:**
    *   Không dùng một biến `sync.RWMutex` khổng lồ cho toàn bộ các cặp giao dịch.
    *   Mỗi Symbol duy trì một Lock riêng, hoặc dùng kiến trúc **Actor Model / Channels** (1 goroutine phụ trách 1 symbol's state) để loại bỏ hoàn toàn việc tranh chấp Lock.
3.  **Goroutine Worker Pool:**
    *   Phân tán tải của 800+ symbols vào một nhóm Worker cố định (ví dụ 50 workers) thay vì spawn 1 goroutine cho mỗi sự kiện tick lẻ tẻ.

---

## 4. Xử Lý Rủi Ro Hệ Thống (Fault Tolerance)

*   **Network Disconnection:** Nếu mất kết nối WebSocket L2, toàn bộ FSM đang ở trạng thái `MONITORING` hoặc `EXIT_STRATEGY` (đang có lệnh trên sàn) phải kích hoạt tiến trình **Bailout / Cancel All**. Không có data mù = Không rủi ro.
*   **API Timeout / Error 50x:** Cơ chế Retry với Exponential Backoff (nhưng có giới hạn cứng). Nếu quá 3 lần gửi lệnh Bailout Market không thành công do API sàn lỗi, bắn alert khẩn cấp qua Telegram.
*   **Bóng ma sổ lệnh (Ghost Orders):** Đôi khi sàn delay update L2. Luôn đối chiếu Best Bid/Ask từ L2 stream với trạng thái Order nội tại để tránh tính toán sai lệch giá.

---

## 5. Tổ Chức Thư Mục (Directory Structure)

Cấu trúc thư mục theo DDD Clean Architecture hiện tại của project:

```text
cmd/
└── penny_jumper/
    └── main.go                          # Entrypoint — khởi tạo bot độc lập

configs/
└── penny_jumper/
    ├── system.jsonc                     # Engine config (WS, API, limits)
    └── penny_jumper.jsonc               # Strategy params (thresholds, sizing)

internal/
├── bots/
│   └── penny_jumper/
│       ├── application/
│       │   ├── fsm.go                   # State Machine (IDLE → FILLED → EXIT)
│       │   ├── detector.go              # Wall Detection logic
│       │   ├── scorer.go                # Wall Trust Score algorithm
│       │   ├── risk_manager.go          # Position sizing, daily loss limit
│       │   ├── order_queue.go           # Priority queue (CANCEL > PLACE)
│       │   └── wall_history.go          # Ring buffer + rolling WallEvent map
│       ├── config/
│       │   └── config.go                # Parse penny_jumper.jsonc
│       └── domain/
│           ├── state.go                 # FSM state definitions
│           └── wall.go                  # Wall entity + trust score attributes
├── infrastructure/                      # Reusable packages (mỗi bot tạo instance riêng)
│   ├── store/                           # Price, Ticker, Contract, Depth stores
│   ├── exchange/                        # REST/WS adapters (MEXC, Binance)
│   └── watcher/                         # Order fill watcher via personal WS
└── domain/
    └── types.go                         # Shared domain types
```

> **Lưu ý:** Các package trong `internal/infrastructure/` là **reusable modules**. Mỗi bot tự tạo instance riêng tại runtime — không chia sẻ connection hay state giữa các bot.

---

## 6. MEXC WebSocket API Reference

Thông tin kỹ thuật về các channel WebSocket mà bot sử dụng:

| Channel | Mô tả | Tần suất Push | Max Levels | Ghi chú |
|---|---|---|---|---|
| `sub.depth.step0` | L2 OrderBook raw (không aggregate) | ~100ms | 20 levels | Dùng cho Wall Detection — cần raw data |
| `sub.depth.step1-5` | L2 OrderBook aggregated | ~100ms | 20 levels | KHÔNG dùng — mất chi tiết tick-level |
| `sub.ticker` | Ticker (last price, 24h vol) | ~1s | N/A | Dùng cho Pre-filter volume check |
| `push.personal.order` | User order fills/cancels | Realtime | N/A | Dùng cho Order Watcher (fill confirmation) |

**Giới hạn quan trọng:**
- **Max pairs per WS connection:** ~30 pairs (MEXC limit). Bot dùng Multiplexer tự mở thêm connection khi cần.
- **Max WS connections per IP:** ~10 (cần verify). Nếu subscribe 300 pairs → cần ~10 connections.
- **Rate limit REST API:** ~100 requests/10 giây. Lệnh CANCEL (bailout) được ưu tiên trong Priority Queue.

---

## 7. Worker Pool Sizing Guidance

Hướng dẫn tính toán số lượng worker goroutine tối ưu:

```
Workers = ceil(ActivePairs × EventsPerSecPerPair × ProcessingTimePerEvent / 1000)

Ví dụ thực tế:
  ActivePairs      = 300 (sau pre-filter)
  EventsPerSec     = 10 L2 updates/sec/pair
  ProcessingTime   = ~1ms (Wall detect + Score calc)
  
  Total events/sec = 300 × 10 = 3,000
  CPU time/sec     = 3,000 × 1ms = 3,000ms = 3 cores
  
  Minimum workers  = 3-4 goroutines
  Recommended      = 8-16 goroutines (headroom cho GC pause + burst)
```

> **Lưu ý:** Con số "50 workers" là upper bound cho trường hợp subscribe 800+ pairs. Với 200-300 pairs (sau pre-filter), 8-16 workers là đủ. Tránh over-provision vì mỗi goroutine tiêu tốn ~8KB stack.

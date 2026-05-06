# Funding Reversion Bot — Flow

**Chiến lược: Funding Reversion** — Vào lệnh ngược chiều Sniper trước giờ Funding, đợi hiệu ứng xả hàng của đám đông săn phí để ăn chênh lệch giá.

---

## Chiến lược Reversion — Side Logic

| Funding Rate       | Sniper (cũ)      | Reversion (hiện tại) | Lý do                                                 |
| ------------------ | ---------------- | -------------------- | ----------------------------------------------------- |
| **FR > 0** (dương) | SHORT (nhận phí) | **LONG**             | Sniper SHORT → xả (buy) → giá pump → ta LONG ăn pump  |
| **FR < 0** (âm)    | LONG (nhận phí)  | **SHORT**            | Sniper LONG → xả (sell) → giá dump → ta SHORT ăn dump |

---

## Tổng quan kiến trúc

```mermaid
flowchart TD
    A["🚀 Bot Start"] --> B["📖 Load Config"]
    B --> C["🌐 Init Global Services<br/>(TimeSync + GlobalStore + WS)"]
    C --> D["🔄 Start Background Syncs"]
    D --> SPAWN

    subgraph SPAWN["🧵 Spawn Per-Symbol Workers"]
        W1["go worker(BTC_USDT)"]
        W2["go worker(ETH_USDT)"]
        W3["go worker(SOL_USDT)"]
        WN["go worker(...)"]
    end

    SPAWN --> WAIT["⏳ Wait for shutdown signal"]

    style SPAWN fill:#0f3460,stroke:#e94560,color:#fff
```

**Nguyên tắc:** Mỗi symbol chạy trên **1 goroutine riêng**, hoàn toàn độc lập qua cơ chế State Machine (FSM). Không liên quan gì đến symbol khác.

---

## Per-Symbol Worker Flow & State Machine

Mỗi Worker vận hành như một cỗ máy trạng thái (FSM) với các chuyển đổi (Transitions) rõ ràng:

```mermaid
stateDiagram-v2
    [*] --> IDLE: Khởi tạo Worker

    IDLE --> SCAN: Tìm thấy Settle Time
    SCAN --> ABORT: FR quá thấp / Lỗi dữ liệu

    SCAN --> ARM: FR >= Threshold
    ARM --> ABORT: Tính IOC / Safety FAIL
    ARM --> WAIT: Safety PASS

    WAIT --> RECHECK: Ngủ đến T-2s

    RECHECK --> FIRE_IOC: FR giữ nguyên chiều
    RECHECK --> ABORT: FR đổi dấu / Quá thấp

    FIRE_IOC --> FIRE_TRAP: Bật Hedge Trap (Limit)
    FIRE_IOC --> HOLD: Tắt Hedge Trap

    FIRE_TRAP --> HOLD: Quăng lệnh Trap xong

    HOLD --> PLACE_TRAILING: Khớp Lệnh (Fill Entry) Thành Công
    HOLD --> ABORT: Hết Timeout không khớp lệnh

    PLACE_TRAILING --> DONE: Đặt TrackOrder Xong

    ABORT --> IDLE: Về ngủ chờ chu kỳ sau
    DONE --> IDLE: Hoàn tất chu kỳ, về ngủ
```

---

## Chi tiết các Phase (States)

### 1. State: `scan` (T - 5 phút)
- Đọc Funding Rate (FR) từ GlobalStore.
- Nếu `|FR| < minFundingRate` → Chuyển sang `abort`.
- Build ứng viên với **reversion side** (FR+ → LONG, FR- → SHORT).

### 2. State: `arm` (T - vài phút)
- Subscribe WS Ticker để lấy giá Real-time.
- Tiến hành tính toán **Entry Price (IOC)** và **Volume** (Xem chi tiết tại [price_flow.md](price_flow.md)).
- Chạy Safety Check: Đánh giá Position Size so với Volume 24h (Max Impact Ratio) và Minimum Volume. Nếu Pass → `wait`.

### 3. State: `wait`
- Gọi `ts.Until()` để sleep chính xác đến `T - 2s` (Server-synced). Không làm gì cả để tiết kiệm tài nguyên.

### 4. State: `recheck` (T - 2 giây)
- Xác nhận lại lần cuối FR chưa đổi dấu và vẫn `>= minFR`.
- Chống rủi ro "bị lừa" phút chót.

### 5. State: `fire_ioc` (T - 200ms)
- Bắt đầu Snapshot lại giá và chờ đúng điểm nổ (Fire Offset).
- Quăng lệnh **Entry IOC (Market)** lên sàn.
- Ghi nhận `OrderID` chờ khớp.

### 6. State: `fire_trap` (T + 10ms) (Tuỳ chọn Hedge Trap)
- Nếu bật tính năng `enableHedgeTrap`, đợi Settle Time đi qua 10ms.
- Quăng lưới lệnh **Limit Order** sâu bên dưới (hoặc trên đỉnh) để bắt râu nến dội lại.

### 7. State: `hold` (Chờ khớp lệnh Entry)
1. **Wait for Deal:** Dùng channel WebSocket `dealChan` chờ sàn báo về lệnh `Entry IOC` (hoặc `Trap`) đã khớp được bao nhiêu volume (`dealVol`).
2. **Timeout Check:** Có timeout bảo vệ (ví dụ 3 giây). Nếu hết giờ không có tín hiệu khớp, quăng lệnh `CloseAll` và chuyển sang `abort`.
3. Nếu `dealVol` > 0, lập tức chuyển sang `place_trailing`.

### 8. State: `place_trailing` (Kích hoạt Trailing Stop)
Đây là state đã được nâng cấp thay vì gồng lệnh thủ công như trước:
- Ngay khi có thông tin `dealVol` > 0 từ state `hold`.
- Bot gọi API `POST /api/v1/private/trackorder/place`.
- Bắn cấu hình Trailing Stop (`backType=1`, `backValue=callbackPct`, `vol=dealVol`, ngược `side` Entry) cho sàn MEXC.
- Sàn sẽ **tự động dò đỉnh/đáy** và đóng lệnh khi giá rút râu đúng tỷ lệ `callbackPct`.
- Bot không cần chờ xem lệnh đóng ở đâu. Chuyển sang `done`.

### 9. State: `done` / `abort`
- Dọn dẹp: Hủy đăng ký (Unsubscribe) WebSocket Ticker.
- Ghi Log tổng kết tiến trình.
- Đưa goroutine về trạng thái `idle` và tính thời gian Settle kế tiếp (Thường là 8 tiếng sau).

---



## Sơ Đồ Cấu Trúc Trailing (Native MEXC)

```mermaid
flowchart LR
    A["Bot: Khớp Entry IOC"] --> B["Bot: Gọi API TrackOrder"]
    B --> C((MEXC Server))
    
    C -->|Theo dõi| D["Giá leo dần lên đỉnh"]
    D -->|Cập nhật| E["Peak Price mới"]
    E -->|Giá rút râu| F["MEXC Tự Kích Hoạt Market Close"]
    
    style C fill:#0f3460,stroke:#e94560,color:#fff
    style F fill:#005c2a,stroke:#e94560,color:#fff
```

> 💡 **Ưu điểm của mô hình này:** 
> Việc đẩy Trailing Order (`trackorder`) xuống trực tiếp Engine của MEXC giúp triệt tiêu hoàn toàn **độ trễ mạng (Network Latency)**. Râu nến rút nhanh cỡ nào thì Engine sàn tự cắt lệnh ngay tức thì, đồng thời chống được rủi ro Bot bị crash/đứt cáp giữa chừng lúc đang gồng.

---

## Error Handling

| Trường hợp lỗi | Hành động (FSM Action) |
| --- | --- |
| Settle Passed / No Settle | Sleep 1 phút, thử lại |
| FR không đủ điều kiện | `abort` → Bỏ qua chu kỳ này |
| Tính toán IOC thất bại | `abort` → Hủy chu kỳ |
| Lệnh IOC không khớp (No Fill) | `abort` → Dọn dẹp rác (CancelAll) |
| Đặt TrackOrder (Trailing) lỗi | Gọi API `close_all` khẩn cấp để chốt lệnh tĩnh |
| Bot tắt ngang sau khi đặt TrackOrder | An toàn. Sàn vẫn chốt lời theo TrackOrder |

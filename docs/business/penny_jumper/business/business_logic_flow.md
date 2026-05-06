# Flow Chart: Business Logic Hệ Thống Front Running (Penny Jump)

Dưới đây là sơ đồ luồng hoạt động (Flowchart) mô tả chi tiết toàn bộ Business Logic của chiến thuật Penny Jump áp dụng cho Altcoin/Shitcoin, từ bước lọc coin ban đầu cho đến khi chốt lời hoặc cắt lỗ khẩn cấp.

```mermaid
graph TD
    %% Define Styles
    classDef filter fill:#f9f2f4,stroke:#d9534f,stroke-width:2px;
    classDef process fill:#e1f5fe,stroke:#0288d1,stroke-width:2px;
    classDef decision fill:#fff3e0,stroke:#f57c00,stroke-width:2px;
    classDef action fill:#e8f5e9,stroke:#388e3c,stroke-width:2px;
    classDef danger fill:#ffebee,stroke:#d32f2f,stroke-width:2px;
    classDef finish fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px;

    %% Pre-filter & Subscribe
    subgraph Phase1 ["1. Pre-Filter & Subscribe"]
        A["Ticker24h Job\n(Every N min)"] --> B{"Volume 24h > nM?"}
        B -->|Yes| C["Subscribe WebSocket\nsub.depth.step0"]
        B -->|No| D["Ignore Pair"]
    end
    B:::decision
    C:::action
    D:::filter

    %% Event Pipeline (Push)
    subgraph Phase2 ["2. Event Pipeline (Push)"]
        C --> E["WS Push: depth.step0\n(10 updates/sec)"]
        E --> F{"Detector: Wall >= 20x\n& Dist <= 1% ?"}
        F -->|No| E
        F -->|Yes| G["Scorer: Calc Trust Score"]
        G --> H{"Score >= 65?"}
        H -->|No| E
        H -->|Yes| I["Trigger: WallQualified Event"]
    end
    F:::decision
    G:::process
    H:::decision
    I:::action

    %% Execution (Penny Jump)
    subgraph Phase3 ["3. Workflow Manager: Execution"]
        I --> J["Check Active Workflows\n& Max Positions"]
        J --> K["Spawn FSM & Place Maker\nat Wall Price ± 1 tick"]
        K --> L["FSM: MONITORING State"]
    end
    J:::process
    K:::action
    L:::process

    %% Monitoring (Event-driven)
    subgraph Phase4 ["4. Live Monitoring (Event-driven)"]
        L --> M{"Listen to Pub/Sub Bus"}
        M -->|"Event: wall:disappeared"| N["Cancel Order Immediately"]
        M -->|"Event: wall:changed (< 50%)"| N
        M -->|"Price moves away > 0.5%"| N
        M -->|"Timeout 60s"| N
        M -->|"Event: order:filled"| O["State: FILLED"]
    end
    M:::decision
    N:::danger
    O:::action

    %% Exit Strategy
    subgraph Phase5 ["5. Exit Strategy"]
        O --> P["Place Maker Take Profit +0.5% ~ +1.5%"]
        P --> Q["State: Monitoring Position"]
        Q --> R{"Exit Triggers"}
        
        R -->|"Original Wall Disappears!"| S["Market Exit - Bailout"]
        R -->|"Profit > 0.3%"| T["Activate Trailing Stop"]
        R -->|"Timeout 120s without TP hit"| U["Market Exit - Time Stop"]
        R -->|"TP Order Filled"| V["Position Closed - Profit Secured"]
        
        T --> W["Exit via Trailing Stop"]
    end
    P:::action
    Q:::process
    R:::decision
    S:::danger
    T:::process
    U:::danger
    V:::finish
    W:::finish
```

### Giải thích các Phase:

1. **Pre-Filter & Scanning:** Bộ lọc ban đầu để loại bỏ các coin rác không có giao dịch (dựa vào volume 24h), giúp giảm tải hệ thống không cần lắng nghe toàn bộ sàn.
2. **Wall Detection & Validation:** Khi bắt được dữ liệu sổ lệnh (Order Book), bot quét để tìm "Bức Tường" (Wall) đủ lớn, và phải nằm gần mức giá hiện tại. Tiếp đó chạy qua thuật toán `Wall Trust Score` để đánh giá xem tường này là thật hay ảo (Spoofing).
3. **Execution:** Đặt lệnh Penny Jump ngay trước Tường (cách 1 bước giá). Khối lượng lệnh phụ thuộc vào điểm uy tín của Tường (Trust Score).
4. **Live Monitoring:** Trong thời gian lệnh chờ khớp, bot giám sát gắt gao tình trạng của Tường lệnh. Nếu có dấu hiệu tường bị rút hoặc giá chạy xa thì ngay lập tức hủy lệnh phòng thân.
5. **Exit Strategy:** Khi lệnh đã khớp, bot đặt mục tiêu Take Profit và tiếp tục... giám sát Tường gốc. Nếu Tường gốc bỗng nhiên "bốc hơi" (tức là ta mất bệ đỡ), bot sẽ lập tức bán Market (Bailout) để thoát thân dù đang lời hay lỗ. Nếu giá chạy tốt thì kích hoạt Trailing Stop.

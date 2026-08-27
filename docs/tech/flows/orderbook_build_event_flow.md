# OrderBook Building & Real-Time Synchronization Flow

This document details the architecture, state machine, event flows, and packet loss recovery algorithms of the **Multi-Exchange Local OrderBook Synchronizer** (`internal/infrastructure/store/orderbook`).

---

## 1. Synchronizer State Machine & Lifecycle

Each active trading symbol maintains an isolated `symbolTracker` state machine:

```mermaid
stateDiagram-v2
    [*] --> SyncStateUninitialized: Symbol Discovered
    
    SyncStateUninitialized --> SyncStateSyncing: WS Depth Subscribed & First WS Event Arrives
    
    state SyncStateSyncing {
        [*] --> QueueBuffer: Incoming WS messages appended to buffer
        QueueBuffer --> FetchSnapshot: Call REST Depth Snapshot API
        FetchSnapshot --> BridgeCommits: Check Gap between Snapshot & Earliest Buffer Event
        BridgeCommits --> DrainSortedBuffer: Apply Commits + Drain Buffered Updates (ASC by Version)
    }
    
    SyncStateSyncing --> SyncStateReady: Snapshot & Buffered Deltas Applied
    SyncStateSyncing --> SyncStateUninitialized: REST Snapshot Failed (Retry on next tick)
    
    state SyncStateReady {
        [*] --> IngestUpdate
        IngestUpdate --> ApplyDirect: Version == LastVersion + 1 (or Snapshot Mode)
        IngestUpdate --> DetectGap: Version > LastVersion + 1 (Strict Sequence Stream)
    }
    
    DetectGap --> SyncStateRecovering: Sequence Gap Detected
    
    state SyncStateRecovering {
        [*] --> QueueRecoveryBuffer: Incoming WS messages buffered
        QueueRecoveryBuffer --> TryCommits: Provider has DepthCommits? (e.g. MEXC)
        TryCommits --> ApplyCommitsASC: Fetch depth_commits/1000 & Replay in ASC order
        ApplyCommitsASC --> DrainSortedBufferRec: Drain Buffered Updates (ASC order)
        
        TryCommits --> FallbackFullSnapshot: Commits Missing or Gaps Exceed Limit
        FallbackFullSnapshot --> FetchFullSnapshot: Call REST Full Snapshot
        FetchFullSnapshot --> DrainSortedBufferRec
    }
    
    SyncStateRecovering --> SyncStateReady: Gap Bridged Successfully
    SyncStateRecovering --> SyncStateUninitialized: Recovery Failed (Full Reset)
```

---

## 2. Sequence Diagrams

### 2.1 Symbol Subscription & Bootstrap Initialization (Zero-Gap Flow)

When dynamic universe discovery identifies a qualified symbol (e.g., top gainer), it triggers parallel subscription and buffer-based bootstrapping:

```mermaid
sequenceDiagram
    autonumber
    actor SubMgr as SubscribeManager
    participant WS as Exchange WebSocket
    participant Sync as OrderBookSynchronizer
    participant REST as Exchange REST API
    participant LocalBook as LocalOrderBook (Memory L2)

    SubMgr->>WS: 1. SubscribeDepth(exchange, symbol)
    SubMgr->>Sync: 2. InitializeSymbol(symbol)
    
    par WebSocket Delta Streaming
        WS-->>Sync: push.depth (Version: 1050)
        Note over Sync: State = Syncing<br/>Appends to tracker.buffer
        WS-->>Sync: push.depth (Version: 1051)
        Note over Sync: Appends to tracker.buffer
    and REST Snapshot Bootstrap
        Sync->>REST: GET /depth/{symbol}?limit=1000
        REST-->>Sync: 200 OK (Snapshot Version: 1000)
        Sync->>LocalBook: LoadSnapshot(snap 1000)
    end

    opt Bridge Gap Between Snapshot & Earliest Buffer Event
        Note over Sync: Gap between Snapshot (1000) and Buffer[0] (1050)
        Sync->>REST: GET /depth_commits/{symbol}/1000
        REST-->>Sync: 200 OK (Commits 1001..1049)
        Sync->>LocalBook: ApplyDelta(commits 1001..1049 in ASC order)
    end

    Note over Sync: Sort tracker.buffer ASC by Version
    Sync->>LocalBook: ApplyDelta(Buffer 1050)
    Sync->>LocalBook: ApplyDelta(Buffer 1051)
    
    Note over Sync: State = Ready
    Sync->>SubMgr: Synchronization Complete (Current Version: 1051)
```

---

### 2.2 Normal Real-Time Ingestion (Ready State)

```mermaid
sequenceDiagram
    autonumber
    participant WS as Exchange WebSocket Stream
    participant Sync as OrderBookSynchronizer
    participant LocalBook as LocalOrderBook
    participant Bus as EventBus (*eventbus.Bus)
    participant Wall as WallDetector

    WS->>Sync: ProcessUpdate(ob: Version 1052)
    
    alt Snapshot Mode (e.g. Toobit)
        Sync->>LocalBook: LoadSnapshot(ob)
    else Incremental Mode (e.g. MEXC, KuCoin)
        alt Version <= CurrentVersion
            Note over Sync: Stale message; discarded
        else Version == CurrentVersion + 1
            Sync->>LocalBook: ApplyDelta(Bids, Asks, Version 1052)
        end
    end

    Sync->>Bus: Publish("penny_jumper.depth.updated", symbolDepthEvent)
    Bus->>Wall: OnDepthUpdated(LocalBook snapshot)
```

---

### 2.3 Sequence Gap Detection & Packet Loss Recovery

When network jitter, packet drops, or WebSocket reconnection causes an incremental version gap (`event_version > current_version + 1`):

```mermaid
sequenceDiagram
    autonumber
    participant WS as Exchange WebSocket
    participant Sync as OrderBookSynchronizer
    participant REST as Exchange REST API
    participant LocalBook as LocalOrderBook

    Note over Sync: Local Book Version = 1100
    WS->>Sync: ProcessUpdate(ob: Version 1180)
    Note over Sync: Gap Detected (1180 > 1100 + 1)<br/>State -> SyncStateRecovering

    par Incoming Stream Continues
        WS-->>Sync: ProcessUpdate(ob: Version 1181)
        Note over Sync: Buffered in tracker.buffer
    and Strategy A: Depth Commits Recovery (MEXC)
        Sync->>REST: GET /depth_commits/{symbol}/1000
        alt Commits Available
            REST-->>Sync: 200 OK (Commits 1101..1179)
            Note over Sync: Sort Commits ASC by Version
            Sync->>LocalBook: Sequentially Apply Commits (1101 -> 1179)
            Sync->>LocalBook: Apply Pending Event (1180)
            
            Note over Sync: Sort tracker.buffer ASC
            Sync->>LocalBook: Drain Buffer (1181)
            Note over Sync: State -> SyncStateReady (Version = 1181)
        else Commits Unavailable / Failed
            Note over Sync: Strategy B: Fallback to Full REST Snapshot
            Sync->>REST: GET /depth/{symbol}?limit=1000
            REST-->>Sync: 200 OK (Snapshot Version = 1200)
            Sync->>LocalBook: LoadSnapshot(1200)
            Sync->>LocalBook: Drain Buffer (events > 1200)
            Note over Sync: State -> SyncStateReady (Version = 1200)
        end
    end
```

---

## 3. Buffer Ordering & Delta Merge Mechanics

### 3.1 Order Maintenance & Sorting

To prevent out-of-order execution, all buffered WebSocket events and REST commits are strictly sorted in **Ascending Order** by `Version` before application:

$$\text{Version}_0 < \text{Version}_1 < \dots < \text{Version}_n$$

```go
// Sort buffer ascending before draining
slices.SortFunc(validBuffer, func(a, b *domain.OrderBook) int {
    return cmp.Compare(a.Version, b.Version)
})
```

### 3.2 L2 Delta Application Rules

For each delta update:
- **`Volume > 0`**: Insert or update price level in the internal map and maintain sorted slices (Bids descending, Asks ascending).
- **`Volume == 0`**: Price level was filled or canceled; delete price level from book.

```
Incoming Delta:
  Bids: [{Price: 0.68485, Volume: 25.0}, {Price: 0.68450, Volume: 0}]
  Asks: [{Price: 0.68520, Volume: 10.0}, {Price: 0.68550, Volume: 0}]

Result in LocalOrderBook:
  - Bid level 0.68485 updated to 25.0 qty
  - Bid level 0.68450 removed from book
  - Ask level 0.68520 updated to 10.0 qty
  - Ask level 0.68550 removed from book
  - Book version bumped to update.Version
```

---

## 4. Multi-Exchange Strategy Matrix

| Exchange | WS Feed Channel | Sync Mode | Sequence Strictness | Primary Gap Recovery | Fallback Recovery |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **MEXC Futures** | `push.depth` | `INCREMENTAL` | `strictSequence: true` | `GET /api/v1/contract/depth_commits/{symbol}/1000` | Full Depth Snapshot (`GET /contract/depth`) |
| **KuCoin Futures** | `/contractMarket/level2Depth50` | `INCREMENTAL` | `strictSequence: true` | Full Level 2 Snapshot (`GET /api/v1/level2/snapshot`) | Full Re-sync |
| **Toobit Futures** | `depth` (Top 10/20 Snapshot) | `SNAPSHOT` | `strictSequence: false` | Self-healing on next WebSocket push | None needed |

---

## 5. Configuration Model

In `configs/penny_jumper/local/penny_jumper.jsonc`:

```jsonc
{
  "orderBookSync": {
    "maxBufferCapacity": 500,
    "snapshotTimeout": "5s",
    "commitRecoverySize": 1000,
    "exchanges": {
      "mexc_futures": {
        "mode": "INCREMENTAL",
        "strictSequence": true
      },
      "kucoin_futures": {
        "mode": "INCREMENTAL",
        "strictSequence": true
      },
      "toobit_futures": {
        "mode": "SNAPSHOT",
        "strictSequence": false
      }
    }
  }
}
```

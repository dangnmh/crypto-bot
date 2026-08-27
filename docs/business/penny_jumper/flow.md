# Penny Jumper Flow Overview

> Status: design contract. This document defines lifecycle, topics, state transitions and terminal outcomes. Strategy thesis is in [analyze.md](analyze.md); scoring and local model judgment is in [wall_trust_score.md](wall_trust_score.md); event sourcing is in [09_wall_event_sourcing_and_storage_flow.md](09_wall_event_sourcing_and_storage_flow.md); implementation boundaries are in [architecture.md](architecture.md).

## Target Flow

```mermaid
flowchart TD
    INIT["bot init<br/>config + stores + ws + bus"] --> JOBS["ticker + contract jobs"]
    JOBS --> FILTER["pre-filter symbols<br/>volume + contract + blacklist"]
    FILTER --> SUB["dynamic subscribe<br/>depth stream"]
    SUB --> DEPTH["depth update (TopicDepthUpdated)"]
    DEPTH --> DETECT["wall detector (Event Generator)"]
    DETECT -->|discrete event stream| STREAM["TopicWallEventStream\n(DepthStore Journal)"]
    STREAM --> JUDGE["Local Model / WallJudge\n(Evaluates []WallEvent)"]
    JUDGE -->|TrustScore < 0.75 / Spoof| SUPPRESS["Suppress / Defensively Cancel"]
    JUDGE -->|TrustScore >= 0.75 (Genuine)| QUALIFIED["TopicWallQualified"]
    QUALIFIED --> ROUTER["workflow manager"]
    ROUTER --> FSM["per-symbol FSM<br/>post-only jump"]
    FSM --> MONITOR["monitor order + event stream"]
    MONITOR -->|filled| EXIT["TP / trailing / wall monitor"]
    MONITOR -->|wall gone / weak / spoof / timeout| CANCEL["cancel pending order"]
    EXIT -->|TP / trailing / timeout| CLOSED["position closed"]
    EXIT -->|wall gone / consumed| BAILOUT["market bailout"]
    CANCEL --> DONE["terminal journal"]
    CLOSED --> DONE
    BAILOUT --> DONE
```

## Lifecycle Phases

### 1. Init And Readiness

| Step | Output |
|---|---|
| Load config | thresholds, max positions, symbol filters, exchange settings |
| Load contract metadata | price unit, volume unit, contract size |
| Load ticker snapshot | 24h volume, last price |
| Initialize stores | ticker, contract, depth (in-memory event journal) |
| Initialize bus | in-memory pub/sub topics |
| Initialize execution layer | order manager, order watcher, risk manager, wall judge |

Bot must not subscribe or trade until required stores are ready.

### 2. Pre-Filter And Dynamic Subscribe

| Step | Output |
|---|---|
| Filter ticker universe | symbols with enough volume and valid contracts |
| Apply blacklist | remove halted/delist/warning symbols |
| Diff current vs previous universe | `new_pairs`, `removed_pairs` |
| Subscribe new pairs | depth stream per symbol across configured exchanges |
| Remove stale pairs | only if no active workflow |

If a symbol has an active workflow, removal is delayed with `pendingRemoval`. Depth data must remain available until the workflow reaches a terminal state.

### 3. Wall Detection (Pure Event Generator)

The detector receives depth updates and emits discrete, immutable micro-events onto `TopicWallEventStream`:

| Micro-Event | Trigger Condition |
|---|---|
| `WALL_BORN` | Initial detection of large orderbook wall (`>= $20k`, `<= 1%` distance) |
| `WALL_MATURED` | Wall survived $\ge \text{MinLifespan}$ (e.g. 2s-5s) |
| `WALL_ABSORBED` | Taker orders executed at wall price (volume decreased) |
| `WALL_RESIZED` | Maker modified/added resting volume |
| `WALL_FLAPPED` | Wall returned at same price within grace period |
| `PRICE_APPROACHED` | Top-of-book moved closer to wall price |
| `WALL_WEAKENED` | Volume dropped below $50\%$ of initial size |
| `WALL_DISAPPEARED` | Wall cancelled or removed from orderbook |
| `WALL_CONSUMED` | Wall volume fully filled to 0 |

### 4. Wall Scoring & Local Model Judgment

The local model (`WallJudge`) subscribes to `TopicWallEventStream` and evaluates the stream `[]WallEvent`:

```text
TopicWallEventStream (WALL_MATURED, WALL_ABSORBED, WALL_RESIZED)
  -> WallJudge.JudgeWall(ctx, wall, []WallEvent)
  -> WallJudgeResult (TrustScore, IsTrusted, Reason)
  -> TopicWallQualified (if IsTrusted == true)
```

`TopicWallQualified` includes:

| Field | Reason |
|---|---|
| `symbol` | workflow key |
| `side` | bid wall = long, ask wall = short |
| `wall_price` | entry anchor |
| `wall_volume` | bailout/weakness baseline |
| `trust_score` | model evaluation score ($0.0 - 1.0$) |
| `is_trusted` | binary qualification gate |
| `best_bid`, `best_ask`, `spread_pct` | execution safety and anti-crossing verification |
| `timestamp` | latency and staleness guard |

### 5. Workflow Spawn

Workflow Manager may spawn a per-symbol FSM only when:

| Guard | Rule |
|---|---|
| Duplicate workflow | no active workflow for symbol |
| Concurrent risk | active workflows below configured max |
| Daily loss | bot not stopped by loss guard |
| Model qualification | `IsTrusted == true` (`TrustScore >= 0.75`) |
| Market data freshness | latest depth/ticker not stale |

### 6. Entry

The FSM places a post-only maker order:

| Wall side | Position | Order side | Entry price |
|---|---|---|---|
| Bid wall | Long | Buy | `min(wall_price + 1 tick, best_ask - 1 tick)` |
| Ask wall | Short | Sell | `max(wall_price - 1 tick, best_bid + 1 tick)` |

Strict bounds prevent crossing rejections (`PostOnlyCrossing`). If post-only placement fails, the workflow ends as `entry_rejected`.

### 7. Pending Order Monitoring

While the maker order is pending, the FSM listens to wall event stream updates:

| Event | Action |
|---|---|
| `WALL_DISAPPEARED` | cancel order immediately |
| `WALL_WEAKENED` / Vol $< 50\%$ | cancel order |
| Model flags spoofing on `WALL_RESIZED` | cancel order |
| Pending timeout | cancel order |
| Order filled | transition to post-fill exit |
| Partial fill below minimum | market exit or cancel remainder, then terminal |

### 8. Post-Fill Exit

After fill:

| Exit path | Trigger | Action |
|---|---|---|
| Maker TP | TP order fills | close as `tp` |
| Trailing | profit exceeds activation and trailing stop hits | close as `trailing` |
| Bailout | original wall disappears / consumed | market exit |
| Time stop | TP not filled before timeout | market exit |
| Critical error | close/cancel fails after bounded retries | alert + journal critical state |

---

## Event Topic Convention

Topic names use a `penny_jumper.<scope>.<event>` namespace:

| Scope | Topic | Description |
|---|---|---|
| Depth | `penny_jumper.depth.updated` | Raw orderbook updates |
| Wall Stream | `penny_jumper.wall.event.stream` | Discrete event sourcing stream (`[]WallEvent`) |
| Wall Legacy | `penny_jumper.wall.detected` | Newly matured active wall |
| Wall Legacy | `penny_jumper.wall.changed` | Active wall volume/level updated |
| Wall Legacy | `penny_jumper.wall.disappeared` | Active wall cancelled/pulled |
| Qualification | `penny_jumper.wall.qualified` | Qualified by local model (`WallJudge`) |
| Workflow | `penny_jumper.workflow.*` | Per-symbol FSM transitions |
| Order | `penny_jumper.order.*` | Order execution and fill updates |

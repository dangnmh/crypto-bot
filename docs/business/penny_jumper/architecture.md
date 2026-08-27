# Penny Jumper Technical Architecture

> Status: target architecture. This document defines runtime boundaries, modules, event bus, stores, FSM and safety constraints.

## Architecture Rule

Penny Jumper is an isolated bot. It may reuse infrastructure packages, but it must not share runtime state with Funding or any other bot.

| Area | Rule |
|---|---|
| Entry point | dedicated `cmd/penny_jumper/main.go` |
| Config | dedicated `configs/penny_jumper/*` |
| WebSocket | dedicated connection pool/multiplexer instance |
| Stores | dedicated ticker, contract, depth (with in-memory wall event journal) |
| Event bus | dedicated in-memory bus instance |
| Wall Judge | local model / SLM / rule-based classifier |
| Orders | dedicated workflow/order manager state |

## High-Level Architecture

```mermaid
flowchart TD
    subgraph Exchanges["Supported Exchanges (MEXC, Toobit, KuCoin, etc.)"]
        REST["REST API"]
        WS["WebSocket API"]
    end

    subgraph Bot["Penny Jumper Bot - Isolated"]
        subgraph Jobs["Background Jobs"]
            TICKER["Ticker24h Job"]
            CONTRACT["Contract Job"]
        end

        subgraph Store["Local Stores"]
            TS[("TickerStore")]
            CS[("ContractStore")]
            DS[("DepthStore\n(In-Memory Wall Event Journal)")]
        end

        SUB["Subscribe Manager"]

        subgraph Pipeline["Event Sourced Pipeline"]
            OB["OB Builder"]
            DET["Wall Detector\n(Pure Event Generator)"]
            JUDGE["Wall Judge / Local Model\n(Evaluates []WallEvent)"]
            WM["Workflow Manager"]
        end

        subgraph Runtime["Active Workflows"]
            FSM1["FSM: symbol A"]
            FSM2["FSM: symbol B"]
        end

        subgraph Exec["Execution"]
            OM["Order Manager"]
            RM["Risk Manager"]
            OW["Order Watcher"]
        end

        subgraph Persistence["Storage"]
            REPO[("WallRepository\n(penny_jumper_walls)")]
        end
    end

    REST --> TICKER
    REST --> CONTRACT
    TICKER --> TS
    CONTRACT --> CS
    TICKER --> SUB
    TS --> SUB
    CS --> SUB
    SUB --> WS
    WS --> OB
    OB --> DS
    OB --> DET
    DET -->|"TopicWallEventStream"| DS
    DET -->|"TopicWallEventStream"| JUDGE
    DET -->|"TopicWallEventStream"| REPO
    JUDGE -->|"TopicWallQualified"| WM
    WM --> FSM1
    WM --> FSM2
    FSM1 --> OM
    FSM2 --> OM
    OM --> REST
    OW --> FSM1
    OW --> FSM2
    WS --> OW
    FSM1 --> RM
    FSM2 --> RM
```

## Event Bus Topics

The pipeline uses an in-memory pub/sub bus.

```mermaid
flowchart LR
    OB["OB Builder"] -->|"penny_jumper.depth.updated"| BUS["In-memory Bus"]
    DET["Wall Detector"] -->|"penny_jumper.wall.event.stream"| BUS
    DET -->|"penny_jumper.wall.detected"| BUS
    DET -->|"penny_jumper.wall.changed"| BUS
    DET -->|"penny_jumper.wall.disappeared"| BUS
    JUDGE["Wall Judge / Local Model"] -->|"penny_jumper.wall.qualified"| BUS
    OW["Order Watcher"] -->|"penny_jumper.order.*"| BUS

    BUS --> DET
    BUS --> JUDGE
    BUS --> WM["Workflow Manager"]
    BUS --> FSM["Symbol FSM"]
    BUS --> RUNNER["PennyJumperRunner (DB Persistence)"]
```

## Pipeline Stages

| Stage | Input | Output |
|---|---|---|
| OB Builder | raw depth WS message | depth store update + `penny_jumper.depth.updated` |
| Wall Detector | depth update | discrete `WallEvent`s on `penny_jumper.wall.event.stream` |
| Wall Judge | `[]WallEvent` from `DepthStore` | `WallJudgeResult` + `penny_jumper.wall.qualified` |
| Workflow Manager | qualified wall | per-symbol FSM or skip |
| Symbol FSM | qualified wall + event stream | post-only front-running order + position monitoring |

# Crypto-Bot Architecture

This document describes the high-level architecture and project structure of the `crypto-bot` trading application.

## Overview

The `crypto-bot` is designed using a **Domain-Driven Design (DDD)** and **Clean Architecture** approach. This ensures that the core business logic is isolated from the infrastructure and external dependencies. This structure allows for the bot's trading strategies (like "Funding Reversion" and "Penny Jumper") to be developed, tested, and scaled independently of the underlying exchange API, WebSocket handling, and data storage.

## Directory Structure

The project follows the standard Go project layout with a clean separation between bots, shared domain, and infrastructure:

### `cmd/`
Contains the entrypoints for the application. Each subdirectory represents a standalone executable bot.
- `funding_reversion/`: The main entrypoint for the Funding Reversion trading bot.
- `penny_jumper/`: The main entrypoint for the Penny Jumper trading bot.
*Note: `main.go` handles configuration loading, engine initialization, wiring of dependencies, and graceful shutdown.*

### `internal/`
Contains private application and library code. This is where the bulk of the system logic resides.

#### `internal/domain/`
The shared core of the application, defining fundamental business rules, abstractions, and types (e.g., `Order`, `OrderBook`, `Event`). It is decoupled from all external dependencies and is used across all bots and infrastructure.

#### `internal/infrastructure/`
Contains the concrete implementations for external services. This layer interacts with the outside world.
- `app/`: Engine initialization and background service orchestration (`StoreRegistry`, `BotLifecycle`). Standardizes the startup sequence.
- `config/`: System-level configuration loaders.
- `exchange/`: Exchange clients and API integrations.
  - `mexc/`: MEXC-specific logic, including the `WsAdapter` implementation for handling raw MEXC WebSockets.
- `store/`: Pure, domain-agnostic in-memory data stores (e.g., `PriceStore`, `DepthStore`) that consume strictly domain models.
- `timesync/`: Network time synchronization services to ensure accurate execution timings.
- `ws/`: The generic WebSocket pool, `EventRouter`, and the `ExchangeAdapter` interface.

#### `internal/bots/`
Contains the specific trading strategies and bots. Each bot is an isolated micro-application.
- `funding_reversion/` & `penny_jumper/`:
  - `application/`: The application service layer. Contains the orchestrator, state machine (`FSM`), and logic specific to the strategy. It glues the bot's domain logic with the infrastructure.
  - `domain/`: Specific domain models for this particular strategy.
  - `config/`: Bot-specific configuration structures.

### `pkg/`
Contains public library code that can be shared across multiple internal services or even external projects.
- `logger/`: Standardized logging utilities (wrapping `slog`).
- `ticker/`: Reusable ticker or timing utilities.
- `ws/`: Generic WebSocket connection and pooling library.

### `configs/`
Contains JSONC configuration files used to tune both the system engine (`system.jsonc`) and the individual bot strategies (e.g., `funding.jsonc`).

### `docs/`
Contains project documentation, strategies, analysis (like front-running analysis), and technical specifications.

## Architecture Flow

1. **Initialization:** The `cmd` package loads the system configuration and initializes the `Engine` from `internal/infrastructure/app`.
2. **Infrastructure Setup:** The engine sets up concrete implementations for the exchange API, data stores, and time sync. Crucially, it injects an `ExchangeAdapter` (e.g., `mexc.WsAdapter`) into the generic WebSocket pool.
3. **Bot Orchestration:** The `cmd` package instantiates the specific bot, passing the engine as a dependency, and calls `BotLifecycle.StartStoresAndWait()`.
4. **Execution:** The bot's application layer orchestrates the trading strategy:
    - Subscribes to real-time data via the generic `Engine.Adapter.Subscribe*()` methods.
    - `EventRouter` routes parsed domain events from the WS Adapter to the Stores and FSMs.
    - Monitors state and triggers actions based on domain rules.
    - Executes trades using the generic exchange client.
5. **State Management:** The application maintains its trading state via FSMs and delegates specific tactical actions to dedicated components.

## Infrastructure Patterns

### Exchange Adapter Pattern
To maintain a multi-exchange architecture, the `internal/infrastructure/ws` package defines an `ExchangeAdapter` interface. The underlying `pkg/ws` handles raw connections, while the adapter defines exchange-specific rules (Ping formats, Authentication hooks, and JSON-to-Domain-Model parsing). This keeps the Stores and Router entirely agnostic of whether they are running on MEXC, Binance, or Bybit.

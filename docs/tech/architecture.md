# Crypto-Bot Architecture

This document describes the high-level architecture and project structure of the `crypto-bot` trading application.

## Overview

The `crypto-bot` is designed using a **Domain-Driven Design (DDD)** and **Clean Architecture** approach. This ensures that the core business logic is isolated from the infrastructure and external dependencies. This structure allows for the bot's trading strategies (like "Funding Reversion") to be developed, tested, and scaled independently of the underlying exchange API, WebSocket handling, and data storage.

## Directory Structure

The project follows the standard Go project layout:

### `cmd/`
Contains the entrypoints for the application. Each subdirectory represents a standalone executable bot.
- `funding_reversion/`: The main entrypoint for the Funding Reversion trading bot. `main.go` handles configuration loading, engine initialization, wiring of dependencies, and graceful shutdown.

### `internal/`
Contains private application and library code. This is where the bulk of the system logic resides.

#### `internal/core/`
The heart of the application, defining the fundamental business rules and abstractions. It is decoupled from all external dependencies.
- `domain/`: Contains the core domain models and entities (e.g., Order, Account, Market).
- `ports/`: Defines the interfaces (ports) for external services like stores, exchange APIs, and WebSockets. The core interacts with the outside world *only* through these interfaces.

#### `internal/infrastructure/`
Contains the concrete implementations for the interfaces defined in `internal/core/ports/`. This layer interacts with external systems.
- `app/`: Engine initialization and background service orchestration.
- `config/`: System-level configuration loaders.
- `exchange/`: REST API clients for the cryptocurrency exchange (e.g., placing orders, fetching balances).
- `store/`: In-memory or persistent data stores (e.g., global state for tickers and contracts).
- `timesync/`: Network time synchronization services to ensure accurate execution timings.
- `ws/`: WebSocket clients for real-time market data streaming.

#### `internal/bots/`
Contains the specific trading strategies and bots.
- `funding_reversion/`: Implementation of the Funding Reversion strategy.
  - `application/`: The application service layer. Contains the orchestrator (`sniper.go`), state machine (`state.go`), opening logic (`opener.go`), trailing stop logic (`trailing.go`), and WebSocket subscription handling (`subscription.go`). It glues the domain logic with the infrastructure.
  - `domain/`: Specific domain models for the Funding Reversion strategy.
  - `config/`: Bot-specific configuration structures.

### `pkg/`
Contains public library code that can be shared across multiple internal services or even external projects.
- `logger/`: Standardized logging utilities (wrapping `slog`).
- `ticker/`: Reusable ticker or timing utilities.

### `configs/`
Contains JSONC configuration files used to tune both the system engine (`system.jsonc`) and the individual bot strategies (`funding.jsonc`).

### `docs/`
Contains project documentation, strategies, analysis (like front-running analysis), and technical specifications.

## Architecture Flow

1. **Initialization:** The `cmd` package loads the system configuration and initializes the `Engine` from `internal/infrastructure/app`.
2. **Infrastructure Setup:** The engine sets up concrete implementations for exchange APIs, WebSockets, data stores, and time sync, implementing the `ports` from `internal/core`.
3. **Bot Orchestration:** The `cmd` package then instantiates the specific bot (e.g., `Sniper` in `funding_reversion/application`), passing the infrastructure interfaces as dependencies.
4. **Execution:** The bot's application layer orchestrates the trading strategy:
    - Subscribes to real-time data via WebSocket ports.
    - Monitors state and triggers actions based on domain rules.
    - Executes trades using the exchange REST API ports.
5. **State Management:** The application maintains its trading state (`state.go`) and delegates specific tactical actions (like `opener.go` or `trailing.go`) to dedicated components.

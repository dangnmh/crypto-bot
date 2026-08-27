# Crypto-Bot Coding Conventions

> Last updated: 2026-05-12

This document outlines the standard coding conventions established for the `crypto-bot` project. Adherence to these standards ensures the codebase remains maintainable, scalable, and resilient as we scale up High-Frequency Trading (HFT) and multi-bot architectures.

We follow the [Uber Go Style Guide](style.md) as our baseline. This document extends it with project-specific rules and Go best practices that the style guide does not cover.

For testing conventions, please refer to [Testing Conventions](testing_conventions.md).
For third-party library conventions, please refer to [Community Libraries](community_libraries.md).

---

## 1. Architecture & Design

### 1.1 Clean Architecture & Domain-Driven Design (DDD)

- **Separation of Concerns:** Business logic must be strictly decoupled from infrastructure implementation.
- **Layers:**
  - `internal/domain`: Core business rules, entities, and interfaces (Ports). **No dependencies on external libraries or infrastructure.**
  - `internal/bots/{bot_name}/application`: Orchestrates business logic, use cases, and FSMs. Interacts with domain interfaces.
  - `internal/bots/{bot_name}/domain`: Bot-specific domain logic (strategies, scoring, pricing).
  - `internal/infrastructure`: External implementations (Adapters) such as Exchange APIs, WebSockets, Stores, and Observability.
  - `pkg/`: Shared, reusable libraries that could be imported by external projects.

### 1.2 Dependency Rule

- **Domain** imports nothing from infrastructure or bots.
- **Infrastructure** imports domain (for types/interfaces), never bots.
- **Bots** import domain + infrastructure.
- **Cmd** wires everything together.
- **Pkg** imports nothing from `internal/`.

### 1.3 State Management & Execution Flow

- **Finite State Machines (FSM):** Complex trading flows must be implemented using state machines to prevent chaotic state transitions.
- **Dependency Injection:** Components must receive their dependencies via constructors rather than relying on global variables.
- **Isolated Goroutines:** Each trading symbol must run in its own fully isolated Goroutine. Data sharing between workers is prohibited unless mediated by thread-safe shared stores with `sync.RWMutex`.

### 1.4 Multi-Exchange Partitioning: Exchange Unit Isolation Pattern (Instance-Per-Exchange)

When designing stores, synchronizers, detectors, or execution units that handle multiple exchanges:

- **Strict Unit Isolation:** Every exchange operates on its own dedicated instance (e.g., `*DepthStore`, `*CandidateStore`, `*WallDetector`, `orderbook.Synchronizer`).
- **Clean Symbol-Only Signatures:** Store and processor methods take only `symbol` (e.g., `GetDepth(symbol)`, `HasActiveTrade(symbol)`, `ProcessOrderBook(ctx, ob, now)`), avoiding composite keys like `GetDepth(exchange, symbol)`.
- **Top-Level Orchestration via Maps:** Orchestrators and runners hold instances partitioned by exchange (e.g., `map[string]*DepthStore`, `map[string]orderbook.Synchronizer`), routing events directly to the target exchange unit.
- **Key Benefits:**
  - **Zero Cross-Contention:** Independent mutexes eliminate lock contention across exchanges.
  - **Fault Domain Isolation:** Issues, reconnects, or flushes on one exchange do not impact state on another.
  - **Simplified Testing:** Unit tests test single-exchange domain logic directly without composite keys or multi-tenant mock plumbing.

---

## 2. Interface Design

### 2.1 Accept Interfaces, Return Structs

Functions should accept interfaces as parameters and return concrete struct types. This is the foundational Go idiom for loose coupling.

- **Producers** (e.g., `store.NewTickerStore()`) return concrete `*TickerStore`.
- **Consumers** (e.g., `symbolWorker`) accept `store.TickerReader` (an interface).
- Never return an interface from a constructor unless you have a compelling reason to hide the concrete type.

### 2.2 Interface Ownership — Consumer Defines, Producer Implements

Interfaces should be defined **where they are consumed**, not where they are implemented.

- The consumer knows what methods it needs; the producer should not dictate that.
- Small, focused interfaces are preferred over large ones (see ISP below).
- **Pragmatic exception:** When multiple consumers need the same interface (e.g., `TickerReader` is used by multiple bots), centralizing the interface in the producer package avoids duplication. This is an acceptable trade-off.

### 2.3 Interface Segregation Principle (ISP)

Prefer many small interfaces over one large interface. Each interface should represent a single coherent capability.

- Split read and write operations into separate interfaces (e.g., `PriceReader` vs. `PriceWriter`).
- Compose larger interfaces from smaller ones only when a consumer genuinely needs the full set (e.g., `KlineReadWriter`).
- A consumer that only reads should accept `DepthReader`, not `*DepthStore`.

### 2.4 Verify Interface Compliance

Use compile-time interface compliance checks to ensure concrete types satisfy their interfaces. Place these in the file where the interface is defined.

### 2.5 Pointers to Interfaces

Never use pointers to interfaces (`*SomeInterface`). Interfaces are already reference types internally. Passing `*SomeInterface` is almost always a bug.

### 2.6 Generics (Type Parameters)

Use generics to eliminate repetitive boilerplate where the logic is **identical** across types and only the concrete type varies. Prefer generics over `any`/`interface{}` + type assertions when the type can be statically known.

#### When to Use

- **Deserialization helpers** — avoid repeating unmarshal + error-wrap for every event/config type:
  ```go
  func unmarshal[T any](data []byte) (T, error) {
      var v T
      if err := json.Unmarshal(data, &v); err != nil {
          return v, fmt.Errorf("unmarshal %T: %w", v, err)
      }
      return v, nil
  }
  ```
- **Response envelope parsing** — one function handles every API endpoint:
  ```go
  func ParseResponse[T any](body []byte, path string) (T, error)
  ```
- **Generic wrapper structs** — typed containers where the shape is fixed but the payload varies:
  ```go
  type APIResponse[T any] struct {
      Success bool   `json:"success"`
      Code    int    `json:"code"`
      Data    T      `json:"data"`
  }
  ```
- **Configuration loaders** — read file + unmarshal into any config struct:
  ```go
  func Load[T any](path string) (*T, error)
  ```

#### When NOT to Use

- **Don't use generics just because you can.** If there are only 1–2 concrete types, a regular function is clearer.
- **Don't add type parameters to methods on concrete structs.** Go does not allow type parameters on methods — use top-level generic functions instead.
- **Don't use generics for domain logic.** Domain types (e.g., `Candidate`, `CycleRecord`) should be concrete. Generics belong in infrastructure/utility layers.
- **Don't create generic interfaces.** Prefer non-generic interfaces with concrete method signatures. A `Repository[T]` interface is almost never the right abstraction in Go.

#### Naming Conventions

- Use short, conventional names for unconstrained type parameters: `T`, `U`, `V`.
- Use descriptive names when the constraint communicates intent: `Elem`, `Key`, `Result`.
- Constraint interfaces go in the same file as the generic function if single-use, or in `pkg/` if shared.

---

## 3. Constructor & Configuration Patterns

### 3.1 Functional Options for Extensible APIs

Use the functional options pattern (`func(*Config)`) for constructors that have more than 2-3 optional parameters. This is preferred over config structs for public APIs.

- Each option should be a self-contained function that configures one aspect.
- Options must be safe to apply in any order.
- Required parameters go as regular function arguments, not options.

### 3.2 Builder Pattern for Complex Wiring

Use the builder pattern with validation for objects that require multiple mandatory dependencies and need build-time validation (e.g., `EngineBuilder`).

- `Build()` must return an error if required fields are missing.
- Builder methods should return `*Builder` for fluent chaining.

### 3.3 Constructor Naming

- Public constructors: `NewXxx()` returns `*Xxx`.
- Factory functions that select implementations: `newSlippageCalculator()` returns an interface.
- Never expose struct fields directly when a constructor exists. Callers must go through the constructor.

---

## 4. Error Handling

### 4.1 Handle Errors Once

Every error should be handled exactly once: either log it, return it, or degrade gracefully. Never log and return the same error.

### 4.2 Wrap with Context

When returning errors from callees, wrap them with `fmt.Errorf("context: %w", err)` to provide a trace. Avoid "failed to" prefixes — they pile up through the stack.

### 4.3 Structured Error Types

Use typed errors (e.g., `exchange.APIError`, `exchange.RateLimitError`) for errors that callers need to match with `errors.As`. Use sentinel errors (`var ErrNotFound = errors.New(...)`) for errors matched with `errors.Is`.

### 4.4 Don't Panic

Production code must never panic. Return errors and let the caller decide. Panics are only acceptable for truly irrecoverable situations during program initialization.

---

## 5. Concurrency

### 5.1 Context Propagation

`context.Context` must be passed as the first argument to every blocking or I/O function. This ensures graceful shutdowns and timeout management. Never use `context.Background()` inside library code — always propagate the caller's context.

### 5.2 Don't Fire-and-Forget Goroutines

Every goroutine must have a clear shutdown mechanism. Use `sync.WaitGroup` or done channels to wait for goroutines to exit. Never use `time.Sleep` in production code to wait for goroutines.

### 5.3 Channel Size is One or None

Channels should be unbuffered (size 0) or buffered with size 1. Any other size requires explicit justification in comments explaining why and what happens under backpressure.

### 5.4 Protect Shared State

All shared mutable state must be protected by `sync.RWMutex`. Use `RLock` for readers and `Lock` for writers. Prefer short critical sections — lock, copy, unlock, then process.

### 5.5 Zero-value Mutexes

Declare mutexes as value fields on structs, not pointers. The zero value of `sync.Mutex` is valid. Never embed mutexes — use a named field (e.g., `mu sync.RWMutex`).

---

## 6. Financial Precision

### 6.1 No Floating-Point for Money

All price, volume, and fee calculations must use `pkg/decmath` (which wraps `shopspring/decimal`). Never use `float64` for financial arithmetic.

### 6.2 Scale Awareness

Always snap prices and volumes to their correct scale using `decmath.RoundToScale()` and `decmath.SnapToTick()` before submitting orders.

---

## 7. Observability & Logging

### 7.1 Structured Logging

Use `log/slog` exclusively. Never use `fmt.Print` or `log.Print`. Every constructor should accept or create a logger with a `component` tag.

### 7.2 Correlation IDs

Every trading cycle must generate and inject a Correlation ID (`req_id`) into the context using `observability.WithCorrelationID(ctx)`. The `TraceHandler` will automatically attach this ID to all logs within that cycle.

### 7.3 Logger Injection

Prefer injecting `*slog.Logger` via constructors rather than using `slog.Default()`. This enables per-component log routing and testability.

---

## 8. Naming

### 8.1 Package Names

- All lower-case, no underscores, no plurals.
- Short and descriptive: `store`, `exchange`, `decmath`.
- Avoid generic names: never `util`, `common`, `helpers`, `lib`.

### 8.2 Variable and Function Names

- Use MixedCaps (Go convention), never snake_case.
- Prefix unexported top-level variables and constants with `_`.
- Error variables use `Err` prefix (exported) or `err` prefix (unexported).
- Error types use `Error` suffix (e.g., `APIError`, `RateLimitError`).

### 8.3 Interface Names

- Single-method interfaces use the method name + `er` suffix (e.g., `Reader`, `Writer`).
- Multi-method interfaces use a descriptive noun (e.g., `TickerReader`, `ExchangeAdapter`).
- Don't prefix interfaces with `I` — this is not Go style.

---

## 9. Code Organization

### 9.1 Import Ordering

Three groups separated by blank lines:
1. Standard library
2. Internal project packages (`crypto-bot/...`)
3. External dependencies

### 9.2 Function Ordering

Within a file, order functions as:
1. Type definition
2. Constructor (`NewXxx`)
3. Exported methods (in rough call order)
4. Unexported methods
5. Free helper functions

### 9.3 Avoid Long Lines

Soft limit of 99 characters per line. Wrap before hitting the limit, but it is not a hard rule.

### 9.4 Use Field Tags in Marshaled Structs

Every struct field that is serialized to JSON, YAML, or similar must have the appropriate struct tag. This makes the serialization contract explicit and protects against renames.

### 9.5 Prefer Early Returns (Guard Clauses) Over Nested Conditionals

- Avoid deeply nested `if` blocks.
- Handle non-matches, inverted preconditions, and error conditions using early returns (`if !condition { return ... }`).
- Keep the happy-path logic un-nested at the main method level for maximum readability.

---

## 10. Dependency Injection & Mandatory Constructor Validation

### 10.1 Everything Must Be Provided at Initialization (`New*` Constructor Pattern)

- All core components and dependencies (e.g. `Clock`, `Client`, `OrderManager`, `TradeReportRepository`, `Notifier`) must be provided via Dependency Injection (`fx` or constructor calls).
- **Constructors (`New*`) MUST validate required dependencies**: If a mandatory dependency parameter is missing or `nil`, the `New*` constructor must return an explicit error (e.g., `return nil, fmt.Errorf("missing required dependency...")`).
- **No Defensive Runtime Nil Checks**: Because constructor validation guarantees non-nil dependencies, method bodies and execution paths must **NOT** contain defensive `nil` checks (e.g. `if m.repo == nil`, `if j.pnlReader == nil`). Assume all dependencies are valid during execution.
- **Unit and Integration Tests:** Test setups must provide valid dependency instances or test mocks rather than passing `nil`.

### 10.2 Configuration Struct Validation Tags

- **Use Struct Validation Tags (`validate:"..."`)**: All configuration structs must define validation tags (`validate:"required"`, `validate:"gt=0"`, etc. via `go-playground/validator`) to validate configuration parameters at load time.
- **No Imperative Fallback Checks in Application Logic**: Application logic and strategy generators must **NOT** write defensive default patching or imperative fallback checks (e.g. `if scalePct <= 0 { scalePct = 50.0 }`, `if tp <= 0 { tp = 0.5 }`). Depend strictly on the validated configuration struct.

### 10.3 Modular Fx Wiring per Folder (`module.go` Pattern)

To keep the application dependency graph maintainable, testable, and strictly decoupled, each component folder/package that exposes injectable services, repositories, or background jobs must define its own `module.go` file.

- **Exported `Module` Option:** Each package defines and exports `var Module = fx.Options(...)` bundling its providers and lifecycle invocations.
- **Provider Functions:** Define explicit `ProvideXxx(...)` functions or supply constructors (e.g. `NewXxx`) in `module.go`.
- **Submodule Composition:** Parent domain or application packages compose child modules (e.g. `application.Module` aggregates `reversion.Module` and `obfuscator.Module`).
- **Clean Root Bootstrap:** High-level bootstrap entrypoints (e.g. `bootstrap.Module`) compose these exported package modules rather than maintaining a monolithic provider list.
- **No Import Cycles:** Ensure strict unidirectional module imports. Packages that define interfaces (e.g. `ordermanager`) must not import child implementation packages (e.g. `ordermanager/persistence`) if the child already imports the parent.

#### Example (`internal/bots/funding/application/obfuscator/module.go`):

```go
package obfuscator

import (
	"go.uber.org/fx"
)

// Module wires obfuscator dependencies and lifecycle hooks.
var Module = fx.Options(
	fx.Provide(
		ProvideOrderGenerator,
		ProvideObfuscatorDispatcher,
		ProvideObfuscatorRunner,
		ProvideObfuscatorJob,
	),
	fx.Invoke(
		RegisterObfuscatorCompletionCallback,
	),
)

func ProvideOrderGenerator(engine *infraapp.Engine) (*OrderGenerator, error) {
	return NewOrderGenerator(engine)
}
// ...
```

#### Example Composition (`internal/bots/funding/bootstrap/module.go`):

```go
func Module(paths ConfigPaths) fx.Option {
	return fx.Options(
		fx.Supply(paths),
		exchange.Module,
		observability.Module,
		server.Module,
		infraapp.Module,
		ordermanager.Module,
		ordermanagerpersistence.Module,
		persistence.Module,
		application.Module,
		fx.Provide(
			provideSystemConfig,
			provideBaseSystemConfig,
			provideLogger,
			provideFundingConfig,
			provideNotifier,
			provideEngine,
			provideClock,
			provideDatabase,
			provideGoCache,
		),
	)
}
```




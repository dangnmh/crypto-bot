# Crypto-Bot Testing Conventions

> Last updated: 2026-05-12

This document outlines the standard testing conventions established for the `crypto-bot` project. Adherence to these standards ensures the codebase remains maintainable, scalable, and resilient.

For coding conventions, please refer to [Coding Conventions](coding_conventions.md).

---

## 1. Testing Conventions

Our goal is a **strict minimum of 90% test coverage** for all packages, enforced via CI pipelines. All new code MUST include comprehensive unit tests.

### 1.1 Test Frameworks & Libraries

- **Standard Library + Testify:** Use the native `testing` package alongside `github.com/stretchr/testify` for assertions.
  - Use `assert.Equal(t, ...)` for soft assertions (test continues on failure).
  - Use `require.NoError(t, err)` for hard assertions (test aborts on failure).
- **Mocking:** Use `go.uber.org/mock/gomock` for generating and utilizing mock interfaces. All external I/O (exchange APIs, WebSockets) must be mocked in unit tests.

### 1.2 Blackbox Testing

- **Always use blackbox testing:** Test packages should be declared with the `_test` suffix (e.g., `package mypkg_test` instead of `package mypkg`). This ensures you only test the public API of the package, exactly as a consumer would use it.
- **Do not export for testing:** Never export a function, variable, or struct field solely for the purpose of making it accessible to tests. If it is internal logic, it should remain unexported and be tested implicitly through the public API.

> [!NOTE]
> **Refactor over workarounds:** If the code is too complicated to write a blackbox test for, do not break these rules. Instead, you must **refactor the code**: split the function, apply clean code principles and design patterns, and strictly follow the [Coding Conventions](coding_conventions.md) to decouple the logic so it can be tested properly.

### 1.3 Table-Driven Tests

Always use table-driven tests for testing multiple scenarios (especially validation logic and state transitions). Use `tests` for the slice name, `tt` for each case, and `give`/`want` prefixes for inputs and outputs.

### 1.4 Parallel Tests

- **`t.Parallel()` is mandatory:** Every test function and every sub-test must call `t.Parallel()` at the very beginning.
- This forces tests to run concurrently, exposing race conditions and reducing CI time.

### 1.5 No Sleep in Tests

Avoid `time.Sleep` in tests. Use mocked Clocks (`domain.Clock`), context timeouts, or channels to synchronize test events deterministically.

### 1.6 Avoid Unnecessary Complexity in Table Tests

Do not use conditional assertions or branching logic inside table-driven subtests. If a test case requires different mock setups or different assertion paths, split it into a separate `Test...` function.

### 1.7 Infrastructure Testing

- **HTTP/REST Clients:** Use `net/http/httptest` to spin up local mock servers.
- **WebSockets:** Use `httptest.NewServer` with `gorilla/websocket` to test connection lifecycle.
- **Data Stores:** Test thread-safe stores for data races using `go test -race`.

---

## 2. Pre-Commit Quality Gates

Before pushing code, always verify compliance using the Makefile commands:

- `make lint` — Runs `golangci-lint` to check code style and detect bugs.
- `make test` — Runs all tests with race detection.
- `make cover-check` — Verifies that total coverage exceeds the 90% threshold.

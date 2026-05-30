# Community Library Conventions

This document outlines the standard third-party community libraries adopted in the `crypto-bot` project and the conventions for when and how to use them.

Before writing custom helpers or utility functions, check if the desired functionality is already provided by one of these community libraries.

---

## 1. Adopted Community Libraries

### 1.1 samber/lo (Slice & Map Utilities)
We use `github.com/samber/lo` as our standard library for collection manipulation and functional programming helpers.

- **Convention:** Prefer `lo` functions over custom `for` loops for basic map, filter, reduction, or containment checks.
- **Examples:**
  - Map elements: `names := lo.Map(users, func(u User, _ int) string { return u.Name })`
  - Filter list: `active := lo.Filter(users, func(u User, _ int) bool { return u.IsActive })`
  - Check containment: `if lo.Contains(supportedSymbols, symbol) { ... }`
  - Deduplicate: `unique := lo.Uniq(ids)`

### 1.2 go-playground/validator (Struct Validation)
We use `github.com/go-playground/validator/v10` for validating configuration schemas, API requests, and system boundaries.

- **Convention:** Annotate struct definitions with `validate` tags instead of writing long, manual `if x == ""` validation blocks.
- **Examples:**
  ```go
  type BotConfig struct {
      Exchange string   `json:"exchange" validate:"required,oneof=bybit mexc okx"`
      Symbols  []string `json:"symbols" validate:"required,dive,required"`
      MinVol   float64  `json:"min_volume" validate:"required,gt=0"`
  }
  ```

### 1.3 uber-go/fx (Dependency Injection)
We use `go.uber.org/fx` for dependency injection and application lifecycle orchestration, specifically at program startup.

- **Convention:** All complex command entry points (`cmd/`) must use `fx.App` to wire components (adapters, application services, bots) and manage their startup/shutdown lifecycles.
- **Examples:**
  ```go
  func main() {
      fx.New(
          fx.Provide(
              config.Load,
              db.NewStore,
              bybit.NewClient,
          ),
          fx.Invoke(
              bootstrap.StartBot,
          ),
      ).Run()
  }
  ```

### 1.4 uber-go/mock/gomock (Mocking)
We use `go.uber.org/mock/gomock` to mock external adapters and dependencies in unit tests.

- **Convention:** Never hand-roll mock implementations. Generate mocks using `mockgen` and verify call behaviors using `gomock` controllers.
- **Examples:**
  ```go
  ctrl := gomock.NewController(t)
  defer ctrl.Finish()

  mockExchange := mock_exchange.NewMockClient(ctrl)
  mockExchange.EXPECT().
      GetRecentClosedPnL(gomock.Any(), "BTCUSDT").
      Return(expectedPnL, nil)
  ```

---

## 2. When to Use Community Libraries

1. **Prioritize Standard Libraries:** If the standard library (`slices`, `maps`, `errors`) provides a direct, highly readable solution, use it.
2. **Prioritize Adopted Libraries:** If the standard library is insufficient, use one of the adopted community libraries listed above.
3. **Avoid Adding New Dependencies:** Do not add new external library dependencies to `go.mod` without aligning with the team and ensuring it matches clean architecture guidelines.

# Design Spec: Observability Metrics Integration

Status: Approved
Date: 2026-06-18

## Background & Goal
To achieve deep visibility into the operations of our high-frequency trading bot, we will implement full-stack metrics monitoring using standard OpenTelemetry Go APIs, exporting them to Prometheus. 

We will automatically instrument four key layers:
1. **Incoming HTTP Request Layer**: A custom Gin middleware capturing server request count, latency (histogram), and active concurrency.
2. **Go Runtime Layer**: Official OTel runtime metrics library for tracking RAM, Heap, CPU, Goroutines count, and GC pauses.
3. **Database Layer**: GORM query execution durations and errors via `otelgorm` plugin.
4. **Outgoing API Layer**: Outgoing REST requests to exchange API endpoints (MEXC, Bybit, Binance, Kucoin, Okx, Gate, etc.) via `otelhttp` round-tripper instrumentation.

---

## Detailed Architecture

```
                                  [ Scraper (Prometheus) ]
                                              │
                                              ▼ (Scrapes /metrics)
 ┌──────────────────────────────────────────────────────────────────────────────────────┐
 │ APIServer (Gin Router)                                                               │
 │                                                                                      │
 │  ┌───────────────────────┐       ┌────────────────────────┐       ┌────────────────┐  │
 │  │ GinMetricsMiddleware  ├──────►│   OTel MeterProvider   │◄──────┤ OTel Runtime   │  │
 │  └───────────────────────┘       │  (Prometheus Exporter) │       └────────────────┘  │
 └─────────────┬────────────────────└───────────▲────▲───────┴──────────────────────────┘
               │                                │    │
               ▼                                │    └─────────────────────────────┐
 ┌─────────────┴────────────┐                   │                                  │
 │   GORM Database (Postgres)                   │                                  │
 │   - Instrumented via otelgorm ───────────────┘                                  │
 └──────────────────────────┘                                                      │
                                                                                   │
 ┌──────────────────────────┐                                                      │
 │   Exchange Clients (HTTP)                                                       │
 │   - Instrumented via otelhttp client round-tripper ─────────────────────────────┘
 └──────────────────────────┘
```

---

## Detailed Design

### 1. OpenTelemetry Initialization & Exporter
We will create `internal/infrastructure/observability/otel_metrics.go` to configure OpenTelemetry metrics:
- Register the OpenTelemetry Prometheus exporter reader.
- Build a global `MeterProvider` mapping all OpenTelemetry metrics to the Prometheus registry.
- Provide a `PrometheusHandler` function returning the `http.Handler` for scraping.
- Wire this into Fx dependency injection with startup lifecycle hooks that register the OTel Go runtime instrumentation (`go.opentelemetry.io/contrib/instrumentation/runtime`).

### 2. Gin Incoming Request Middleware
We will implement a clean, custom Gin middleware inside `internal/infrastructure/observability/middleware.go` to measure HTTP traffic:
- **Metrics Recorded**:
  - `http.server.request_duration` (Histogram, seconds): Latency of request execution.
  - `http.server.requests_total` (Counter, count): Total cumulative request count.
  - `http.server.active_requests` (UpDownCounter, count): Concurrency indicator (active requests).
- **Labels/Attributes**:
  - `http.method` (GET, POST, etc.)
  - `http.route` (Path template, e.g. `/debug/:exchange/order_pnl` rather than resource-specific URLs)
  - `http.status_code` (200, 400, 502, etc.)

### 3. Outgoing REST API Calls (`otelhttp`)
- Wrap the HTTP client pool in `internal/bots/funding/bootstrap/module.go` (specifically `provideHTTPClient`) using `otelhttp.NewTransport`.
- This automatically records outbound API latencies and sizes to exchange endpoints.

### 4. Database query instrumentation (`otelgorm`)
- Add the `otelgorm` plugin inside database initialization (`internal/bots/funding/infrastructure/persistence/db.go` or GORM setup).
- This registers metric hooks into database operations.

### 5. Server Scraping Endpoint
- Expose `GET /metrics` on the Gin router (registered in `internal/infrastructure/server/server.go`) using `gin.WrapH(observability.PrometheusHandler())`.

---

## Verification Plan
1. **Linter & Build Validation**: Run `make lint` and ensure no formatting or import boundaries checks fail.
2. **Unit Tests**: Implement test coverage for the Gin metrics middleware, database plugin configuration, and Prometheus handler inside the `observability` and `server` packages.
3. **Local Testing**: Fire request traffic to the API server, fetch `http://localhost:8080/metrics` and check for:
  - `http_server_request_duration_seconds_bucket`
  - `go_goroutines`
  - `db_query_duration_seconds` (if queries run)
  - `otelhttp_client_roundtrip_latency` (if API requests execute)

# Design Spec: Split Exchange Config

Status: Approved
Date: 2026-06-18

## Background & Goal
Currently, the `"exchange"` block containing connection details, endpoints, and credentials structure is embedded inside the main `system.jsonc` file. To improve modularity and support flexible configurations, we will split this block into a standalone `exchange.jsonc` file.

We will update:
1. The local and production config files.
2. The Go bot configuration loader to load and merge both files.
3. The internal tools configuration utility to load the split files.
4. Kubernetes deployment configurations (Terraform & YAML).
5. Docker Compose and local running scripts (Makefile).

---

## Detailed Design

### 1. Configuration Files

#### `configs/funding/local/exchange.jsonc` [NEW]
Contains the `"exchange"` block extracted from `configs/funding/local/system.jsonc`.
```jsonc
{
  "exchange": {
    "mexc": {
      "enable": true,
      "future": {
        "baseURL": "https://api.mexc.com"
      },
      "websocket": {
        "wsURL": "wss://contract.mexc.com/edge"
      }
    },
    "bybit": {
      "enable": true,
      "future": {
        "baseURL": "https://api.bybit.com"
      },
      "websocket": {
        "publicURL": "wss://stream.bybit.com/v5/public/linear",
        "privateURL": "wss://stream.bybit.com/v5/private",
        "maxPairsPerWSConn": 30
      },
      "accountType": "unified"
    },
    "binance": {
      "enable": true,
      "future": {
        "baseURL": "https://fapi.binance.com"
      },
      "websocket": {
        "wsURL": "wss://fstream.binance.com/ws",
        "publicURL": "wss://fstream.binance.com/public/ws",
        "marketURL": "wss://fstream.binance.com/market/ws",
        "privateURL": "wss://fstream.binance.com/private/ws"
      }
    },
    "kucoin": {
      "enable": true,
      "future": {
        "baseURL": "https://api-futures.kucoin.com"
      },
      "websocket": {
        "publicURL": "wss://ws-api-futures.kucoin.com",
        "privateURL": "wss://ws-api-futures.kucoin.com"
      }
    },
    "okx": {
      "enable": true,
      "future": {
        "baseURL": "https://openapi.okx.com"
      },
      "websocket": {
        "privateURL": "wss://ws.okx.com:8443/ws/v5/private",
        "publicURL": "wss://ws.okx.com:8443/ws/v5/public"
      }
    },
    "gate": {
      "enable": true,
      "future": {
        "baseURL": "https://api.gateio.ws/api/v4"
      },
      "websocket": {
        "wsURL": "wss://fx-ws.gateio.ws/v4/ws/usdt"
      }
    },
    "hyperliquid": {
      "enable": false,
      "future": {
        "baseURL": "https://api.hyperliquid.xyz"
      },
      "websocket": {
        "wsURL": "wss://api.hyperliquid.xyz/ws"
      }
    },
    "bitget": {
      "enable": false,
      "future": {
        "baseURL": "https://api.bitget.com"
      },
      "websocket": {
        "publicURL": "wss://ws.bitget.com/v2/ws/public",
        "privateURL": "wss://ws.bitget.com/v2/ws/private"
      }
    },
    "bingx": {
      "enable": false,
      "future": {
        "baseURL": "https://open-api.bingx.com"
      },
      "websocket": {
        "wsURL": "wss://open-api-ws.bingx.com/market"
      }
    }
  }
}
```

#### `configs/funding/prod/exchange.jsonc` [NEW]
Contains the production-equivalent `"exchange"` block.

#### `configs/funding/local/system.jsonc` & `configs/funding/prod/system.jsonc` [MODIFY]
Remove the `"exchange"` block (lines 6-97).

---

### 2. Configuration Loader & CLI Flags

#### `cmd/funding/main.go` [MODIFY]
Add `-exch` flag:
```go
exchCfgPath := flag.String("exch", "", "path to exchange config (defaults to exchange.jsonc in system config dir)")
```
Pass to `bootstrap.ConfigPaths`.

#### `internal/bots/funding/bootstrap/module.go` [MODIFY]
Add `Exchange` field to `ConfigPaths` struct, and pass it to `LoadSystemConfig`.

#### `internal/bots/funding/config/system.go` [MODIFY]
Update `LoadSystemConfig(systemPath string, exchangePath ...string)` to:
1. Parse the main system configuration.
2. If `exchangePath` is provided and non-empty, use that; otherwise default to `exchange.jsonc` in the same directory as `systemPath`.
3. Load the exchange configuration and merge its `ExchangeConfig` field into `SystemConfig`.
4. Apply defaults/validation.

#### `tools/toolconfig/config.go` [MODIFY]
Update the tool config load function to automatically resolve `exchange.jsonc` in the same directory as the passed system configuration and merge it.

---

### 3. Deployments & Local Run Scripts

#### `Makefile` [MODIFY]
- Define `FUNDING_EXCH := ./configs/funding/local/exchange.jsonc`.
- Update `run/funding` target to pass `-exch $(FUNDING_EXCH)`.
- Update `apply-configs` target to include `--from-file=configs/funding/prod/exchange.jsonc`. (Note: The existing target used `configs/funding/system.jsonc`. Since the directory contains `prod/` and `local/` subdirectories, we will ensure it correctly hot-reloads the production configs.)

#### `docker-compose.yml` [MODIFY]
Update the bot container `command` arguments to include `-exch /app/configs/funding/local/exchange.jsonc`.

#### `deploy/terraform/app.tf` [MODIFY]
- Add `"exchange.jsonc" = file("${path.module}/../../configs/funding/prod/exchange.jsonc")` to `kubernetes_config_map.crypto_bot_configs.data`.
- Add `"-exch", "/app/configs/funding/prod/exchange.jsonc"` to `kubernetes_deployment.crypto_bot` container args.

#### `deploy/k8s/deployment.yaml` [MODIFY]
Add `"-exch", "/app/configs/funding/prod/exchange.jsonc"` to the container arguments.

---

## Verification Plan
1. Run `make lint` and verify it compiles cleanly.
2. Run unit tests `make test` and verify that config tests pass.
3. Test locally using `make run/funding` (or go run directly) and ensure the bot boots successfully.

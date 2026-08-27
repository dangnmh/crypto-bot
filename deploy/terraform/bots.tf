# ==============================================================================
# Dynamic Multi-Bot Orchestration (Scale N Bots via Terraform for_each)
# ==============================================================================

locals {
  enabled_bots = { for k, v in var.bots : k => v if lookup(v, "enabled", true) }
}

# 1. Dynamic ConfigMap for each bot instance
resource "kubernetes_config_map_v1" "bot_configs" {
  for_each = local.enabled_bots

  metadata {
    name      = "${each.key}-configs"
    namespace = "default"
    labels = {
      app      = each.key
      bot_type = each.value.bot_type
    }
  }

  data = {
    for f in fileset(
      startswith(each.value.config_dir, "/") ? each.value.config_dir : "${path.module}/${each.value.config_dir}",
      "*.jsonc"
    ) : f => file("${startswith(each.value.config_dir, "/") ? each.value.config_dir : "${path.module}/${each.value.config_dir}"}/${f}")
  }
}

# 2. Dynamic Deployment for each bot instance
resource "kubernetes_deployment_v1" "bot" {
  for_each = local.enabled_bots

  metadata {
    name      = each.key
    namespace = "default"
    labels = {
      app      = each.key
      bot_type = each.value.bot_type
    }
  }

  spec {
    replicas = 1
    strategy {
      type = "Recreate" # Enforces strictly single running instance per trading bot for safety
    }

    selector {
      match_labels = {
        app = each.key
      }
    }

    template {
      metadata {
        labels = {
          app      = each.key
          bot_type = each.value.bot_type
        }
        annotations = {
          "checksum/config"      = sha256(jsonencode(kubernetes_config_map_v1.bot_configs[each.key].data))
          "prometheus.io/scrape" = "true"
          "prometheus.io/path"   = "/metrics"
          "prometheus.io/port"   = tostring(coalesce(each.value.metrics_port, 3100))
        }
      }

      spec {
        service_account_name = kubernetes_service_account_v1.crypto_bot.metadata[0].name

        dynamic "image_pull_secrets" {
          for_each = var.registry_auth_enabled ? [1] : []
          content {
            name = kubernetes_secret_v1.registry_pull_secret[0].metadata[0].name
          }
        }

        security_context {
          fs_group        = 10001
          run_as_non_root = true
          run_as_user     = 10001
          run_as_group    = 10001
        }

        container {
          name              = "bot"
          image             = var.docker_image
          image_pull_policy = var.docker_image == "crypto-bot:latest" ? "Never" : "Always"

          command = [
            coalesce(
              each.value.binary_path,
              each.value.bot_type == "funding" ? "/usr/local/bin/funding-bot" : "/usr/local/bin/penny-jumper-bot"
            )
          ]

          args = length(coalesce(each.value.command_args, [])) > 0 ? each.value.command_args : (
            each.value.bot_type == "funding" ? [
              "-sys", "/app/configs/system.jsonc",
              "-exch", "/app/configs/exchange.jsonc",
              "-bot", "/app/configs/funding.jsonc",
              "-blacklist", "/app/configs/blacklist.jsonc",
              "-reversion", "/app/configs/reversion.jsonc",
              "-obfuscator", "/app/configs/obfuscator.jsonc",
              "-dilution", "/app/configs/dilution.jsonc"
            ] : [
              "-sys", "/app/configs/system.jsonc",
              "-exch", "/app/configs/exchange.jsonc",
              "-bot", "/app/configs/penny_jumper.jsonc",
              "-blacklist", "/app/configs/blacklist.jsonc"
            ]
          )

          dynamic "env" {
            for_each = coalesce(each.value.env_vars, {})
            content {
              name  = env.key
              value = env.value
            }
          }

          env_from {
            secret_ref {
              name     = "crypto-bot-vault-secrets"
              optional = true
            }
          }

          resources {
            limits = {
              cpu    = coalesce(each.value.cpu_limit, "4000m")
              memory = coalesce(each.value.memory_limit, "2Gi")
            }
            requests = {
              cpu    = coalesce(each.value.cpu_request, "2000m")
              memory = coalesce(each.value.memory_request, "1Gi")
            }
          }

          security_context {
            read_only_root_filesystem  = true
            allow_privilege_escalation = false
            capabilities {
              drop = ["ALL"]
            }
          }

          volume_mount {
            name       = "configs"
            mount_path = "/app/configs"
            read_only  = true
          }
        }

        volume {
          name = "configs"
          config_map {
            name = kubernetes_config_map_v1.bot_configs[each.key].metadata[0].name
          }
        }
      }
    }
  }
}

# 3. Dynamic ClusterIP Service for metrics & health monitoring
resource "kubernetes_service_v1" "bot" {
  for_each = local.enabled_bots

  metadata {
    name      = each.key
    namespace = "default"
    labels = {
      app      = each.key
      bot_type = each.value.bot_type
    }
  }

  spec {
    selector = {
      app = each.key
    }

    port {
      name        = "metrics"
      port        = coalesce(each.value.metrics_port, 3100)
      target_port = coalesce(each.value.metrics_port, 3100)
    }

    type = "ClusterIP"
  }
}

# ==============================================================================
# Application Infrastructure (Crypto-Bot)
# ==============================================================================

# Deploy Kubernetes ConfigMap for Bot Configurations
resource "kubernetes_config_map_v1" "crypto_bot_configs" {
  metadata {
    name      = "crypto-bot-configs"
    namespace = "default"
    labels = {
      app = "crypto-bot"
    }
  }

  data = {
    "system.jsonc"    = file("${path.module}/../../configs/funding/prod/system.jsonc")
    "exchange.jsonc"  = file("${path.module}/../../configs/funding/prod/exchange.jsonc")
    "funding.jsonc"   = file("${path.module}/../../configs/funding/prod/funding.jsonc")
    "blacklist.jsonc" = file("${path.module}/../../configs/funding/prod/blacklist.jsonc")
    "reversion.jsonc" = file("${path.module}/../../configs/funding/prod/reversion.jsonc")
  }
}

# Deploy Kubernetes Deployment for the Bot
resource "kubernetes_deployment_v1" "crypto_bot" {
  metadata {
    name      = "crypto-bot"
    namespace = "default"
    labels = {
      app = "crypto-bot"
    }
  }

  spec {
    replicas = 1
    strategy {
      type = "Recreate" # Enforces strictly single running instance for safety
    }

    selector {
      match_labels = {
        app = "crypto-bot"
      }
    }

    template {
      metadata {
        labels = {
          app = "crypto-bot"
        }
        annotations = {
          "checksum/config"      = sha256(jsonencode(kubernetes_config_map_v1.crypto_bot_configs.data))
          "prometheus.io/scrape" = "true"
          "prometheus.io/path"   = "/metrics"
          "prometheus.io/port"   = "3100"
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

          args = [
            "-sys", "/app/configs/funding/prod/system.jsonc",
            "-exch", "/app/configs/funding/prod/exchange.jsonc",
            "-bot", "/app/configs/funding/prod/funding.jsonc"
          ]

          env_from {
            secret_ref {
              name = "crypto-bot-vault-secrets"
            }
          }

          resources {
            limits = {
              cpu    = "1000m"
              memory = "512Mi"
            }
            requests = {
              cpu    = "200m"
              memory = "256Mi"
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
            mount_path = "/app/configs/funding/prod"
            read_only  = true
          }
        }

        volume {
          name = "configs"
          config_map {
            name = kubernetes_config_map_v1.crypto_bot_configs.metadata[0].name
          }
        }
      }
    }
  }
}

# Deploy Kubernetes Service for the Bot
resource "kubernetes_service_v1" "crypto_bot" {
  metadata {
    name      = "crypto-bot"
    namespace = "default"
  }
  spec {
    selector = {
      app = "crypto-bot"
    }
    port {
      port        = 3100
      target_port = 3100
    }
    type = "ClusterIP"
  }
}

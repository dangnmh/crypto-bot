terraform {
  required_version = ">= 1.0"
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.26"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
  }
}

provider "kubernetes" {
  config_path = var.kubeconfig_path
}

provider "helm" {
  kubernetes {
    config_path = var.kubeconfig_path
  }
}

# 1. Install Loki Stack (Loki + Promtail + Grafana)
resource "helm_release" "loki_stack" {
  name             = "loki-stack"
  repository       = "https://grafana.github.io/helm-charts"
  chart            = "loki-stack"
  namespace        = "default"
  create_namespace = false

  # Load the values file we created for local storage and configuration overrides
  values = [
    file("${path.module}/../k8s/loki-values.yaml")
  ]
}

# 2. Deploy Kubernetes Secret for Bitwarden Credentials
resource "kubernetes_secret" "crypto_bot_secrets" {
  metadata {
    name      = "crypto-bot-secrets"
    namespace = "default"
    labels = {
      app = "crypto-bot"
    }
  }

  type = "Opaque"

  data = {
    BITWARDEN_ACCESS_TOKEN    = var.bitwarden_access_token
    BITWARDEN_ORGANIZATION_ID = var.bitwarden_organization_id
    BITWARDEN_PROJECT_NAME    = var.bitwarden_project_name
  }
}

# 3. Deploy Kubernetes ConfigMap for Bot Configurations
resource "kubernetes_config_map" "crypto_bot_configs" {
  metadata {
    name      = "crypto-bot-configs"
    namespace = "default"
    labels = {
      app = "crypto-bot"
    }
  }

  data = {
    "system.jsonc"    = file("${path.module}/../../configs/funding/prod/system.jsonc")
    "funding.jsonc"   = file("${path.module}/../../configs/funding/prod/funding.jsonc")
    "blacklist.jsonc" = file("${path.module}/../../configs/funding/prod/blacklist.jsonc")
    "reversion.jsonc" = file("${path.module}/../../configs/funding/prod/reversion.jsonc")
  }
}

# 4. Deploy Kubernetes Deployment for the Bot
resource "kubernetes_deployment" "crypto_bot" {
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
          "checksum/config" = sha256(jsonencode(kubernetes_config_map.crypto_bot_configs.data))
        }
      }

      spec {
        dynamic "image_pull_secrets" {
          for_each = var.registry_auth_enabled ? [1] : []
          content {
            name = kubernetes_secret.registry_pull_secret[0].metadata[0].name
          }
        }

        security_context {
          fs_group      = 10001
          run_as_non_root = true
          run_as_user   = 10001
          run_as_group  = 10001
        }

        container {
          name  = "bot"
          image = var.docker_image
          image_pull_policy = var.docker_image == "crypto-bot:latest" ? "Never" : (var.registry_auth_enabled ? "Always" : "IfNotPresent")

          args = [
            "-sys", "/app/configs/funding/prod/system.jsonc",
            "-bot", "/app/configs/funding/prod/funding.jsonc"
          ]

          env {
            name = "BITWARDEN_ACCESS_TOKEN"
            value_from {
              secret_key_ref {
                name = kubernetes_secret.crypto_bot_secrets.metadata[0].name
                key  = "BITWARDEN_ACCESS_TOKEN"
              }
            }
          }

          env {
            name = "BITWARDEN_ORGANIZATION_ID"
            value_from {
              secret_key_ref {
                name = kubernetes_secret.crypto_bot_secrets.metadata[0].name
                key  = "BITWARDEN_ORGANIZATION_ID"
              }
            }
          }

          env {
            name = "BITWARDEN_PROJECT_NAME"
            value_from {
              secret_key_ref {
                name = kubernetes_secret.crypto_bot_secrets.metadata[0].name
                key  = "BITWARDEN_PROJECT_NAME"
              }
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
            read_only_root_filesystem = true
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
            name = kubernetes_config_map.crypto_bot_configs.metadata[0].name
          }
        }
      }
    }
  }
}

# 5. Deploy Kubernetes ConfigMap for Grafana Loki Datasource
resource "kubernetes_config_map" "grafana_datasource_loki" {
  metadata {
    name      = "grafana-datasource-loki"
    namespace = "default"
    labels = {
      grafana_datasource = "1"
    }
  }

  data = {
    "loki.yaml" = file("${path.module}/../grafana/provisioning/datasources/loki.yaml")
  }
}

# 6. Deploy Kubernetes ConfigMap for Grafana P&L Dashboard
resource "kubernetes_config_map" "grafana_dashboard_pnl" {
  metadata {
    name      = "grafana-dashboard-pnl"
    namespace = "default"
    labels = {
      grafana_dashboard = "1"
    }
  }

  data = {
    "pnl-analytics.json" = file("${path.module}/../grafana/dashboards/pnl-analytics.json")
  }
}

# 7. Deploy Kubernetes Secret for Docker Registry Pull Credentials
resource "kubernetes_secret" "registry_pull_secret" {
  count = var.registry_auth_enabled ? 1 : 0

  metadata {
    name      = "registry-pull-secret"
    namespace = "default"
  }

  type = "kubernetes.io/dockerconfigjson"

  data = {
    ".dockerconfigjson" = jsonencode({
      auths = {
        "${var.registry_server}" = {
          username = var.registry_username
          password = var.registry_password
          auth     = base64encode("${var.registry_username}:${var.registry_password}")
        }
      }
    })
  }
}

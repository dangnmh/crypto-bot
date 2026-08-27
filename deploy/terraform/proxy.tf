# ==============================================================================
# Dedicated AI Proxy Service (cli-proxy-api)
# ==============================================================================

# Generate random WebUI management secret if not explicitly provided
resource "random_password" "proxy_management_secret" {
  count   = var.enable_ai_proxy && var.proxy_management_secret == "" ? 1 : 0
  length  = 32
  special = false
}

# Generate random client API key (Bearer token) if not explicitly provided
resource "random_password" "proxy_api_key" {
  count   = var.enable_ai_proxy && var.proxy_api_key == "" ? 1 : 0
  length  = 32
  special = false
}

locals {
  proxy_mgmt_secret = var.proxy_management_secret != "" ? var.proxy_management_secret : (length(random_password.proxy_management_secret) > 0 ? random_password.proxy_management_secret[0].result : "")
  proxy_client_key  = var.proxy_api_key != "" ? var.proxy_api_key : (length(random_password.proxy_api_key) > 0 ? random_password.proxy_api_key[0].result : "")
}

# Dedicated Kubernetes Secret storing the generated proxy credentials for reference
resource "kubernetes_secret_v1" "ai_proxy_secrets" {
  count = var.enable_ai_proxy ? 1 : 0

  metadata {
    name      = "ai-proxy-secrets"
    namespace = "default"
    labels = {
      app = "ai-proxy"
    }
  }

  type = "Opaque"

  data = {
    "AI_PROXY_MANAGEMENT_SECRET" = local.proxy_mgmt_secret
    "AI_PROXY_API_KEY"           = local.proxy_client_key
  }
}

# ConfigMap for AI Proxy configuration (rendered from template or static file)
resource "kubernetes_config_map_v1" "ai_proxy_config" {
  count = var.enable_ai_proxy ? 1 : 0

  metadata {
    name      = "ai-proxy-config"
    namespace = "default"
    labels = {
      app = "ai-proxy"
    }
  }

  data = {
    "config.yaml" = var.config_path_proxy != "" ? file(startswith(var.config_path_proxy, "/") ? var.config_path_proxy : "${path.module}/${var.config_path_proxy}") : templatefile("${path.module}/templates/config.proxy.yaml.tftpl", {
      secret_key            = local.proxy_mgmt_secret
      api_key               = local.proxy_client_key
      debug                 = var.proxy_debug
      disable_control_panel = var.proxy_disable_control_panel
      proxy_url             = var.proxy_egress_url
    })
  }
}

# Dedicated Deployment for AI Proxy
resource "kubernetes_deployment_v1" "ai_proxy" {
  count = var.enable_ai_proxy ? 1 : 0

  metadata {
    name      = "ai-proxy"
    namespace = "default"
    labels = {
      app = "ai-proxy"
    }
  }

  spec {
    replicas = 1

    selector {
      match_labels = {
        app = "ai-proxy"
      }
    }

    template {
      metadata {
        labels = {
          app = "ai-proxy"
        }
        annotations = {
          "checksum/config" = sha256(jsonencode(kubernetes_config_map_v1.ai_proxy_config[0].data))
        }
      }

      spec {
        container {
          name  = "ai-proxy"
          image = "eceasy/cli-proxy-api:latest"

          port {
            name           = "http"
            container_port = 8317
          }

          resources {
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
            requests = {
              cpu    = "100m"
              memory = "128Mi"
            }
          }

          liveness_probe {
            tcp_socket {
              port = 8317
            }
            initial_delay_seconds = 10
            period_seconds        = 15
          }

          readiness_probe {
            tcp_socket {
              port = 8317
            }
            initial_delay_seconds = 5
            period_seconds        = 10
          }

          volume_mount {
            name       = "proxy-config"
            mount_path = "/CLIProxyAPI/config.yaml"
            sub_path   = "config.yaml"
            read_only  = true
          }

          volume_mount {
            name       = "proxy-auth"
            mount_path = "/root/.cli-proxy-api"
          }

          volume_mount {
            name       = "proxy-plugins"
            mount_path = "/CLIProxyAPI/plugins"
          }
        }

        volume {
          name = "proxy-config"
          config_map {
            name = kubernetes_config_map_v1.ai_proxy_config[0].metadata[0].name
          }
        }

        volume {
          name = "proxy-auth"
          empty_dir {}
        }

        volume {
          name = "proxy-plugins"
          empty_dir {}
        }
      }
    }
  }
}

# Dedicated Service exposing AI Proxy internally
resource "kubernetes_service_v1" "ai_proxy" {
  count = var.enable_ai_proxy ? 1 : 0

  metadata {
    name      = "ai-proxy"
    namespace = "default"
    labels = {
      app = "ai-proxy"
    }
  }

  spec {
    selector = {
      app = "ai-proxy"
    }

    port {
      name        = "http"
      port        = 8317
      target_port = 8317
    }

    type = "ClusterIP"
  }
}

# ==============================================================================
# Monitoring & Logging Infrastructure (Loki + Promtail + Grafana)
# ==============================================================================

# 1. Install Loki Stack (Loki + Promtail + Grafana)
resource "helm_release" "loki_stack" {
  name             = "loki-stack"
  repository       = "https://grafana.github.io/helm-charts"
  chart            = "loki-stack"
  version          = "2.10.3"
  namespace        = "default"
  create_namespace = false

  # Load the values file we created for local storage and configuration overrides
  values = [
    file("${path.module}/../k8s/loki-values.yaml")
  ]

  set = [
    {
      name  = "grafana.adminPassword"
      value = var.grafana_password
    }
  ]
}

# 2. Deploy Kubernetes ConfigMap for Grafana Loki Datasource
resource "kubernetes_config_map_v1" "grafana_datasource_loki" {
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

# 3. Deploy Kubernetes ConfigMap for Grafana P&L Dashboard
resource "kubernetes_config_map_v1" "grafana_dashboard_pnl" {
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

# 4. Deploy Kubernetes ConfigMap for Grafana PostgreSQL Datasource
resource "kubernetes_config_map_v1" "grafana_datasource_postgres" {
  metadata {
    name      = "grafana-datasource-postgres"
    namespace = "default"
    labels = {
      grafana_datasource = "1"
    }
  }

  data = {
    "postgres.yaml" = templatefile("${path.module}/postgres.yaml.tpl", {
      postgres_password = var.postgres_password
    })
  }
}

# 5. Install Prometheus Server
resource "helm_release" "prometheus" {
  name             = "prometheus"
  repository       = "https://prometheus-community.github.io/helm-charts"
  chart            = "prometheus"
  version          = "29.13.0"
  namespace        = "default"
  create_namespace = false

  values = [
    file("${path.module}/../k8s/prometheus-values.yaml")
  ]
}

# 6. Deploy Kubernetes ConfigMap for Grafana Prometheus Datasource
resource "kubernetes_config_map_v1" "grafana_datasource_prometheus" {
  metadata {
    name      = "grafana-datasource-prometheus"
    namespace = "default"
    labels = {
      grafana_datasource = "1"
    }
  }

  data = {
    "prometheus.yaml" = file("${path.module}/../grafana/provisioning/datasources/prometheus.yaml")
  }
}

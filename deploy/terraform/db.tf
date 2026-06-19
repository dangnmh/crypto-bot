# ==============================================================================
# Database Infrastructure (PostgreSQL)
# ==============================================================================

# Install PostgreSQL (using Bitnami chart)
resource "helm_release" "postgresql" {
  name             = "postgresql"
  chart            = "oci://registry-1.docker.io/bitnamicharts/postgresql"
  version          = "18.7.6"
  namespace        = "default"
  create_namespace = false

  set = [
    {
      name  = "fullnameOverride"
      value = "postgresql"
    },
    {
      name  = "auth.database"
      value = "postgres"
    },
    {
      name  = "auth.postgresPassword"
      value = var.postgres_password
    },
    {
      name  = "primary.persistence.enabled"
      value = "true"
    },
    {
      name  = "primary.persistence.size"
      value = "8Gi"
    }
  ]
}

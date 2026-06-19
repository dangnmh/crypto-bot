# ==============================================================================
# Secret Management Infrastructure (HashiCorp Vault)
# ==============================================================================

# Deploy ServiceAccount for the bot to authenticate against Vault
resource "kubernetes_service_account" "crypto_bot" {
  metadata {
    name      = "crypto-bot"
    namespace = "default"
    labels = {
      app = "crypto-bot"
    }
  }
}

# Deploy HashiCorp Vault via Helm
resource "helm_release" "vault" {
  name             = "vault"
  repository       = "https://helm.releases.hashicorp.com"
  chart            = "vault"
  version          = "0.33.0"
  namespace        = "default"
  create_namespace = false

  values = [
    templatefile("${path.module}/vault-values.yaml.tpl", {
      vault_password = var.vault_password
    })
  ]
}

# Deploy HashiCorp Vault Secrets Operator via Helm
resource "helm_release" "vault_secrets_operator" {
  name             = "vault-secrets-operator"
  repository       = "https://helm.releases.hashicorp.com"
  chart            = "vault-secrets-operator"
  version          = "1.4.0"
  namespace        = "default"
  create_namespace = false
  depends_on       = [helm_release.vault]

  set {
    name  = "defaultVaultConnection.enabled"
    value = "true"
  }

  set {
    name  = "defaultVaultConnection.address"
    value = "http://vault.default.svc.cluster.local:8200"
  }
}


variable "kubeconfig_path" {
  type        = string
  description = "Path to the kubeconfig file"
  default     = "~/.kube/config"
}

# ==============================================================================
# Registry Variables for deployment image migration
# ==============================================================================

variable "docker_image" {
  type        = string
  description = "The container image to deploy (e.g., ghcr.io/username/crypto-bot:latest)"
  default     = "crypto-bot:latest"
}

variable "registry_auth_enabled" {
  type        = bool
  description = "Set to true if pulling from a private registry requiring authentication credentials"
  default     = false
}

variable "registry_username" {
  type        = string
  description = "Username for the container registry"
  default     = ""
}

variable "registry_password" {
  type        = string
  description = "Password/token for the container registry"
  default     = ""
  sensitive   = true
}

variable "registry_server" {
  type        = string
  description = "Registry server URL (e.g. https://index.docker.io/v1/ for Docker Hub, ghcr.io for GitHub Container Registry)"
  default     = "https://index.docker.io/v1/"
}

variable "postgres_password" {
  type        = string
  description = "The password for the PostgreSQL postgres superuser"
  default     = "postgres"
  sensitive   = true
}

variable "grafana_password" {
  type        = string
  description = "The admin password for Grafana"
  default     = "admin"
  sensitive   = true
}

variable "vault_password" {
  type        = string
  description = "The root token for HashiCorp Vault in dev mode"
  default     = "root"
  sensitive   = true
}

# ==============================================================================
# Configuration File Paths
# ==============================================================================

variable "config_path_system" {
  type        = string
  description = "Path to the system.jsonc configuration file"
  default     = "../../configs/funding/prod/system.jsonc"
}

variable "config_path_exchange" {
  type        = string
  description = "Path to the exchange.jsonc configuration file"
  default     = "../../configs/funding/prod/exchange.jsonc"
}

variable "config_path_funding" {
  type        = string
  description = "Path to the funding.jsonc configuration file"
  default     = "../../configs/funding/prod/funding.jsonc"
}

variable "config_path_blacklist" {
  type        = string
  description = "Path to the blacklist.jsonc configuration file"
  default     = "../../configs/funding/prod/blacklist.jsonc"
}

variable "config_path_reversion" {
  type        = string
  description = "Path to the reversion.jsonc configuration file"
  default     = "../../configs/funding/prod/reversion.jsonc"
}

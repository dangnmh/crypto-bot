
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
# Dynamic Multi-Bot Scaling Variables
# ==============================================================================

variable "bots" {
  type = map(object({
    enabled        = optional(bool, true)
    bot_type       = string                       # "penny_jumper", "funding", or custom
    binary_path    = optional(string, null)       # Default: /usr/local/bin/{bot_type}-bot
    config_dir     = string                       # Path to directory containing the bot's .jsonc files
    command_args   = optional(list(string), null) # Custom CLI arguments override
    env_vars       = optional(map(string), {})    # Extra environment variables
    cpu_limit      = optional(string, "4000m")
    memory_limit   = optional(string, "2Gi")
    cpu_request    = optional(string, "2000m")
    memory_request = optional(string, "1Gi")
    metrics_port   = optional(number, 3100)
  }))
  description = "Dynamic catalog of trading bot instances to deploy"
  default = {
    penny-jumper = {
      enabled    = true
      bot_type   = "penny_jumper"
      config_dir = "../../configs/penny_jumper/prod"
      env_vars   = { "AI_PROXY_URL" = "http://ai-proxy:8317" }
    }
  }
}

# ==============================================================================
# AI Proxy Configuration
# ==============================================================================

variable "enable_ai_proxy" {
  type        = bool
  description = "Set to true to deploy the dedicated AI Proxy service (cli-proxy-api)"
  default     = true
}

variable "config_path_proxy" {
  type        = string
  description = "Optional static path to config.proxy.yaml. If empty or default template exists, Terraform renders from template."
  default     = ""
}

variable "proxy_management_secret" {
  type        = string
  description = "Optional override for AI proxy WebUI management secret. If empty, Terraform generates a random 32-char secret."
  default     = ""
  sensitive   = true
}

variable "proxy_api_key" {
  type        = string
  description = "Optional override for AI proxy client API key (Bearer token). If empty, Terraform generates a random 32-char secret."
  default     = ""
  sensitive   = true
}

variable "proxy_debug" {
  type        = bool
  description = "Set to true to enable debug logs in AI proxy"
  default     = false
}

variable "proxy_disable_control_panel" {
  type        = bool
  description = "Set to true to disable WebUI control panel in production"
  default     = false
}

variable "proxy_egress_url" {
  type        = string
  description = "Optional outbound proxy URL (e.g. socks5:// or http://) for AI Proxy"
  default     = ""
}


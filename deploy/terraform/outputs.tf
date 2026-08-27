# ==============================================================================
# Terraform Outputs
# ==============================================================================

output "ai_proxy_management_secret" {
  description = "Secret key for AI Proxy Web Control Panel"
  value       = var.enable_ai_proxy ? local.proxy_mgmt_secret : null
  sensitive   = true
}

output "ai_proxy_api_key" {
  description = "Client API Key (Bearer token) for AI Proxy access"
  value       = var.enable_ai_proxy ? local.proxy_client_key : null
  sensitive   = true
}

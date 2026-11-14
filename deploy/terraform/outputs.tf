output "resource_group_name" {
  description = "Resource group that holds Healbot."
  value       = azurerm_resource_group.bot.name
}

output "hosting" {
  description = "Host selected for this apply: app_service or container_apps."
  value       = var.hosting
}

output "hostname" {
  description = "Public hostname of the Healbot process, without a scheme."
  value       = local.hostname
}

output "health_url" {
  description = "Unauthenticated liveness URL."
  value       = "https://${local.hostname}/healthz"
}

output "principal_id" {
  description = "Object id of the user-assigned managed identity."
  value       = azurerm_user_assigned_identity.bot.principal_id
}

output "identity_client_id" {
  description = "Client id of the user-assigned managed identity. The process reads it as AZURE_CLIENT_ID."
  value       = azurerm_user_assigned_identity.bot.client_id
}

output "action_group_id" {
  description = "Action group that posts the common alert schema to Healbot. Attach existing alerts to this id."
  value       = azurerm_monitor_action_group.bot.id
}

output "role_definition_ids" {
  description = "Custom Healbot Operator role definitions, one per operator scope."
  value       = { for scope, role in azurerm_role_definition.operator : scope => role.role_definition_resource_id }
}

output "key_vault_name" {
  description = "Key Vault that holds the generated webhook and admin tokens."
  value       = azurerm_key_vault.bot.name
}

output "key_vault_uri" {
  description = "Key Vault URI."
  value       = azurerm_key_vault.bot.vault_uri
}

output "workspace_id" {
  description = "Log Analytics workspace customer id (a GUID), which Healbot reads as HEALBOT_WORKSPACE_ID."
  value       = azurerm_log_analytics_workspace.bot.workspace_id
}

output "registry_login_server" {
  description = "Login server of the registry created by this stack. Empty when create_container_registry is false."
  value       = local.acr_login
}

output "webhook_token" {
  description = "Generated ingest token. Also stored in Key Vault as webhook-token. The action group URL contains this value because action groups cannot set a header."
  value       = random_password.webhook.result
  sensitive   = true
}

output "admin_token" {
  description = "Generated admin token for kill, resume, and suppress. Also stored in Key Vault as admin-token."
  value       = random_password.admin.result
  sensitive   = true
}

output "name" {
  description = "Name prefix including the random suffix."
  value       = local.name
}

output "web_name" {
  description = "Globally unique name for the App Service site or Container App."
  value       = local.web_name
}

output "resource_group_name" {
  description = "Resource group that holds Healbot."
  value       = azurerm_resource_group.bot.name
}

output "resource_group_id" {
  description = "Resource ID of the Healbot resource group."
  value       = azurerm_resource_group.bot.id
}

output "location" {
  description = "Azure region of the resource group."
  value       = azurerm_resource_group.bot.location
}

output "tags" {
  description = "Tags applied to resources this module creates."
  value       = local.tags
}

output "identity_id" {
  description = "Resource ID of the user-assigned managed identity."
  value       = azurerm_user_assigned_identity.bot.id
}

output "principal_id" {
  description = "Object id of the user-assigned managed identity."
  value       = azurerm_user_assigned_identity.bot.principal_id
}

output "identity_client_id" {
  description = "Client id of the user-assigned managed identity. The process reads it as AZURE_CLIENT_ID."
  value       = azurerm_user_assigned_identity.bot.client_id
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

output "log_analytics_workspace_id" {
  description = "Resource ID of the Log Analytics workspace."
  value       = azurerm_log_analytics_workspace.bot.id
}

output "role_definition_ids" {
  description = "Custom Healbot Operator role definitions, one per operator scope."
  value       = { for scope, role in azurerm_role_definition.operator : scope => role.role_definition_resource_id }
}

output "registry_login_server" {
  description = "Login server of the registry created by this module. Empty when create_container_registry is false."
  value       = local.acr_login
}

output "image_name" {
  description = "Repository and tag passed to App Service, without the registry host."
  value       = local.image_name
}

output "image_host" {
  description = "Registry host, without a scheme."
  value       = local.image_host
}

output "full_image" {
  description = "Full image reference for Container Apps."
  value       = local.full_image
}

output "use_managed_pull" {
  description = "True when the host should pull the image with the managed identity."
  value       = local.use_managed_pull
}

output "plain_env" {
  description = "Non-secret environment variables shared by both hosts."
  value       = local.plain_env
}

output "app_settings" {
  description = "App Service settings, including Key Vault references for tokens."
  value       = merge(local.plain_env, local.secret_env)
}

output "webhook_secret_id" {
  description = "Versionless Key Vault URI of the generated webhook token."
  value       = azurerm_key_vault_secret.webhook.versionless_id
}

output "admin_secret_id" {
  description = "Versionless Key Vault URI of the generated admin token."
  value       = azurerm_key_vault_secret.admin.versionless_id
}

output "notifier_secrets" {
  description = "Optional notifier secrets keyed by environment variable name. The map keys are known at plan time."
  value = {
    for key, secret in azurerm_key_vault_secret.notifier : key => {
      name                = secret.name
      key_vault_secret_id = secret.versionless_id
    }
  }
}

output "container_secret_env" {
  description = "Map of environment variable name to Container App secret name."
  value = merge(
    {
      HEALBOT_WEBHOOK_TOKEN = "webhook-token"
      HEALBOT_ADMIN_TOKEN   = "admin-token"
    },
    { for key, secret in azurerm_key_vault_secret.notifier : key => secret.name },
  )
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

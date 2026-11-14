output "hosting" {
  description = "Host this root deploys. Always container_apps."
  value       = "container_apps"
}

output "resource_group_name" {
  description = "Resource group that holds Healbot."
  value       = module.core.resource_group_name
}

output "hostname" {
  description = "Public hostname of the Healbot process, without a scheme."
  value       = module.host.hostname
}

output "health_url" {
  description = "Unauthenticated liveness URL."
  value       = "https://${module.host.hostname}/healthz"
}

output "principal_id" {
  description = "Object id of the user-assigned managed identity."
  value       = module.core.principal_id
}

output "identity_client_id" {
  description = "Client id of the user-assigned managed identity. The process reads it as AZURE_CLIENT_ID."
  value       = module.core.identity_client_id
}

output "action_group_id" {
  description = "Action group that posts the common alert schema to Healbot. Attach existing alerts to this id."
  value       = module.alerts.action_group_id
}

output "role_definition_ids" {
  description = "Custom Healbot Operator role definitions, one per operator scope."
  value       = module.core.role_definition_ids
}

output "key_vault_name" {
  description = "Key Vault that holds the generated webhook and admin tokens."
  value       = module.core.key_vault_name
}

output "key_vault_uri" {
  description = "Key Vault URI."
  value       = module.core.key_vault_uri
}

output "workspace_id" {
  description = "Log Analytics workspace customer id (a GUID), which Healbot reads as HEALBOT_WORKSPACE_ID."
  value       = module.core.workspace_id
}

output "registry_login_server" {
  description = "Login server of the registry created by this stack. Empty when create_container_registry is false."
  value       = module.core.registry_login_server
}

output "webhook_token" {
  description = "Generated ingest token. Also stored in Key Vault as webhook-token. The action group URL contains this value because action groups cannot set a header."
  value       = module.core.webhook_token
  sensitive   = true
}

output "admin_token" {
  description = "Generated admin token for kill, resume, and suppress. Also stored in Key Vault as admin-token."
  value       = module.core.admin_token
  sensitive   = true
}

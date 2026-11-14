variable "name" {
  type        = string
  description = "Container App name. Must be unique in the environment."
}

variable "environment_name" {
  type        = string
  description = "Container Apps environment name."
}

variable "resource_group_name" {
  type        = string
  description = "Resource group for the environment and the app."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Tags applied to the environment and the app."
}

variable "identity_id" {
  type        = string
  description = "Resource ID of the user-assigned managed identity."
}

variable "log_analytics_workspace_id" {
  type        = string
  description = "Resource ID of the Log Analytics workspace linked to the environment."
}

variable "image" {
  type        = string
  description = "Full image reference, including the registry host."
}

variable "image_host" {
  type        = string
  description = "Registry host, without a scheme. Used when pulling with a managed identity."
}

variable "use_managed_pull" {
  type        = bool
  description = "Pull the image with the user-assigned identity."
}

variable "plain_env" {
  type        = map(string)
  description = "Environment variables that are not secrets."
}

variable "secret_env" {
  type        = map(string)
  description = "Map of environment variable name to Container App secret name."
}

variable "webhook_secret_id" {
  type        = string
  description = "Versionless Key Vault URI of the webhook token."
}

variable "admin_secret_id" {
  type        = string
  description = "Versionless Key Vault URI of the admin token."
}

variable "notifier_secrets" {
  type = map(object({
    name                = string
    key_vault_secret_id = string
  }))
  default     = {}
  description = "Optional secrets keyed by environment variable name. Keys must be known at plan time."
}

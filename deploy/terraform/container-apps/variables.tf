variable "name" {
  type        = string
  default     = "healbot"
  description = "Short prefix for resource names. Lowercase letters, numbers, and hyphens. A random suffix is added where the name must be globally unique."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.name))
    error_message = "name must be 2-21 characters, start with a letter, and use only lowercase letters, numbers, and hyphens."
  }
}

variable "location" {
  type        = string
  default     = "eastus"
  description = "Azure region for Healbot and the resources this stack creates."
}

variable "resource_group_name" {
  type        = string
  default     = ""
  description = "Resource group name. Empty creates <name>-rg in this stack so terraform destroy removes it."
}

variable "container_image" {
  type        = string
  default     = "example.azurecr.io/healbot:0.1.0"
  description = "Image to run. Use a full reference such as example.azurecr.io/healbot:0.1.0. When create_container_registry is true, use repository:tag only, for example healbot:0.1.0."

  validation {
    condition     = length(trimspace(var.container_image)) > 0 && !strcontains(var.container_image, " ")
    error_message = "container_image must be a non-empty image reference without spaces."
  }
}

variable "create_container_registry" {
  type        = bool
  default     = false
  description = "Create a Basic Azure Container Registry and grant the Healbot identity AcrPull. Leave false to use a registry you already pushed to."
}

variable "container_registry_id" {
  type        = string
  default     = ""
  description = "Resource ID of an existing Azure Container Registry. When set, the identity is granted AcrPull and the app pulls with that identity. Ignored when create_container_registry is true."
}

variable "dry_run" {
  type        = bool
  default     = true
  description = "Process-wide dry-run ceiling. True refuses live remediation even if a resource policy opts in. Set false only together with a resource allowlist that also sets dry_run false."
}

variable "enable_plan_scale" {
  type        = bool
  default     = false
  description = "Grant Microsoft.Web/serverfarms/write on the operator scopes. Required for scale_out of an App Service plan. The permission can also change the SKU, so leave it false until a resource allowlists scale_out."
}

variable "enable_container_scale" {
  type        = bool
  default     = false
  description = "Grant Microsoft.App/containerApps/write on the operator scopes. Required to scale a Container App or to move ingress traffic during a revision rollback. Activating a revision does not need it."
}

variable "target_resource_group_ids" {
  type        = list(string)
  default     = []
  description = "Extra resource groups the identity may operate on. The group Healbot is deployed into is always included. Each group gets its own custom role definition so the role cannot be assigned at subscription scope."

  validation {
    condition = alltrue([
      for id in var.target_resource_group_ids :
      can(regex("^/subscriptions/[0-9a-fA-F-]{36}/resourceGroups/[^/]+$", id))
    ])
    error_message = "target_resource_group_ids must be resource group IDs (/subscriptions/<guid>/resourceGroups/<name>)."
  }
}

variable "log_analytics_daily_quota_gb" {
  type        = number
  default     = 1
  description = "Daily ingestion cap in GB for the Log Analytics workspace. -1 disables the cap. The default keeps a quiet sandbox small."

  validation {
    condition     = var.log_analytics_daily_quota_gb == -1 || var.log_analytics_daily_quota_gb > 0
    error_message = "log_analytics_daily_quota_gb must be -1 or a positive number of gigabytes."
  }
}

variable "log_analytics_retention_days" {
  type        = number
  default     = 30
  description = "Log Analytics retention in days. 30 is the minimum for the PerGB2018 SKU."

  validation {
    condition     = var.log_analytics_retention_days >= 30 && var.log_analytics_retention_days <= 730
    error_message = "log_analytics_retention_days must be between 30 and 730."
  }
}

variable "notifier_tokens" {
  type        = map(string)
  default     = {}
  sensitive   = true
  description = "Optional secrets injected as environment variables, for example AZDO_TOKEN. Values are stored in Key Vault. Leave the map empty in git. Pass them with TF_VAR_notifier_tokens or an untracked tfvars file."

  validation {
    condition = alltrue([
      for k, v in var.notifier_tokens :
      can(regex("^[A-Z][A-Z0-9_]{0,62}$", k)) && length(v) > 0
    ])
    error_message = "notifier_tokens keys must be uppercase environment variable names and values must be non-empty."
  }
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Extra tags applied to resources this stack creates. A component=healbot tag is always set."
}

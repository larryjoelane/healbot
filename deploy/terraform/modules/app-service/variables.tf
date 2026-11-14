variable "name" {
  type        = string
  description = "Globally unique App Service name."
}

variable "resource_group_name" {
  type        = string
  description = "Resource group for the plan and the site."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Tags applied to the plan and the site."
}

variable "sku" {
  type        = string
  description = "Linux App Service plan SKU. B1 is the cheapest tier that supports Always On and a custom container."

  validation {
    condition     = !contains(["F1", "D1", "FREE", "SHARED"], upper(var.sku)) && can(regex("^[A-Za-z][A-Za-z0-9]{1,15}$", var.sku))
    error_message = "sku must support Always On. F1 and D1 do not."
  }
}

variable "identity_id" {
  type        = string
  description = "Resource ID of the user-assigned managed identity."
}

variable "identity_client_id" {
  type        = string
  description = "Client id of that identity, used when pulling from a private registry."
}

variable "app_settings" {
  type        = map(string)
  description = "Healbot settings, including Key Vault references. This module adds the App Service port settings."
}

variable "image_name" {
  type        = string
  description = "Repository and tag, without the registry host."
}

variable "image_host" {
  type        = string
  description = "Registry host, without a scheme."
}

variable "use_managed_pull" {
  type        = bool
  description = "Pull the image with the user-assigned identity."
}

variable "log_analytics_workspace_id" {
  type        = string
  description = "Resource ID of the Log Analytics workspace that receives platform logs."
}

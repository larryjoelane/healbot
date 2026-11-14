variable "name" {
  type        = string
  description = "Name prefix used for the action group and alerts."
}

variable "resource_group_name" {
  type        = string
  description = "Resource group that holds the action group."
}

variable "resource_group_id" {
  type        = string
  description = "Resource ID of the group whose resource-health events are forwarded."
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Tags applied to the action group and alerts."
}

variable "hostname" {
  type        = string
  description = "Public hostname of Healbot, without a scheme."
}

variable "webhook_token" {
  type        = string
  sensitive   = true
  description = "Ingest token placed on the action group webhook query string. Action groups cannot set a custom header."
}

variable "alert_scope_id" {
  type        = string
  description = "Resource ID of the App Service site or Container App the metric alert watches."
}

variable "metric_namespace" {
  type        = string
  description = "Metric namespace for the symptom alert, for example Microsoft.Web/sites."
}

variable "metric_name" {
  type        = string
  description = "Metric name for the symptom alert, for example Http5xx."
}

variable "metric_threshold" {
  type        = number
  description = "Total count at which the symptom alert fires."
}

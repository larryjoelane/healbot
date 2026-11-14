module "core" {
  source = "../modules/healbot-core"

  name                         = var.name
  location                     = var.location
  resource_group_name          = var.resource_group_name
  tags                         = var.tags
  container_image              = var.container_image
  create_container_registry    = var.create_container_registry
  container_registry_id        = var.container_registry_id
  dry_run                      = var.dry_run
  enable_plan_scale            = var.enable_plan_scale
  enable_container_scale       = var.enable_container_scale
  target_resource_group_ids    = var.target_resource_group_ids
  log_analytics_daily_quota_gb = var.log_analytics_daily_quota_gb
  log_analytics_retention_days = var.log_analytics_retention_days
  notifier_tokens              = var.notifier_tokens
}

module "host" {
  source = "../modules/app-service"

  name                       = module.core.web_name
  resource_group_name        = module.core.resource_group_name
  location                   = module.core.location
  tags                       = module.core.tags
  sku                        = var.app_service_sku
  identity_id                = module.core.identity_id
  identity_client_id         = module.core.identity_client_id
  app_settings               = module.core.app_settings
  image_name                 = module.core.image_name
  image_host                 = module.core.image_host
  use_managed_pull           = module.core.use_managed_pull
  log_analytics_workspace_id = module.core.log_analytics_workspace_id

  # Wait for AcrPull and Key Vault role propagation inside the core module,
  # including resources this module does not reference directly.
  depends_on = [module.core]
}

module "alerts" {
  source = "../modules/healbot-alerts"

  name                = module.core.name
  resource_group_name = module.core.resource_group_name
  resource_group_id   = module.core.resource_group_id
  tags                = module.core.tags
  hostname            = module.host.hostname
  webhook_token       = module.core.webhook_token
  alert_scope_id      = module.host.id
  metric_namespace    = "Microsoft.Web/sites"
  metric_name         = "Http5xx"
  metric_threshold    = 5
}

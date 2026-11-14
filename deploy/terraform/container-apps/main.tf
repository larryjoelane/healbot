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
  source = "../modules/container-apps"

  name                       = module.core.web_name
  environment_name           = "${module.core.name}-env"
  resource_group_name        = module.core.resource_group_name
  location                   = module.core.location
  tags                       = module.core.tags
  identity_id                = module.core.identity_id
  log_analytics_workspace_id = module.core.log_analytics_workspace_id
  image                      = module.core.full_image
  image_host                 = module.core.image_host
  use_managed_pull           = module.core.use_managed_pull
  plain_env                  = module.core.plain_env
  secret_env                 = module.core.container_secret_env
  webhook_secret_id          = module.core.webhook_secret_id
  admin_secret_id            = module.core.admin_secret_id
  notifier_secrets           = module.core.notifier_secrets

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
  metric_namespace    = "Microsoft.App/containerApps"
  metric_name         = "RestartCount"
  metric_threshold    = 3
}

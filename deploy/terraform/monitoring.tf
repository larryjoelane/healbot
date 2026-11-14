resource "azurerm_monitor_action_group" "bot" {
  name                = "${local.name}-ag"
  resource_group_name = azurerm_resource_group.bot.name
  short_name          = "healbot"
  enabled             = true
  tags                = local.tags

  webhook_receiver {
    name                    = "healbot"
    service_uri             = local.webhook_uri
    use_common_alert_schema = true
  }
}

resource "azurerm_monitor_metric_alert" "symptom" {
  name                = "${local.name}-symptom"
  resource_group_name = azurerm_resource_group.bot.name
  scopes = concat(
    azurerm_linux_web_app.bot[*].id,
    azurerm_container_app.bot[*].id,
  )
  description = "Forward a host symptom to Healbot. Healbot stays in dry run until dry_run is set to false."
  severity    = 2
  frequency   = "PT5M"
  window_size = "PT15M"
  enabled     = true
  tags        = local.tags

  criteria {
    metric_namespace       = var.hosting == "app_service" ? "Microsoft.Web/sites" : "Microsoft.App/containerApps"
    metric_name            = var.hosting == "app_service" ? "Http5xx" : "RestartCount"
    aggregation            = "Total"
    operator               = "GreaterThanOrEqual"
    threshold              = var.hosting == "app_service" ? 5 : 3
    skip_metric_validation = true
  }

  action {
    action_group_id = azurerm_monitor_action_group.bot.id
  }
}

resource "azurerm_monitor_activity_log_alert" "resource_health" {
  name                = "${local.name}-health"
  resource_group_name = azurerm_resource_group.bot.name
  location            = "global"
  scopes              = [azurerm_resource_group.bot.id]
  description         = "Forward degraded or unavailable resource health events in this group to Healbot."
  enabled             = true
  tags                = local.tags

  criteria {
    category = "ResourceHealth"

    resource_health {
      current = ["Degraded", "Unavailable"]
    }
  }

  action {
    action_group_id = azurerm_monitor_action_group.bot.id
  }
}

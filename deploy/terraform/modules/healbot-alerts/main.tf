resource "azurerm_monitor_action_group" "bot" {
  name                = "${var.name}-ag"
  resource_group_name = var.resource_group_name
  short_name          = "healbot"
  enabled             = true
  tags                = var.tags

  webhook_receiver {
    name                    = "healbot"
    service_uri             = "https://${var.hostname}/v1/ingest/monitor?token=${var.webhook_token}"
    use_common_alert_schema = true
  }
}

resource "azurerm_monitor_metric_alert" "symptom" {
  name                = "${var.name}-symptom"
  resource_group_name = var.resource_group_name
  scopes              = [var.alert_scope_id]
  description         = "Forward a host symptom to Healbot. Healbot stays in dry run until dry_run is set to false."
  severity            = 2
  frequency           = "PT5M"
  window_size         = "PT15M"
  enabled             = true
  tags                = var.tags

  criteria {
    metric_namespace       = var.metric_namespace
    metric_name            = var.metric_name
    aggregation            = "Total"
    operator               = "GreaterThanOrEqual"
    threshold              = var.metric_threshold
    skip_metric_validation = true
  }

  action {
    action_group_id = azurerm_monitor_action_group.bot.id
  }
}

resource "azurerm_monitor_activity_log_alert" "resource_health" {
  name                = "${var.name}-health"
  resource_group_name = var.resource_group_name
  location            = "global"
  scopes              = [var.resource_group_id]
  description         = "Forward degraded or unavailable resource health events in this group to Healbot."
  enabled             = true
  tags                = var.tags

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

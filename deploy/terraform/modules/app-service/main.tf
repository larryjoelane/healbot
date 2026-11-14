resource "azurerm_service_plan" "bot" {
  name                = "${var.name}-plan"
  resource_group_name = var.resource_group_name
  location            = var.location
  os_type             = "Linux"
  sku_name            = var.sku
  tags                = var.tags
}

resource "azurerm_linux_web_app" "bot" {
  name                          = var.name
  resource_group_name           = var.resource_group_name
  location                      = var.location
  service_plan_id               = azurerm_service_plan.bot.id
  https_only                    = true
  client_affinity_enabled       = false
  public_network_access_enabled = true

  ftp_publish_basic_authentication_enabled       = false
  webdeploy_publish_basic_authentication_enabled = false

  # User-assigned, created before this app, so Key Vault and ACR grants exist
  # before the first pull and the first Key Vault reference resolution.
  key_vault_reference_identity_id = var.identity_id

  identity {
    type         = "UserAssigned"
    identity_ids = [var.identity_id]
  }

  site_config {
    always_on                                     = true
    health_check_path                             = "/healthz"
    health_check_eviction_time_in_min             = 10
    ftps_state                                    = "Disabled"
    http2_enabled                                 = true
    minimum_tls_version                           = "1.2"
    scm_minimum_tls_version                       = "1.2"
    use_32_bit_worker                             = false
    vnet_route_all_enabled                        = false
    container_registry_use_managed_identity       = var.use_managed_pull
    container_registry_managed_identity_client_id = var.use_managed_pull ? var.identity_client_id : null

    application_stack {
      docker_image_name   = var.image_name
      docker_registry_url = "https://${var.image_host}"
    }
  }

  # Console logs only. HTTP logs would store the webhook token the action
  # group has to put in the query string.
  logs {
    detailed_error_messages = false
    failed_request_tracing  = false
    application_logs {
      file_system_level = "Information"
    }
  }

  app_settings = merge(var.app_settings, {
    WEBSITES_PORT                       = "43117"
    WEBSITES_ENABLE_APP_SERVICE_STORAGE = "false"
    WEBSITES_CONTAINER_START_TIME_LIMIT = "600"
  })

  tags = var.tags
}

# Platform logs, not request logs. Request logs would store the webhook token
# that the action group places in the query string.
resource "azurerm_monitor_diagnostic_setting" "app" {
  name                       = "healbot-platform"
  target_resource_id         = azurerm_linux_web_app.bot.id
  log_analytics_workspace_id = var.log_analytics_workspace_id

  enabled_log {
    category = "AppServiceConsoleLogs"
  }

  enabled_log {
    category = "AppServicePlatformLogs"
  }

  enabled_metric {
    category = "AllMetrics"
  }
}

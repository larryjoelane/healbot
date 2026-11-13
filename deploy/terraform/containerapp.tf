resource "azurerm_container_app_environment" "bot" {
  count                      = var.hosting == "container_apps" ? 1 : 0
  name                       = "${local.name}-env"
  location                   = azurerm_resource_group.bot.location
  resource_group_name        = azurerm_resource_group.bot.name
  log_analytics_workspace_id = azurerm_log_analytics_workspace.bot.id
  logs_destination           = "log-analytics"
  tags                       = local.tags
}

resource "azurerm_container_app" "bot" {
  count                        = var.hosting == "container_apps" ? 1 : 0
  name                         = local.web_name
  resource_group_name          = azurerm_resource_group.bot.name
  container_app_environment_id = azurerm_container_app_environment.bot[count.index].id
  revision_mode                = "Single"
  tags                         = local.tags

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.bot.id]
  }

  secret {
    name                = "webhook-token"
    key_vault_secret_id = azurerm_key_vault_secret.webhook.versionless_id
    identity            = azurerm_user_assigned_identity.bot.id
  }

  secret {
    name                = "admin-token"
    key_vault_secret_id = azurerm_key_vault_secret.admin.versionless_id
    identity            = azurerm_user_assigned_identity.bot.id
  }

  dynamic "secret" {
    for_each = local.notifier_names
    content {
      name                = replace(lower(secret.key), "_", "-")
      key_vault_secret_id = azurerm_key_vault_secret.notifier[secret.key].versionless_id
      identity            = azurerm_user_assigned_identity.bot.id
    }
  }

  dynamic "registry" {
    for_each = local.use_managed_pull ? [1] : []
    content {
      server   = local.image_host
      identity = azurerm_user_assigned_identity.bot.id
    }
  }

  ingress {
    external_enabled           = true
    target_port                = 43117
    transport                  = "auto"
    allow_insecure_connections = false

    traffic_weight {
      latest_revision = true
      percentage      = 100
    }
  }

  template {
    min_replicas = 1
    max_replicas = 2

    container {
      name   = "healbot"
      image  = local.full_image
      cpu    = 0.5
      memory = "1Gi"

      env {
        name  = "HEALBOT_MODE"
        value = local.healbot_env.HEALBOT_MODE
      }
      env {
        name  = "HEALBOT_DRY_RUN"
        value = local.healbot_env.HEALBOT_DRY_RUN
      }
      env {
        name  = "HEALBOT_ADDR"
        value = local.healbot_env.HEALBOT_ADDR
      }
      env {
        name  = "HEALBOT_CONFIG"
        value = local.healbot_env.HEALBOT_CONFIG
      }
      env {
        name  = "HEALBOT_SUBSCRIPTION_ID"
        value = local.healbot_env.HEALBOT_SUBSCRIPTION_ID
      }
      env {
        name  = "HEALBOT_WORKSPACE_ID"
        value = local.healbot_env.HEALBOT_WORKSPACE_ID
      }
      env {
        name  = "HEALBOT_AUDIT_PATH"
        value = local.healbot_env.HEALBOT_AUDIT_PATH
      }
      env {
        name  = "HEALBOT_LLM_PROVIDER"
        value = local.healbot_env.HEALBOT_LLM_PROVIDER
      }
      env {
        name  = "AZURE_CLIENT_ID"
        value = local.healbot_env.AZURE_CLIENT_ID
      }
      env {
        name  = "APPLICATIONINSIGHTS_CONNECTION_STRING"
        value = local.healbot_env.APPLICATIONINSIGHTS_CONNECTION_STRING
      }
      env {
        name        = "HEALBOT_WEBHOOK_TOKEN"
        secret_name = "webhook-token"
      }
      env {
        name        = "HEALBOT_ADMIN_TOKEN"
        secret_name = "admin-token"
      }

      dynamic "env" {
        for_each = local.notifier_names
        content {
          name        = env.key
          secret_name = replace(lower(env.key), "_", "-")
        }
      }

      liveness_probe {
        transport        = "HTTP"
        port             = 43117
        path             = "/healthz"
        interval_seconds = 30
      }

      readiness_probe {
        transport        = "HTTP"
        port             = 43117
        path             = "/readyz"
        interval_seconds = 10
      }
    }
  }

  depends_on = [
    time_sleep.kv_rbac,
    azurerm_role_assignment.acr_pull,
    azurerm_key_vault_secret.webhook,
    azurerm_key_vault_secret.admin,
  ]
}

resource "azurerm_container_app_environment" "bot" {
  name                       = var.environment_name
  location                   = var.location
  resource_group_name        = var.resource_group_name
  log_analytics_workspace_id = var.log_analytics_workspace_id
  logs_destination           = "log-analytics"
  tags                       = var.tags
}

resource "azurerm_container_app" "bot" {
  name                         = var.name
  resource_group_name          = var.resource_group_name
  container_app_environment_id = azurerm_container_app_environment.bot.id
  revision_mode                = "Single"
  tags                         = var.tags

  identity {
    type         = "UserAssigned"
    identity_ids = [var.identity_id]
  }

  secret {
    name                = "webhook-token"
    key_vault_secret_id = var.webhook_secret_id
    identity            = var.identity_id
  }

  secret {
    name                = "admin-token"
    key_vault_secret_id = var.admin_secret_id
    identity            = var.identity_id
  }

  dynamic "secret" {
    for_each = var.notifier_secrets
    content {
      name                = secret.value.name
      key_vault_secret_id = secret.value.key_vault_secret_id
      identity            = var.identity_id
    }
  }

  dynamic "registry" {
    for_each = var.use_managed_pull ? [1] : []
    content {
      server   = var.image_host
      identity = var.identity_id
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
      image  = var.image
      cpu    = 0.5
      memory = "1Gi"

      dynamic "env" {
        for_each = var.plain_env
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = var.secret_env
        content {
          name        = env.key
          secret_name = env.value
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
}

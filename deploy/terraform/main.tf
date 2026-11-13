data "azurerm_client_config" "current" {}

resource "random_string" "suffix" {
  length  = 6
  upper   = false
  special = false
}

resource "random_password" "webhook" {
  length  = 48
  special = false
}

resource "random_password" "admin" {
  length  = 48
  special = false
}

locals {
  name    = "${var.name}-${random_string.suffix.result}"
  compact = substr(join("", regexall("[a-z0-9]", lower(var.name))), 0, 10)
  tags    = merge({ component = "healbot" }, var.tags)

  kv_name  = substr("kv${local.compact}${random_string.suffix.result}", 0, 24)
  acr_name = substr("acr${local.compact}${random_string.suffix.result}", 0, 50)
  web_name = substr(local.name, 0, 60)

  rg_name = var.resource_group_name != "" ? var.resource_group_name : "${var.name}-rg"

  # Parse the caller-supplied image only. It is known at plan time.
  # A host is the first slash-separated segment when that segment contains a dot.
  given_parts    = split("/", var.container_image)
  given_has_host = length(local.given_parts) > 1 && strcontains(local.given_parts[0], ".")
  given_host     = local.given_has_host ? local.given_parts[0] : "index.docker.io"
  given_name     = local.given_has_host ? join("/", slice(local.given_parts, 1, length(local.given_parts))) : var.container_image

  acr_login  = try(azurerm_container_registry.bot[0].login_server, "")
  image_host = var.create_container_registry && !local.given_has_host ? local.acr_login : local.given_host
  image_name = var.create_container_registry && !local.given_has_host ? var.container_image : local.given_name
  full_image = var.create_container_registry && !local.given_has_host ? "${local.acr_login}/${var.container_image}" : var.container_image

  use_managed_pull = var.create_container_registry || var.container_registry_id != ""
  acr_scopes = concat(
    azurerm_container_registry.bot[*].id,
    var.create_container_registry || var.container_registry_id == "" ? [] : [var.container_registry_id],
  )

  operator_scopes = toset(concat(
    [azurerm_resource_group.bot.id],
    var.target_resource_group_ids,
  ))

  read_and_restart = [
    "Microsoft.Web/sites/read",
    "Microsoft.Web/sites/restart/action",
    "Microsoft.Web/sites/slots/read",
    "Microsoft.Web/sites/slots/restart/action",
    "Microsoft.Web/sites/slots/slotsswap/action",
    "Microsoft.Web/sites/deployments/read",
    "Microsoft.Web/sites/config/read",
    "Microsoft.Web/serverfarms/read",
    "Microsoft.App/containerApps/read",
    "Microsoft.App/containerApps/revisions/read",
    "Microsoft.App/containerApps/revisions/restart/action",
    "Microsoft.App/containerApps/revisions/activate/action",
    "Microsoft.Insights/components/read",
    "Microsoft.OperationalInsights/workspaces/read",
    "Microsoft.OperationalInsights/workspaces/query/action",
  ]

  operator_actions = concat(
    local.read_and_restart,
    var.enable_plan_scale ? ["Microsoft.Web/serverfarms/write"] : [],
    var.enable_container_scale ? ["Microsoft.App/containerApps/write"] : [],
  )

  # Plain settings shared by both hosts. Tokens are Key Vault references, not literals.
  healbot_env = {
    HEALBOT_MODE                          = "azure"
    HEALBOT_DRY_RUN                       = var.dry_run ? "true" : "false"
    HEALBOT_ADDR                          = ":43117"
    HEALBOT_CONFIG                        = "/config.azure.yaml"
    HEALBOT_SUBSCRIPTION_ID               = data.azurerm_client_config.current.subscription_id
    HEALBOT_WORKSPACE_ID                  = azurerm_log_analytics_workspace.bot.workspace_id
    HEALBOT_AUDIT_PATH                    = "/var/lib/healbot/audit.jsonl"
    HEALBOT_LLM_PROVIDER                  = "heuristic"
    AZURE_CLIENT_ID                       = azurerm_user_assigned_identity.bot.client_id
    APPLICATIONINSIGHTS_CONNECTION_STRING = azurerm_application_insights.bot.connection_string
  }

  secret_env = merge(
    {
      HEALBOT_WEBHOOK_TOKEN = "@Microsoft.KeyVault(SecretUri=${azurerm_key_vault_secret.webhook.versionless_id})"
      HEALBOT_ADMIN_TOKEN   = "@Microsoft.KeyVault(SecretUri=${azurerm_key_vault_secret.admin.versionless_id})"
    },
    {
      for key, secret in azurerm_key_vault_secret.notifier :
      key => "@Microsoft.KeyVault(SecretUri=${secret.versionless_id})"
    },
  )

  hostnames = concat(
    azurerm_linux_web_app.bot[*].default_hostname,
    flatten([
      for app in azurerm_container_app.bot : [for ing in app.ingress : ing.fqdn]
    ]),
  )
  hostname = local.hostnames[0]

  # Action groups cannot set a custom header. The token therefore has to
  # travel in the query string. It is also stored in Key Vault.
  webhook_uri = "https://${local.hostname}/v1/ingest/monitor?token=${random_password.webhook.result}"

  # Keys are environment variable names, not secrets. for_each cannot take
  # the sensitive map itself.
  notifier_names = toset(nonsensitive(keys(var.notifier_tokens)))
}

check "acr_image_form" {
  assert {
    condition     = !var.create_container_registry || !local.given_has_host
    error_message = "When create_container_registry is true, set container_image to repository:tag (for example healbot:0.1.0). The new registry's login server is added for you."
  }
}

resource "azurerm_resource_group" "bot" {
  name     = local.rg_name
  location = var.location
  tags     = local.tags
}

resource "azurerm_user_assigned_identity" "bot" {
  name                = "${local.name}-id"
  location            = azurerm_resource_group.bot.location
  resource_group_name = azurerm_resource_group.bot.name
  tags                = local.tags
}

resource "azurerm_log_analytics_workspace" "bot" {
  name                = local.name
  location            = azurerm_resource_group.bot.location
  resource_group_name = azurerm_resource_group.bot.name
  sku                 = "PerGB2018"
  retention_in_days   = var.log_analytics_retention_days
  daily_quota_gb      = var.log_analytics_daily_quota_gb
  tags                = local.tags
}

resource "azurerm_application_insights" "bot" {
  name                                 = local.name
  location                             = azurerm_resource_group.bot.location
  resource_group_name                  = azurerm_resource_group.bot.name
  workspace_id                         = azurerm_log_analytics_workspace.bot.id
  application_type                     = "web"
  daily_data_cap_in_gb                 = var.log_analytics_daily_quota_gb == -1 ? null : var.log_analytics_daily_quota_gb
  daily_data_cap_notifications_enabled = false
  tags                                 = local.tags
}

resource "azurerm_container_registry" "bot" {
  count               = var.create_container_registry ? 1 : 0
  name                = local.acr_name
  resource_group_name = azurerm_resource_group.bot.name
  location            = azurerm_resource_group.bot.location
  sku                 = "Basic"
  admin_enabled       = false
  tags                = local.tags
}

resource "azurerm_key_vault" "bot" {
  name                       = local.kv_name
  location                   = azurerm_resource_group.bot.location
  resource_group_name        = azurerm_resource_group.bot.name
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  rbac_authorization_enabled = true
  purge_protection_enabled   = false
  soft_delete_retention_days = 7
  tags                       = local.tags
}

resource "azurerm_role_assignment" "deployer_kv" {
  scope                            = azurerm_key_vault.bot.id
  role_definition_name             = "Key Vault Secrets Officer"
  principal_id                     = data.azurerm_client_config.current.object_id
  skip_service_principal_aad_check = true
}

resource "azurerm_role_assignment" "app_kv" {
  scope                            = azurerm_key_vault.bot.id
  role_definition_name             = "Key Vault Secrets User"
  principal_id                     = azurerm_user_assigned_identity.bot.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
}

# Role assignment is not visible to Key Vault data-plane calls immediately.
resource "time_sleep" "kv_rbac" {
  depends_on      = [azurerm_role_assignment.deployer_kv, azurerm_role_assignment.app_kv]
  create_duration = "60s"
}

resource "azurerm_key_vault_secret" "webhook" {
  name         = "webhook-token"
  value        = random_password.webhook.result
  key_vault_id = azurerm_key_vault.bot.id
  content_type = "text/plain"
  depends_on   = [time_sleep.kv_rbac]
}

resource "azurerm_key_vault_secret" "admin" {
  name         = "admin-token"
  value        = random_password.admin.result
  key_vault_id = azurerm_key_vault.bot.id
  content_type = "text/plain"
  depends_on   = [time_sleep.kv_rbac]
}

resource "azurerm_key_vault_secret" "notifier" {
  for_each     = local.notifier_names
  name         = replace(lower(each.key), "_", "-")
  value        = var.notifier_tokens[each.key]
  key_vault_id = azurerm_key_vault.bot.id
  content_type = "text/plain"
  depends_on   = [time_sleep.kv_rbac]
}

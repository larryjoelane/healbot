# One custom role per resource group. A role defined at subscription scope
# would have to list the subscription as an assignable scope, which would let
# a later assignment cover every group. A definition whose only assignable
# scope is the target group cannot be assigned more widely.
resource "azurerm_role_definition" "operator" {
  for_each = local.operator_scopes

  name        = "Healbot Operator ${substr(sha1(each.key), 0, 8)}"
  scope       = each.key
  description = "Read diagnostics and run Healbot reversible actions. Delete and site write are denied."

  permissions {
    actions = local.operator_actions
    not_actions = [
      "Microsoft.Web/sites/delete",
      "Microsoft.Web/sites/write",
      "Microsoft.App/containerApps/delete",
    ]
  }

  assignable_scopes = [each.key]
}

resource "azurerm_role_assignment" "operator" {
  for_each                         = azurerm_role_definition.operator
  scope                            = each.key
  role_definition_id               = each.value.role_definition_resource_id
  principal_id                     = azurerm_user_assigned_identity.bot.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
}

resource "azurerm_role_assignment" "acr_pull" {
  count                            = length(local.acr_scopes)
  scope                            = local.acr_scopes[count.index]
  role_definition_name             = "AcrPull"
  principal_id                     = azurerm_user_assigned_identity.bot.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
}

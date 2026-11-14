plugin "terraform" {
  enabled = true
  preset  = "recommended"
}

plugin "azurerm" {
  enabled = true
  version = "0.33.0"
  source  = "github.com/terraform-linters/tflint-ruleset-azurerm"
}

# A sandbox has to be removable with terraform destroy. prevent_destroy on
# the vault and its secrets would block that.
rule "azurerm_resources_missing_prevent_destroy" {
  enabled = false
}

# The platform auto-heal recycle is a separate restart path from Healbot's
# runbooks. Leave it off so a health blip does not bounce the process.
rule "azurerm_app_service_missing_auto_heal_setting" {
  enabled = false
}

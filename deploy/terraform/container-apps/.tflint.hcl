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

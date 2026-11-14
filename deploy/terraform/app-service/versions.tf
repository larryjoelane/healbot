terraform {
  required_version = ">= 1.6.0"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }

  # Remote state is optional. Create the storage account outside this
  # configuration, then uncomment the block and pass the values with
  # -backend-config or by editing a copy that you do not commit.
  # Do not point this at a real storage account in git.
  #
  # backend "azurerm" {
  #   resource_group_name  = "example-tfstate-rg"
  #   storage_account_name = "exampletfstate000"
  #   container_name       = "tfstate"
  #   key                  = "healbot-app-service.tfstate"
  # }
}

provider "azurerm" {
  features {
    key_vault {
      purge_soft_delete_on_destroy    = false
      recover_soft_deleted_key_vaults = true
    }
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

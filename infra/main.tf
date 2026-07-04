terraform {
  required_version = ">= 1.5"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }

  # Remote state so CI runs and local runs share one source of truth. The
  # storage account/container is created once by the `bootstrap` workflow job
  # (it lives outside Terraform's own state, like ISF's backend-setup). For
  # local-only use, comment this block out to fall back to local state.
  backend "azurerm" {
    resource_group_name  = "forge-tfstate-rg"
    storage_account_name = "forgetfstatea52008"
    container_name       = "tfstate"
    key                  = "forge.tfstate"
    use_oidc             = true
  }
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "rg" {
  name     = "${var.name}-rg"
  location = var.location
}

# Azure OpenAI account. `kind = "OpenAI"` is the OpenAI service specifically.
resource "azurerm_cognitive_account" "openai" {
  name                = "${var.name}-openai"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  kind                = "OpenAI"
  sku_name            = "S0"

  # Use a custom subdomain so the regional inference endpoint resolves; the
  # SDK/HTTP client needs https://<name>.openai.azure.com.
  custom_subdomain_name = "${var.name}-openai"

  tags = {
    Project   = "forge"
    ManagedBy = "Terraform"
  }
}

# A model deployment inside the account. The deployment NAME (not the model
# name) is what Forge sends as AZURE_OPENAI_DEPLOYMENT.
resource "azurerm_cognitive_deployment" "chat" {
  name                 = var.deployment_name
  cognitive_account_id = azurerm_cognitive_account.openai.id

  model {
    format  = "OpenAI"
    name    = var.model_name
    version = var.model_version
  }

  scale {
    type     = "Standard"
    capacity = var.capacity # thousands of tokens-per-minute
  }
}

# The shared Forge substrate: ONE Azure OpenAI account with one deployment per
# mode, provisioned once from this repo and read by every consumer.
#
# Why one shared instance rather than a module call inside each app: Azure
# OpenAI throughput quota is scoped to a subscription and region, not to an
# account. Standing up an account per app does not buy more capacity, it just
# splits the same pool into fragments that cannot lend to each other. One
# account with per-mode deployments keeps the quota pooled and puts rate limits
# in one place you can reason about.
#
# Everything below is a thin call of ./modules/openai. If an app ever needs its
# own isolated substrate — a different region, a separate quota bucket, a
# tenant boundary — it calls that module directly instead of reading this
# state, and gets identical outputs.

terraform {
  required_version = ">= 1.5"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }

  # Remote state is what makes this readable by consumers: their Terraform
  # pulls the endpoint and mode map out of this state with a
  # terraform_remote_state data source (see README). The storage account and
  # container are created once by the `bootstrap` workflow job, outside this
  # state. For local-only use, comment this block out to fall back to local
  # state.
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

module "openai" {
  source = "./modules/openai"

  name     = var.name
  location = var.location
  modes    = var.modes
}

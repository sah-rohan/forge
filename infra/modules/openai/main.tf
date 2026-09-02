# The Forge model kernel's infrastructure, as a reusable Terraform module.
#
# This is the infra half of the same move the code made. The Go and Node
# packages are libraries you *import*; this is a module you *call*. Neither one
# is a running service, and neither one knows anything about the app consuming
# it.
#
# What it owns is exactly what the kernel's "modes" concept needs: one Azure
# OpenAI account, and one model deployment per mode. The deployment is named
# after the mode, so `fast` is served by a deployment literally called "fast"
# and FORGE_MODE_FAST=fast. Changing which model backs a mode is a change to
# var.modes here — no consumer redeploys, no code edits, no call sites touched.

terraform {
  required_version = ">= 1.5"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

resource "azurerm_resource_group" "rg" {
  name     = "${var.name}-rg"
  location = var.location
  tags     = var.tags
}

# The Azure OpenAI account. `kind = "OpenAI"` is the OpenAI service
# specifically (as opposed to the wider Cognitive Services account).
resource "azurerm_cognitive_account" "openai" {
  name                = "${var.name}-openai"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  kind                = "OpenAI"
  sku_name            = var.sku_name

  # A custom subdomain is required for the regional inference endpoint to
  # resolve — the kernel talks to https://<name>.openai.azure.com.
  custom_subdomain_name = "${var.name}-openai"

  # Off by default: the kernel authenticates with the account key. Set
  # var.local_auth_enabled = false once consumers move to Entra ID identities.
  local_auth_enabled = var.local_auth_enabled

  tags = var.tags
}

# One deployment per mode. This for_each is the whole point of the module: the
# kernel's mode -> deployment mapping is declared once, here, and every
# consumer in every language reads it back out of the outputs.
resource "azurerm_cognitive_deployment" "mode" {
  for_each = var.modes

  # The deployment name IS the mode name. Keeping them identical means the
  # env var a consumer sets (FORGE_MODE_DEEP=deep) needs no lookup table.
  name                 = each.key
  cognitive_account_id = azurerm_cognitive_account.openai.id

  model {
    format  = "OpenAI"
    name    = each.value.model
    version = each.value.version
  }

  scale {
    # Newer models (gpt-5*) require "GlobalStandard"; older ones use
    # "Standard". Check a model's skus[] via `az cognitiveservices account
    # list-models` if an apply is rejected.
    type = each.value.sku
    # Provisioned throughput in thousands of tokens per minute. This is the
    # real lever on cost and on 429s — size `fast` generously (it takes the
    # high-volume traffic) and `deep` modestly.
    capacity = each.value.capacity
  }
}

# Data-plane access for each consumer's identity. "Cognitive Services OpenAI
# User" is inference-only: it can call deployments but cannot create, change,
# or read the keys of the account.
resource "azurerm_role_assignment" "openai_user" {
  for_each = var.role_assignments

  scope                = azurerm_cognitive_account.openai.id
  role_definition_name = "Cognitive Services OpenAI User"
  principal_id         = each.value
}

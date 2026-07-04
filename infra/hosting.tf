# Hosting for the Forge kernel API on Azure Container Apps (scale-to-zero).
# Separate file from the OpenAI resources so the model infra and the app infra
# read independently.

# --- Container registry: holds the Forge image the deploy workflow pushes. ---
resource "azurerm_container_registry" "acr" {
  name                = "${replace(var.name, "-", "")}acr"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "Basic"
  admin_enabled       = true
}

# --- Key Vault: the OpenAI key + the Forge API key live here, not in env. ---
data "azurerm_client_config" "current" {}

resource "azurerm_key_vault" "kv" {
  name                = "${var.name}-kv"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  tenant_id           = data.azurerm_client_config.current.tenant_id
  sku_name            = "standard"
}

# The applier (CI SP or you) needs to write secrets.
resource "azurerm_key_vault_access_policy" "applier" {
  key_vault_id       = azurerm_key_vault.kv.id
  tenant_id          = data.azurerm_client_config.current.tenant_id
  object_id          = data.azurerm_client_config.current.object_id
  secret_permissions = ["Get", "List", "Set", "Delete", "Purge", "Recover"]
}

# Store the OpenAI key Terraform already created, so the app reads it from KV.
resource "azurerm_key_vault_secret" "openai_key" {
  name         = "azure-openai-key"
  value        = azurerm_cognitive_account.openai.primary_access_key
  key_vault_id = azurerm_key_vault.kv.id
  depends_on   = [azurerm_key_vault_access_policy.applier]
}

# The API gate key (X-Forge-Key) consumers send. Supplied via TF_VAR or tfvars.
resource "azurerm_key_vault_secret" "forge_api_key" {
  name         = "forge-api-key"
  value        = var.forge_api_key
  key_vault_id = azurerm_key_vault.kv.id
  depends_on   = [azurerm_key_vault_access_policy.applier]
}

# --- Observability ---
resource "azurerm_log_analytics_workspace" "logs" {
  name                = "${var.name}-logs"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  sku                 = "PerGB2018"
  retention_in_days   = 30
}

# --- Container Apps environment + the API app ---
resource "azurerm_container_app_environment" "env" {
  name                       = "${var.name}-env"
  resource_group_name        = azurerm_resource_group.rg.name
  location                   = azurerm_resource_group.rg.location
  log_analytics_workspace_id = azurerm_log_analytics_workspace.logs.id
}

resource "azurerm_container_app" "api" {
  name                         = "${var.name}-api"
  resource_group_name          = azurerm_resource_group.rg.name
  container_app_environment_id = azurerm_container_app_environment.env.id
  revision_mode                = "Single"

  # System-assigned identity so the app can pull from ACR + read Key Vault
  # without stored credentials.
  identity {
    type = "SystemAssigned"
  }

  registry {
    server   = azurerm_container_registry.acr.login_server
    identity = "System"
  }

  # Secrets sourced from Key Vault via the app's managed identity. Use the
  # versionLESS id so rotating a secret doesn't force the container to re-plan
  # against a pinned version (which trips a provider "inconsistent plan" bug).
  secret {
    name                = "azure-openai-key"
    key_vault_secret_id = azurerm_key_vault_secret.openai_key.versionless_id
    identity            = "System"
  }
  secret {
    name                = "forge-api-key"
    key_vault_secret_id = azurerm_key_vault_secret.forge_api_key.versionless_id
    identity            = "System"
  }

  template {
    # Scale to zero when idle — you pay nothing between requests.
    min_replicas = 0
    max_replicas = 2

    container {
      name   = "forge-api"
      image  = "${azurerm_container_registry.acr.login_server}/forge-api:latest"
      cpu    = 0.25
      memory = "0.5Gi"

      env {
        name  = "PORT"
        value = "8090"
      }
      env {
        name  = "FORGE_MODEL_PROVIDER"
        value = "azure"
      }
      env {
        name  = "AZURE_OPENAI_ENDPOINT"
        value = azurerm_cognitive_account.openai.endpoint
      }
      env {
        name  = "AZURE_OPENAI_DEPLOYMENT"
        value = azurerm_cognitive_deployment.chat.name
      }
      env {
        name        = "AZURE_OPENAI_KEY"
        secret_name = "azure-openai-key"
      }
      env {
        name        = "FORGE_API_KEY"
        secret_name = "forge-api-key"
      }
    }
  }

  ingress {
    external_enabled = true
    target_port      = 8090
    traffic_weight {
      percentage      = 100
      latest_revision = true
    }
  }
}

# Let the Container App's identity pull images + read secrets.
resource "azurerm_role_assignment" "acr_pull" {
  scope                = azurerm_container_registry.acr.id
  role_definition_name = "AcrPull"
  principal_id         = azurerm_container_app.api.identity[0].principal_id
}

resource "azurerm_key_vault_access_policy" "app" {
  key_vault_id       = azurerm_key_vault.kv.id
  tenant_id          = data.azurerm_client_config.current.tenant_id
  object_id          = azurerm_container_app.api.identity[0].principal_id
  secret_permissions = ["Get", "List"]
}

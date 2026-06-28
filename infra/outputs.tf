# The three values Forge needs at runtime. Endpoint + deployment are safe to
# print; the key is marked sensitive (read it with `terraform output -raw`).

output "azure_openai_endpoint" {
  description = "Set as AZURE_OPENAI_ENDPOINT."
  value       = azurerm_cognitive_account.openai.endpoint
}

output "azure_openai_deployment" {
  description = "Set as AZURE_OPENAI_DEPLOYMENT."
  value       = azurerm_cognitive_deployment.chat.name
}

output "azure_openai_key" {
  description = "Set as AZURE_OPENAI_KEY. Read with: terraform output -raw azure_openai_key"
  value       = azurerm_cognitive_account.openai.primary_access_key
  sensitive   = true
}

# --- Hosting outputs (consumed by the deploy workflow) ---

output "acr_login_server" {
  description = "Container registry to push the Forge image to."
  value       = azurerm_container_registry.acr.login_server
}

output "acr_name" {
  description = "ACR name (for az acr login)."
  value       = azurerm_container_registry.acr.name
}

output "container_app_name" {
  description = "Container App to update after pushing a new image."
  value       = azurerm_container_app.api.name
}

output "resource_group" {
  description = "Resource group holding all Forge resources."
  value       = azurerm_resource_group.rg.name
}

output "api_url" {
  description = "Public HTTPS URL of the Forge kernel API."
  value       = "https://${azurerm_container_app.api.ingress[0].fqdn}"
}

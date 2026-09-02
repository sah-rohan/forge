# The module's outputs ARE its public API — the infra equivalent of the
# exported surface of the Go and Node packages. Consumers depend on these, not
# on the resource addresses behind them.

output "endpoint" {
  description = "Inference endpoint. Set as AZURE_OPENAI_ENDPOINT."
  value       = azurerm_cognitive_account.openai.endpoint
}

output "api_key" {
  description = "Account key. Set as AZURE_OPENAI_KEY. Read with: terraform output -raw api_key"
  value       = azurerm_cognitive_account.openai.primary_access_key
  sensitive   = true
}

output "modes" {
  description = "Map of mode name -> Azure deployment name serving it."
  value       = { for mode, dep in azurerm_cognitive_deployment.mode : mode => dep.name }
}

output "forge_env" {
  description = <<-EOT
    Every non-secret environment variable a consumer needs, ready to splat into
    an app's settings. This is what makes the module composable: a consuming
    app never hardcodes a mode name or a deployment name, it just forwards this
    map and merges the key in from wherever it keeps secrets.

        locals {
          forge_env = merge(
            module.forge.forge_env,
            { AZURE_OPENAI_KEY = module.forge.api_key },
          )
        }

    Both kernels read exactly these variables, so the same map configures a Go
    service and a Node service without translation.
  EOT
  value = merge(
    { AZURE_OPENAI_ENDPOINT = azurerm_cognitive_account.openai.endpoint },
    { for mode, dep in azurerm_cognitive_deployment.mode : "FORGE_MODE_${upper(replace(mode, "-", "_"))}" => dep.name },
  )
}

output "account_id" {
  description = "Resource ID of the OpenAI account — for role assignments when consumers move to Entra ID auth."
  value       = azurerm_cognitive_account.openai.id
}

output "account_name" {
  description = "Name of the OpenAI account."
  value       = azurerm_cognitive_account.openai.name
}

output "resource_group" {
  description = "Resource group holding the account and its deployments."
  value       = azurerm_resource_group.rg.name
}

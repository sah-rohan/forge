# Re-exported from the module so consumers can read them straight out of this
# root's remote state without knowing the module's internals.

output "endpoint" {
  description = "Set as AZURE_OPENAI_ENDPOINT."
  value       = module.openai.endpoint
}

output "api_key" {
  description = "Set as AZURE_OPENAI_KEY. Read with: terraform output -raw api_key"
  value       = module.openai.api_key
  sensitive   = true
}

output "modes" {
  description = "Map of mode name -> Azure deployment name serving it."
  value       = module.openai.modes
}

output "forge_env" {
  description = "Every non-secret environment variable a consumer needs, ready to splat into an app's settings."
  value       = module.openai.forge_env
}

output "account_id" {
  description = "Resource ID of the OpenAI account — for role assignments when consumers move to Entra ID auth."
  value       = module.openai.account_id
}

output "resource_group" {
  description = "Resource group holding all Forge resources."
  value       = module.openai.resource_group
}

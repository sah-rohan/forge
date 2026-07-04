variable "name" {
  description = "Base name for all resources. Must be globally unique-ish because it drives ACR, Key Vault, and the OpenAI subdomain — all of which live in GLOBAL namespaces. Keep the random suffix."
  type        = string
  default     = "forge3adc8d"
}

variable "location" {
  description = "Azure region. Must support the chosen model — eastus / swedencentral are good defaults."
  type        = string
  default     = "eastus"
}

variable "deployment_name" {
  description = "Name of the model deployment. This is what Forge sends as AZURE_OPENAI_DEPLOYMENT (NOT the model name)."
  type        = string
  default     = "gpt-4o-mini"
}

variable "model_name" {
  description = "Azure OpenAI base model to deploy."
  type        = string
  default     = "gpt-4o-mini"
}

variable "model_version" {
  description = "Model version. Check availability in your region if apply fails."
  type        = string
  default     = "2024-07-18"
}

variable "capacity" {
  description = "Provisioned throughput in thousands of tokens-per-minute (TPM/1000). 10 = 10K TPM, plenty for dev."
  type        = number
  default     = 10
}

variable "forge_api_key" {
  description = "The X-Forge-Key consumers must send to call the kernel API. Supply via TF_VAR_forge_api_key (CI secret) or terraform.tfvars."
  type        = string
  sensitive   = true
}

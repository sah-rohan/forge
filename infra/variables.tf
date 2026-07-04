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
  description = "Name of the model deployment. This is what Forge sends as AZURE_OPENAI_DEPLOYMENT (NOT the model name). Kept model-agnostic ('chat') so swapping the underlying model doesn't require renaming the deployment."
  type        = string
  default     = "chat"
}

variable "model_name" {
  description = "Azure OpenAI base model to deploy."
  type        = string
  default     = "gpt-5-mini"
}

variable "model_version" {
  description = "Model version. Check availability in your region if apply fails (az cognitiveservices account list-models)."
  type        = string
  default     = "2025-08-07"
}

variable "sku_type" {
  description = "Deployment SKU type. Newer models (gpt-5*) require 'GlobalStandard', older ones use 'Standard'. Check the model's skus[] via list-models."
  type        = string
  default     = "GlobalStandard"
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

variable "name" {
  description = <<-EOT
    Base name for all resources. Drives the OpenAI account's custom subdomain,
    which lives in a GLOBAL namespace — keep a random suffix so two people can
    stand this up independently.
  EOT
  type        = string
}

variable "location" {
  description = "Azure region. Must support every model in var.modes — eastus and swedencentral are good defaults."
  type        = string
  default     = "eastus"
}

variable "sku_name" {
  description = "SKU for the Cognitive Services account itself. S0 is the only standard tier for OpenAI."
  type        = string
  default     = "S0"
}

variable "modes" {
  description = <<-EOT
    The kernel's modes, mapped to the model that serves each one. The map key
    becomes both the mode name and the Azure deployment name.

    Add a mode here and it exists everywhere at once: the Go kernel and the
    Node kernel both discover modes from FORGE_MODE_* environment variables, so
    a new key needs no code change in either language.

    Capacity is thousands of tokens per minute. It is subscription- and
    region-scoped quota, so the sum across every mode (and every other OpenAI
    account you run in that region) is what actually has to fit.

    Required deliberately: a module that silently picks models for you hides
    the most consequential decision in the stack. The caller states it.
  EOT
  type = map(object({
    model    = string
    version  = string
    sku      = optional(string, "GlobalStandard")
    capacity = optional(number, 10)
  }))

  validation {
    condition     = length(var.modes) > 0
    error_message = "At least one mode must be defined — the kernel refuses to start with no modes configured."
  }

  validation {
    condition     = alltrue([for m in keys(var.modes) : can(regex("^[a-z0-9-]+$", m))])
    error_message = "Mode names must be lowercase alphanumeric or hyphens; they become Azure deployment names and FORGE_MODE_* variables."
  }
}

variable "local_auth_enabled" {
  description = <<-EOT
    Whether the account accepts API-key auth. True today because the kernel
    authenticates with the account key. Set false once consumers authenticate
    with Entra ID managed identities instead.
  EOT
  type        = bool
  default     = true
}

variable "tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default = {
    Project   = "forge"
    ManagedBy = "Terraform"
  }
}

variable "role_assignments" {
  description = <<-EOT
    Principal object IDs granted "Cognitive Services OpenAI User" on the
    account, keyed by a stable name so adding one does not move the others.

    This is the other half of Entra ID auth: the package acquires a token, and
    this is what makes that token mean something. Grant every consumer's
    managed identity here, then set local_auth_enabled = false and the account
    key stops existing as an attack surface.
  EOT
  type        = map(string)
  default     = {}
}

variable "name" {
  description = <<-EOT
    Base name for all resources. Drives the OpenAI account's custom subdomain,
    which lives in a GLOBAL namespace — keep the random suffix.
  EOT
  type        = string
  default     = "forge3adc8d"
}

variable "location" {
  description = "Azure region. Must support every model in var.modes — eastus and swedencentral are good defaults."
  type        = string
  default     = "eastus"
}

variable "modes" {
  description = <<-EOT
    The mode -> model mapping for the shared substrate. The map key is both the
    mode name and the Azure deployment name.

    Adding a mode here is the entire change: both kernels discover modes from
    FORGE_MODE_* variables at startup, so no Go or TypeScript is touched.

    Capacity is thousands of tokens per minute, and it is subscription- and
    region-scoped quota — the sum across every mode is what has to fit.
  EOT
  type = map(object({
    model    = string
    version  = string
    sku      = optional(string, "GlobalStandard")
    capacity = optional(number, 10)
  }))

  default = {
    # High-volume workhorse: classification, extraction, routing. Most
    # capacity, because it takes the most traffic.
    fast = {
      model    = "gpt-5-mini"
      version  = "2025-08-07"
      capacity = 30
    }
    # Most real work: conversation, drafting, tool use.
    balanced = {
      model    = "gpt-5-mini"
      version  = "2025-08-07"
      capacity = 20
    }
    # Hard reasoning. Expensive per token and reached for deliberately, so it
    # needs the least throughput.
    deep = {
      model    = "gpt-5"
      version  = "2025-08-07"
      capacity = 10
    }
  }
}

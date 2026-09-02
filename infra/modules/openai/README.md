# `openai` — the Forge kernel's substrate

One Azure OpenAI account, and one model deployment per kernel **mode**.

This is the infra half of the same move the code made. The Go and Node packages
are libraries you *import*; this is a module you *call*. Neither is a running
service, and neither knows anything about the app consuming it.

```
code   →  import "github.com/sah-rohan/forge"                    // Go
          import { defaultCredential } from "@sah-rohan/forge"   // Node
infra  →  module "openai" { source = ".../infra/modules/openai" }
```

The package authenticates against this account; this module creates it, its
deployments, and the role assignments that let a managed identity call them.

## What it owns

| Resource | Why |
|---|---|
| Resource group | Holds everything the module creates. |
| `azurerm_cognitive_account` (kind `OpenAI`) | The account the kernel talks to. A custom subdomain is set so the regional endpoint resolves. |
| `azurerm_cognitive_deployment` × modes | One per mode, created with `for_each`. |

**The deployment name is the mode name.** Mode `fast` is served by a deployment
literally called `fast`, so `FORGE_MODE_FAST=fast` in your application's
settings and no lookup table exists anywhere. Changing which model backs a mode is a change to `var.modes` — no
consumer redeploys, no code edits, no call sites touched.

## Usage

```hcl
module "openai" {
  source = "git::ssh://git@github.com/sah-rohan/forge.git//infra/modules/openai?ref=v0.1.0"

  name     = "forge3adc8d"   # drives a GLOBAL subdomain — keep a random suffix
  location = "eastus"

  modes = {
    fast     = { model = "gpt-5-mini", version = "2025-08-07", capacity = 30 }
    balanced = { model = "gpt-5-mini", version = "2025-08-07", capacity = 20 }
    deep     = { model = "gpt-5",      version = "2025-08-07", capacity = 10 }
  }
}
```

`modes` is required on purpose: a module that silently picks models for you
hides the most consequential decision in the stack.

Private-repo sourcing needs no registry — Terraform reads the git repo the same
way `go get` does. Pin `?ref=` to a tag so an infra change lands when you choose.

## Inputs

| Name | Type | Default | Notes |
|---|---|---|---|
| `name` | `string` | — | Base name. Drives a **global** subdomain; keep a random suffix. |
| `location` | `string` | `eastus` | Must support every model in `modes`. |
| `modes` | `map(object)` | — | `{ model, version, sku?, capacity? }` per mode. Key = mode = deployment name. |
| `sku_name` | `string` | `S0` | Account tier. |
| `local_auth_enabled` | `bool` | `true` | API-key auth. Set `false` once consumers use Entra ID. |
| `tags` | `map(string)` | project tags | Applied to every resource. |

Per-mode `sku` defaults to `GlobalStandard` (required by `gpt-5*`; older models
use `Standard`). `capacity` defaults to `10` and is **thousands of tokens per
minute**.

## Outputs

The outputs are the module's public API — depend on these, not on the resource
addresses behind them.

| Output | Use |
|---|---|
| `endpoint` | `AZURE_OPENAI_ENDPOINT` |
| `api_key` (sensitive) | `AZURE_OPENAI_KEY` |
| `modes` | `{ mode => deployment name }` |
| `forge_env` | Every non-secret env var, ready to splat into app settings |
| `account_id` | For role assignments under Entra ID auth |
| `resource_group` | — |

`forge_env` is what makes the module composable. A consuming app never
hardcodes a mode or deployment name:

```hcl
locals {
  forge_env = merge(
    module.openai.forge_env,
    { AZURE_OPENAI_KEY = module.openai.api_key },
  )
}
```

Both kernels read exactly these variables, so the same map configures a Go
service and a Node service without translation.

## Capacity is the thing to get right

Throughput quota is scoped to a **subscription and region**, not to an account.
Standing up an account per app does not buy more capacity — it splits one pool
into fragments that cannot lend to each other. That is why the repo root
provisions a single shared instance of this module rather than each app calling
it. Call it directly only when an app genuinely needs an isolated substrate: a
different region, a separate quota bucket, or a tenant boundary.

The sum of `capacity` across every mode here, plus every other OpenAI account
in that region, is what has to fit. Size `fast` generously — it takes the
high-volume traffic — and `deep` modestly.

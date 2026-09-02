# Forge infra

Terraform that provisions the Azure OpenAI account and one model deployment per
kernel **mode**. State lives in a remote `azurerm` backend so CI and local runs
agree.

The layout mirrors the code: `modules/openai/` is the reusable piece (the infra
equivalent of the importable package), and this root is one thin call of it.

```
infra/
  modules/openai/   the reusable module — see its README
  main.tf           one call of that module: the SHARED substrate
  variables.tf      the mode -> model mapping
  outputs.tf        re-exported so consumers can read them from remote state
```

## Why one shared instance

Azure OpenAI throughput quota is scoped to a **subscription and region**, not
to an account. An account per app does not buy more capacity — it splits one
pool into fragments that cannot lend to each other. So this root stands up a
single substrate and every consumer reads from it.

An app that genuinely needs isolation — a different region, a separate quota
bucket, a tenant boundary — calls `modules/openai` directly instead and gets
identical outputs.

## CI — GitHub Actions, OIDC

`.github/workflows/terraform.yml` is one job running four commands: `init`,
`validate`, `plan`, and (on merge to `main` only) `apply`. A PR stops after the
plan.

### Required repo secrets (Settings → Secrets → Actions)

| Secret | What |
|---|---|
| `AZURE_CLIENT_ID` | App registration (service principal) client ID, federated to this repo |
| `AZURE_TENANT_ID` | Azure AD tenant ID |
| `AZURE_SUBSCRIPTION_ID` | Target subscription |

The SP needs **Contributor** on the subscription (or the relevant RGs) to create
the OpenAI account, and **Storage Blob Data Contributor** on the state storage
account. Federate the credential against
`repo:sah-rohan/forge:ref:refs/heads/main` and `:pull_request`.

## Local provision

```bash
az login
az account set --subscription <your-sub-id>

terraform -chdir=infra init      # connects to the remote backend
terraform -chdir=infra plan      # 1 RG + 1 OpenAI account + 1 deployment per mode
terraform -chdir=infra apply
```

> **First-ever run:** the state backend must already exist — Terraform cannot
> hold its own backend, so nothing in CI creates it either. Once, by hand:
> ```bash
> az group create -n forge-tfstate-rg -l eastus
> az storage account create -n forgetfstatea52008 -g forge-tfstate-rg -l eastus --sku Standard_LRS
> az storage container create -n tfstate --account-name forgetfstatea52008 --auth-mode login
> ```

## Wire the outputs into a consumer

### Locally

```bash
cd infra
export AZURE_OPENAI_ENDPOINT="$(terraform output -raw endpoint)"
export AZURE_OPENAI_KEY="$(terraform output -raw api_key)"
eval "$(terraform output -json forge_env | jq -r 'to_entries[] | "export \(.key)=\(.value)"')"
```

That leaves `AZURE_OPENAI_ENDPOINT` plus one `FORGE_MODE_*` per mode in the
environment. Those are your application's configuration — Forge itself only
authenticates, and reads none of them.

### From a consumer's Terraform

Read this root's state; don't copy values between repos.

```hcl
data "terraform_remote_state" "forge" {
  backend = "azurerm"
  config = {
    resource_group_name  = "forge-tfstate-rg"
    storage_account_name = "forgetfstatea52008"
    container_name       = "tfstate"
    key                  = "forge.tfstate"
    use_oidc             = true
  }
}

# Splat straight into the app's settings — no mode or deployment name is ever
# hardcoded on the consumer side.
locals {
  forge_env = merge(
    data.terraform_remote_state.forge.outputs.forge_env,
    { AZURE_OPENAI_KEY = data.terraform_remote_state.forge.outputs.api_key },
  )
}
```

Add a mode in `variables.tf` here, apply, and it appears in every consumer's
`forge_env` on their next apply. Neither kernel needs a code change — both
discover modes from `FORGE_MODE_*` at startup.

Under Entra ID auth (`local_auth_enabled = false` plus `role_assignments`)
there is no `AZURE_OPENAI_KEY` to merge in at all, and `forge_env` is the
complete, entirely non-secret configuration.

## Customize

Override in a `terraform.tfvars` (gitignored) or via `-var`:

```hcl
location = "swedencentral"

modes = {
  fast     = { model = "gpt-5-mini", version = "2025-08-07", capacity = 50 }
  balanced = { model = "gpt-5",      version = "2025-08-07", capacity = 20 }
  deep     = { model = "gpt-5",      version = "2025-08-07", capacity = 10 }
  # A new mode needs no code change in either language.
  vision   = { model = "gpt-5",      version = "2025-08-07", capacity = 5 }
}
```

## Gotchas

- **Provider registration:** if apply errors about `Microsoft.CognitiveServices`,
  run `az provider register --namespace Microsoft.CognitiveServices` and retry.
- **Model/region availability:** not every model is offered in every region. If a
  deployment fails, switch `location` (try `swedencentral`) or adjust `version`.
- **SKU type:** `gpt-5*` requires `GlobalStandard` (the per-mode default); older
  models use `Standard`. Check a model's `skus[]` via
  `az cognitiveservices account list-models`.
- **Quota:** the sum of `capacity` across all modes competes with every other
  OpenAI account in the region. A 429 storm usually means capacity, not code —
  the kernel retries them, but it cannot manufacture throughput.
- **Cost:** the account costs nothing at rest; you pay per token. Deployments are
  free to keep provisioned under the standard SKUs.

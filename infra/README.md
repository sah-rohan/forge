# Forge infra

Terraform that provisions the Azure OpenAI resource + model deployment Forge
uses. State lives in a remote `azurerm` backend so CI and local runs agree.

## CI (recommended) — GitHub Actions, OIDC

`.github/workflows/terraform.yml` runs automatically:
- **PR touching `infra/**`** → `fmt` + `validate` + `plan`
- **merge to `main`** → `apply`
- A `bootstrap-state` job idempotently creates the state storage account first.

### Required repo secrets (Settings → Secrets → Actions)
The same OIDC app registration pattern as ISF:

| Secret | What |
|---|---|
| `AZURE_CLIENT_ID` | App registration (service principal) client ID, federated to this repo |
| `AZURE_TENANT_ID` | Azure AD tenant ID |
| `AZURE_SUBSCRIPTION_ID` | Target subscription |

The SP needs **Contributor** on the subscription (or the relevant RGs) to
create the OpenAI account, and **Storage Blob Data Contributor** on the state
storage account. Set up the federated credential to trust
`repo:<owner>/forge:ref:refs/heads/main` and `:pull_request`.

## Local provision (alternative)

```bash
cd infra
az login
az account set --subscription <your-sub-id>

terraform init                 # connects to the remote backend
terraform plan                 # 1 RG + 1 OpenAI account + 1 deployment
terraform apply
```

> First-ever run: the remote backend storage must exist. Either let the CI
> `bootstrap-state` job create it once, or create it locally:
> ```bash
> az group create -n forge-tfstate-rg -l eastus
> az storage account create -n forgetfstate -g forge-tfstate-rg -l eastus --sku Standard_LRS
> az storage container create -n tfstate --account-name forgetfstate --auth-mode login
> ```

## Wire the outputs into Forge

```bash
export FORGE_MODEL_PROVIDER=azure
export AZURE_OPENAI_ENDPOINT="$(terraform output -raw azure_openai_endpoint)"
export AZURE_OPENAI_DEPLOYMENT="$(terraform output -raw azure_openai_deployment)"
export AZURE_OPENAI_KEY="$(terraform output -raw azure_openai_key)"

cd .. && go run ./cmd/api
```

## Customize

Override defaults in a `terraform.tfvars` (gitignored) or via `-var`:

```hcl
location        = "swedencentral"
model_name      = "gpt-4o"
deployment_name = "gpt-4o"
model_version   = "2024-08-06"
capacity        = 30
```

## Gotchas

- **Provider registration:** if apply errors about `Microsoft.CognitiveServices`,
  run `az provider register --namespace Microsoft.CognitiveServices` and retry.
- **Model/region availability:** not every model is offered in every region. If
  the deployment fails, switch `location` (try `swedencentral`) or adjust
  `model_version`.
- **Deployment name ≠ model name:** `AZURE_OPENAI_DEPLOYMENT` is the deployment
  you named here, not the base model.
- **Cost:** `gpt-4o-mini` is ~$0.15/1M input tokens. The account itself costs
  nothing at rest — you pay per token used.

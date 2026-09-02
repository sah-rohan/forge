# Forge

**An authentication kernel for Azure OpenAI.** One small library that resolves,
caches, refreshes, and attaches the credentials your services use to call the
model — and does nothing else.

It is **not a service.** You import it and call it in-process. It opens no port,
exposes no endpoint, and adds no network hop. The only requests it ever makes
are to an identity endpoint, on your behalf, about once an hour.

It **contains no credentials.** A credential is something you supply — a managed
identity, a mounted file, or a key you hold. Nothing secret is compiled in,
checked in, or published with it, which is why this repository is safe to make
public regardless of who consumes it.

It ships twice, with the same API in both, because consumers are split across
two runtimes:

| | Import | Source |
|---|---|---|
| **Go** | `github.com/sah-rohan/forge` | repo root |
| **Node** | `@sah-rohan/forge` | [`node/`](node/) |
| **Terraform** | `modules/openai` | [`infra/modules/openai/`](infra/modules/openai/) |

## The one idea

**A credential is resolved per request, not captured at startup.**

Everything else follows from that. A bearer token that expires in an hour, an
account key remounted by Kubernetes, a secret rotated in Key Vault — all of them
keep working without a restart, because nothing is frozen into the process at
construction time.

Around that sit four behaviours you would otherwise write in every service:

- **Caching.** A token is acquired once and reused until it is nearly expired,
  with a five-minute skew so a long call never starts on a dying token.
- **Coalescing.** Twenty concurrent requests arriving on an expired token
  produce one call to the identity endpoint, not twenty.
- **Reauthentication.** A request rejected with 401 invalidates the cached
  token and retries once, so a revoked token recovers on its own instead of
  failing every call until the process restarts.
- **A singleton.** `Default()` builds the credential once per process, so one
  token cache serves your whole binary.

## Go

```bash
go get github.com/sah-rohan/forge
```

The repo needs no registry — `go get` reads the git repo directly. If you keep
it private:

```bash
go env -w GOPRIVATE=github.com/sah-rohan/*
git config --global url."git@github.com:".insteadOf https://github.com/
```

```go
// An HTTP client that authenticates everything it sends.
client := forge.Client(forge.Default())

resp, err := client.Post(
    endpoint+"/openai/deployments/gpt-5/chat/completions?api-version=2024-10-21",
    "application/json", body,
)
```

Hand that client to any SDK that accepts an `*http.Client` and authentication
stops being something your code thinks about. If you already have a transport
worth keeping — tracing, connection tuning, a proxy — wrap it instead:

```go
client := &http.Client{Transport: forge.Transport(forge.Default(), myTransport)}
```

And if you want the headers yourself:

```go
headers, err := forge.Default().Headers(ctx)
```

## Node

```bash
npm install @sah-rohan/forge
```

Published privately to GitHub Packages. In the consumer's `.npmrc`:

```
@sah-rohan:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

```ts
import { authorizedFetch, defaultCredential } from "@sah-rohan/forge";

const fetch = authorizedFetch(defaultCredential());

const res = await fetch(
  `${endpoint}/openai/deployments/gpt-5/chat/completions?api-version=2024-10-21`,
  { method: "POST", body: JSON.stringify(payload) },
);
```

`authorizedFetch` returns a drop-in `fetch`, so it goes straight into any client
that accepts one — including the official `openai` package's `fetch` option. Or
take the headers directly:

```ts
const headers = await defaultCredential().headers();
```

## Credentials

Four implementations cover the ground. All of them satisfy one interface, so
swapping between them is a one-line change at startup and nothing downstream
notices.

| | Go | Node | Use |
|---|---|---|---|
| Managed identity | `EntraID("")` | `entraID()` | **Production.** No secret anywhere. |
| Key on disk | `KeyFile(path)` | `keyFile(path)` | Key auth that can rotate. |
| Key in hand | `StaticKey(k)` | `staticKey(k)` | Local development, tests. |
| Anything else | `FromTokenProvider(fn)` | `fromTokenProvider(fn)` | `azidentity`, `@azure/identity`, a broker. |

`EntraID` is the one to deploy with, because with it the application holds no
secret at all: nothing in app settings, nothing in Terraform state, nothing to
rotate. Its identity flow is detected from the environment:

| Environment | Flow |
|---|---|
| `AZURE_FEDERATED_TOKEN_FILE` | Workload identity federation (AKS) |
| `AZURE_CLIENT_SECRET` | Service principal |
| `IDENTITY_ENDPOINT` | App Service / Container Apps managed identity |
| *(none of the above)* | IMDS managed identity (VMs, VMSS) |

Anything outside that list plugs in through `FromTokenProvider` rather than
being built in, which is what keeps this package dependency-free:

```go
cred := forge.FromTokenProvider(func(ctx context.Context) (string, time.Time, error) {
    t, err := azCred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{forge.DefaultScope}})
    return t.Token, t.ExpiresOn, err
})
```

## Configuration

Both runtimes read the same environment, so one set of app settings configures a
Go service and a Node service without translation.

| Variable | |
|---|---|
| `AZURE_OPENAI_KEY_FILE` | Account key on disk, re-read as it rotates |
| `AZURE_OPENAI_KEY` | Account key held inline |
| `AZURE_OPENAI_SCOPE` | Optional — overrides the default Entra ID scope |

The credential is resolved in that order: key file, then inline key, then Entra
ID. Explicit keys win so an existing deployment keeps working untouched; the
file form is preferred over the inline one because it can rotate; Entra ID is
preferred over both because it means there is no secret to hold.

Setting none of them is the intended production configuration.

## Sharing one credential

`forge.Default()` / `defaultCredential()` build the credential once per process
and hand the same instance to every caller. Use it.

Sharing is not a convenience — it is the optimization. A credential caches its
token and coalesces its refreshes, so **one instance means one token fetch per
hour for the whole binary.** Two instances mean two of everything: two caches,
two refresh cycles, two chances to be mid-refresh when a request arrives. Build
your own with `FromEnv` / `fromEnv` only when you genuinely need a second
identity.

Resolution is lazy. Nothing touches the environment or the network until the
first call, and no token is acquired until the first request needs one.

## Infrastructure

[`infra/modules/openai`](infra/modules/openai/) provisions the account this
authenticates against, and the role assignments that make an Entra ID token
mean something:

```hcl
module "openai" {
  source = "git::ssh://git@github.com/sah-rohan/forge.git//infra/modules/openai?ref=v0.2.0"

  name     = "forge3adc8d"
  location = "eastus"

  modes = {
    fast = { model = "gpt-5-mini", version = "2025-08-07", capacity = 30 }
    deep = { model = "gpt-5",      version = "2025-08-07", capacity = 10 }
  }

  role_assignments = {
    api    = azurerm_linux_web_app.api.identity[0].principal_id
    worker = azurerm_container_app.worker.identity[0].principal_id
  }

  local_auth_enabled = false
}
```

`role_assignments` grants **Cognitive Services OpenAI User** — inference only,
with no ability to create deployments, change the account, or read its keys.
With every consumer listed there, `local_auth_enabled = false` removes key auth
entirely and the account key stops existing as an attack surface.

The module also outputs the endpoint and its deployment names. Those are your
application's configuration, not this package's — Forge authenticates; what you
call and with which model is yours to decide.

## Layout

```
auth.go                    the Go kernel
node/src/auth.ts           the Node kernel (same API)
infra/modules/openai/      the account, its deployments, and its role assignments
```

## Testing

```bash
go test ./...           # Go kernel, against stubbed identity endpoints
cd node && npm test     # Node kernel, same cases
```

Neither suite needs a key, an identity, or a network.

<h1 align="center">Forge</h1>

<p align="center">
  Authentication for Azure OpenAI, as a library.<br>
  You give it a credential; it resolves, caches, refreshes, and attaches it.
</p>

<p align="center">
  <code>go/</code> · <code>node/</code> — same API, both runtimes
</p>

---

It holds no secrets and runs no service. You import it and call it in-process.

<table>
<tr><th align="left" width="50%">Go</th><th align="left" width="50%">Node</th></tr>
<tr valign="top"><td>

```bash
go get github.com/sah-rohan/forge/go@v0.2.0
```

</td><td>

```bash
npm install @sah-rohan/forge
```

</td></tr>
</table>

Both are private, so each needs credentials once per machine.

<details>
<summary><b>Go setup</b></summary>

```bash
go env -w GOPRIVATE=github.com/sah-rohan/*
git config --global url."git@github.com:".insteadOf https://github.com/
```
</details>

<details>
<summary><b>Node setup</b> — a GitHub token with <code>read:packages</code></summary>

In your `.npmrc`:

```
@sah-rohan:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```
</details>

## Use

An authenticated HTTP client. That's the whole surface.

<table>
<tr><th align="left" width="50%">Go</th><th align="left" width="50%">Node</th></tr>
<tr valign="top"><td>

```go
import "github.com/sah-rohan/forge/go"

client := forge.Client(forge.Default())

resp, err := client.Post(url,
    "application/json", body)
```

</td><td>

```ts
import { authorizedFetch, defaultCredential }
  from "@sah-rohan/forge";

const fetch = authorizedFetch(defaultCredential());

const res = await fetch(url,
  { method: "POST", body });
```

</td></tr>
<tr valign="top"><td>

Hand `client` to anything taking an `*http.Client`.

</td><td>

`authorizedFetch` is a drop-in `fetch` — pass it to
the `openai` package's `fetch` option.

</td></tr>
</table>

`Default()` / `defaultCredential()` is a per-process singleton. Call it
anywhere; you get the same cached credential.

Or take the headers directly:

<table>
<tr valign="top"><td width="50%">

```go
h, err := forge.Default().Headers(ctx)
```

</td><td width="50%">

```ts
const h = await defaultCredential().headers();
```

</td></tr>
</table>

## Configure

Set nothing and it uses the managed identity of whatever it's running on.
To override:

| Variable | |
|---|---|
| `AZURE_OPENAI_KEY_FILE` | Key on disk, re-read as it rotates |
| `AZURE_OPENAI_KEY` | Key held inline |
| `AZURE_OPENAI_SCOPE` | Overrides the default Entra ID scope |

Resolved in that order. Or pick a credential explicitly:

| | Go | Node |
|---|---|---|
| Managed identity | `EntraID("")` | `entraID()` |
| Key on disk | `KeyFile(path)` | `keyFile(path)` |
| Key in hand | `StaticKey(k)` | `staticKey(k)` |
| Something else | `FromTokenProvider(fn)` | `fromTokenProvider(fn)` |

## Repo

```
go/       the Go package
node/     the Node package, same API
infra/    the Azure account and its role assignments
```

**Test:** `cd go && go test ./...` · `cd node && npm test`
Neither needs a key, an identity, or a network.

**Release:** Go resolves from a subdirectory tag, npm from CI.

```bash
git tag v0.2.0 && git tag go/v0.2.0
git push origin v0.2.0 go/v0.2.0
```

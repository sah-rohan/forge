# `@sah-rohan/forge`

The Node half of [Forge](../README.md) — an authentication kernel for Azure
OpenAI. It resolves, caches, refreshes, and attaches credentials, and does
nothing else.

A library, not a service. It contains no credentials; you supply one.

## Install

```bash
npm install @sah-rohan/forge
```

Published privately to GitHub Packages. In the consumer's `.npmrc`:

```
@sah-rohan:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

## Use

```ts
import { authorizedFetch, defaultCredential } from "@sah-rohan/forge";

const fetch = authorizedFetch(defaultCredential());

const res = await fetch(
  `${process.env.AZURE_OPENAI_ENDPOINT}/openai/deployments/gpt-5/chat/completions?api-version=2024-10-21`,
  {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ messages: [{ role: "user", content: "hello" }] }),
  },
);
```

`authorizedFetch` returns a drop-in `fetch`, so it goes straight into any client
that accepts one:

```ts
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: `${endpoint}/openai/deployments/gpt-5`,
  apiKey: "unused",
  fetch: authorizedFetch(defaultCredential()),
});
```

Or take the headers yourself:

```ts
const headers = await defaultCredential().headers();
```

## API

| | |
|---|---|
| `defaultCredential()` | The process-wide credential. One token cache for the whole app — use this. |
| `fromEnv(env?)` | Builds an independent credential from the environment. |
| `entraID(scope?, opts?)` | Managed identity, workload identity, or service principal. |
| `keyFile(path)` | Account key on disk, re-read as it rotates. |
| `staticKey(key)` | Account key held in memory. |
| `fromTokenProvider(fn)` | Wraps `@azure/identity` or any other token source. |
| `authorizedFetch(cred, base?)` | A `fetch` that authenticates and reauthenticates on 401. |
| `resetDefaultCredential()` | Clears the singleton. For tests. |

Every credential implements one interface:

```ts
interface Credential {
  headers(): Promise<Record<string, string>>;
  invalidate?(): void;
}
```

`headers()` is called on every request, which is what lets an expiring token or
a rotating key work without a restart. `invalidate()` exists on the caching
credentials and is what `authorizedFetch` calls after a 401.

## Configuration

`AZURE_OPENAI_KEY_FILE`, then `AZURE_OPENAI_KEY`, then Entra ID. Setting none of
them is the intended production configuration. See the
[root README](../README.md#configuration).

## Development

```bash
npm run build      # tsc
npm test           # build, then node --test
npm run typecheck
```

Zero runtime dependencies; `typescript` and `@types/node` are dev-only. The test
suite needs no key, no identity, and no network.

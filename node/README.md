# @sah-rohan/forge

The Forge model kernel for Node — Azure OpenAI behind named modes, with tools,
agents, structured output, and streaming.

Not a service. You import it and call it in-process, so the only network request
in a run is the one to the model. The identical API exists for Go in this repo's
root package, so a TypeScript service and a Go service run the same prompts
against the same modes.

## Install

Published privately to GitHub Packages. In your `.npmrc`:

```
@sah-rohan:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

```bash
npm install @sah-rohan/forge
```

Requires Node 20+ (for `fetch`, `AbortSignal.any`, and `AbortSignal.timeout`).

## Use

```ts
import { Kernel, user, schemaOf, tool, FAST, DEEP } from "@sah-rohan/forge";

const kernel = Kernel.fromEnv();

// One call.
const res = await kernel.complete({
  mode: FAST,
  system: "You summarize in one sentence.",
  messages: [user(text)],
});

// Structured output, typed.
const plan = await kernel.json<StudyPlan>({
  mode: DEEP,
  messages: [user(history)],
  schema: schemaOf("study_plan", planSchema),
});

// An agent with tools.
const search = tool<{ query: string }>({
  name: "search",
  description: "Search the docs.",
  parameters: {
    type: "object",
    properties: { query: { type: "string" } },
    required: ["query"],
  },
  handler: async ({ query }) => index.search(query),
});

const out = await kernel.run(
  { system: "Answer from the docs. Cite what you used.", tools: [search] },
  [user(question)],
);
console.log(out.text, out.usage);

// Streaming.
await kernel.stream({ messages: [user(q)] }, (chunk) => {
  if (chunk.text) response.write(chunk.text);
});
```

A tool with no `handler` is deliberate: the run stops and returns the call in
`result.pending` for you to execute — how a confirmation prompt or a
client-side action works.

## Configuration

`Kernel.fromEnv()` reads:

| Variable | |
|---|---|
| `AZURE_OPENAI_ENDPOINT` | required |
| `AZURE_OPENAI_KEY` | required |
| `AZURE_OPENAI_API_VERSION` | optional |
| `FORGE_MODE_FAST` / `_BALANCED` / `_DEEP` | deployment name per mode |
| `FORGE_DEFAULT_MODE` | optional |

Any `FORGE_MODE_<NAME>` defines a mode, so custom modes need no code change.
Pass a `Config` to the constructor instead when you load settings yourself.

## Develop

```bash
npm run typecheck
npm test          # 26 cases against a stub fetch; no key, no network
npm run build
```

Full documentation is in the [repo README](../README.md).

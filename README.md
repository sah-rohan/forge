# Forge

**A model kernel.** One small library that activates the Azure OpenAI API and
gives every consumer the same way to run AI — modes, tools, agents, structured
output.

Forge is **not a service**. You import it and call it in-process, so the only
network request in a run is the one to the model. It ships twice, with the same
API in both, because consumers are split across two runtimes:

| | Import | Source |
|---|---|---|
| **Go** | `github.com/sah-rohan/forge` | repo root |
| **Node** | `@sah-rohan/forge` | [`node/`](node/) |
| **Terraform** | `modules/openai` | [`infra/modules/openai/`](infra/modules/openai/) |

Nothing about a consumer is compiled into Forge. Agents, retrieval, prompts, and
chains are ordinary code you write in *your* repo against this interface. The
kernel stays opinion-free.

## Three ideas

**Modes.** You ask for `fast`, `balanced`, or `deep` — a class of model, not a
deployment name. Which model serves each mode is configuration, so upgrading a
model never touches a call site. Both kernels discover modes from `FORGE_MODE_*`
environment variables at startup, so adding one is an infra change and nothing
else.

**Tools.** Declare functions the model may call. `Complete` hands back the calls
it wants and executes nothing; `Run` executes them and loops until the model has
an answer. A tool with no handler is deliberate — the run stops and hands the
call to you, which is how a confirmation prompt or a client-side action works.

**Structured output.** Give it a JSON Schema and decode straight into your own
type. Nothing parses prose.

## Go

```bash
go get github.com/sah-rohan/forge
```

The repo is private, and Go needs no registry — `go get` reads the git repo
directly:

```bash
go env -w GOPRIVATE=github.com/sah-rohan/*
git config --global url."git@github.com:".insteadOf https://github.com/
```

`GOPRIVATE` also bypasses the public proxy and checksum database, so the code
never leaves your network.

```go
k, err := forge.FromEnv()

// One call.
res, err := k.Complete(ctx, forge.Request{
    Mode:     forge.ModeFast,
    System:   "You summarize in one sentence.",
    Messages: []forge.Message{forge.User(text)},
})
fmt.Println(res.Text)

// Straight into a struct.
var plan StudyPlan
err = k.JSON(ctx, forge.Request{
    Mode:     forge.ModeDeep,
    System:   "Build a study plan.",
    Messages: []forge.Message{forge.User(history)},
    Schema:   forge.SchemaOf("study_plan", planSchema),
}, &plan)

// An agent with tools.
search := forge.NewTool("search", "Search the docs.", searchSchema,
    func(ctx context.Context, a struct{ Query string }) (string, error) {
        return index.Search(ctx, a.Query)
    })

out, err := k.Run(ctx, forge.Agent{
    System: "Answer from the docs. Cite what you used.",
    Tools:  []forge.Tool{search},
}, forge.User(question))
fmt.Println(out.Text, out.Usage)

// Streaming.
_, err = k.Stream(ctx, req, func(c forge.Chunk) error {
    io.WriteString(w, c.Text)
    return nil
})
```

## Node

Published privately to GitHub Packages. In the consumer's `.npmrc`:

```
@sah-rohan:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

```bash
npm install @sah-rohan/forge
```

```ts
import { Kernel, user, schemaOf, FAST, DEEP } from "@sah-rohan/forge";

const kernel = Kernel.fromEnv();

const res = await kernel.complete({
  mode: FAST,
  system: "You summarize in one sentence.",
  messages: [user(text)],
});

const plan = await kernel.json<StudyPlan>({
  mode: DEEP,
  messages: [user(history)],
  schema: schemaOf("study_plan", planSchema),
});

const out = await kernel.run(
  { system: "Answer from the docs.", tools: [search] },
  [user(question)],
);

await kernel.stream({ messages: [user(q)] }, (c) => res.write(c.text));
```

## Configuration

Both kernels read the same environment, so one set of app settings configures
either:

| Variable | |
|---|---|
| `AZURE_OPENAI_ENDPOINT` | required — `https://<resource>.openai.azure.com` |
| `AZURE_OPENAI_KEY` | required |
| `AZURE_OPENAI_API_VERSION` | optional |
| `FORGE_MODE_FAST` | deployment name serving `fast` |
| `FORGE_MODE_BALANCED` | deployment name serving `balanced` |
| `FORGE_MODE_DEEP` | deployment name serving `deep` |
| `FORGE_DEFAULT_MODE` | optional; defaults to `balanced` when configured |

Any `FORGE_MODE_<NAME>` defines a mode, so custom modes need no code change.
`AZURE_OPENAI_DEPLOYMENT` alone points all three standard modes at one
deployment — enough to start, split later.

`infra/` emits exactly these as a `forge_env` output. See
[infra/README.md](infra/README.md).

## What you get for free

- **Retries** on 429s, 5xx, and connection failures — honoring `Retry-After`,
  otherwise exponential backoff with jitter. A stream that already emitted
  tokens is never retried.
- **Typed errors** carrying Azure's own code, so you branch on `content_filter`
  or `context_length_exceeded` without matching strings.
- **Usage accounting** on every call, including cached and reasoning tokens,
  with an `OnUsage` hook for cost tracking and budgets.
- **Reassembled streaming tool calls** — arguments arrive fragmented across SSE
  frames and are joined before you see them.
- **Zero dependencies** in Go; only `typescript`/`@types/node` as dev
  dependencies in Node.

## Layout

```
forge.go, types.go, azure.go, agent.go   the Go kernel
node/src/                                the Node kernel (same API)
infra/                                   Azure OpenAI + one deployment per mode
```

`azure.go` and `node/src/azure.ts` are the only files that know the vendor's
wire format. A second provider means one more file each — not a change to
agents, tools, or call sites.

## Testing

```bash
go test ./...           # Go kernel, against a stub Azure endpoint
cd node && npm test     # Node kernel, same cases
```

Neither suite needs an API key or a network.

# Forge

**A context-driven AI agent kernel.** One engine, many agents: give it context
(a code repo, a resume, a LeetCode history, a job posting), it reasons with an
LLM, and returns **typed, frontend-renderable output**. Consumers (the career
app, Kronos, internal automation) call one uniform API and render the result by
`kind`.

The design goal is FoundationDB-style versatility: **nothing about a consumer
is compiled into Forge.** Context sources, the model provider, and output
shapes are all pluggable.

## The three seams

```
context  ──►  model  ──►  render
 (what the    (Claude /    (typed output:
  agent       Azure AI,     a discriminated
  knows)      swappable)    union the UI maps
                            to components)
```

- **`kernel/model`** — `Model` interface with `Anthropic` + `AzureOpenAI`
  implementations. Chosen per deploy via `FORGE_MODEL_PROVIDER`; agents never
  see a vendor.
- **`kernel/render`** — every agent returns `Output{ kind, data }`. The TS SDK
  mirrors these as a discriminated union so the frontend renders one component
  per kind and never parses prose.
- **`kernel`** — the `Agent` interface + `Registry`. An agent owns its context
  adapter, prompt, and output schema. Adding one is a single `Register` call.

## Layout

```
kernel/            the reusable engine (the IP)
  model/           LLM providers behind one interface
  render/          typed output schemas
  server/          POST /v1/agents/{name}/run  — the uniform API
agents/            opinionated agents built on the kernel
  resume/          resume-grill: resume -> interviewer questions (typed)
  prbot/           (planned) GitHub PR agent — see internal/ webhook plumbing
cmd/
  api/             kernel HTTP API server  (the career app / Kronos call this)
  server/          GitHub App webhook receiver (the PR agent's event entrypoint)
sdk/ts/            TypeScript client for React apps
internal/          PR-agent plumbing: GitHub App auth, sandbox clone, profiler
```

Two entrypoints share one kernel:
- **`cmd/api`** — request/response agents (resume-grill, future skill-graph,
  mock-interview). This is what frontends call.
- **`cmd/server`** — the GitHub PR agent, which is event-driven (webhook), not
  request/response. Both are model-bound, so the network hop between a consumer
  and Forge is <0.1% of total latency — agents are split by *coupling*, never
  by milliseconds.

## Run the kernel API locally

```bash
export FORGE_MODEL_PROVIDER=anthropic      # or "azure"
export ANTHROPIC_API_KEY=sk-ant-...        # for anthropic
# export AZURE_OPENAI_ENDPOINT=... AZURE_OPENAI_DEPLOYMENT=... AZURE_OPENAI_KEY=...
go run ./cmd/api                            # :8090

curl -s localhost:8090/v1/agents/resume-grill/run \
  -H 'content-type: application/json' \
  -d '{"context":{"resume":"Founding engineer at ISF. Built a Redis→Postgres translation cache with placeholder protection; hardened Stripe webhooks (signature + idempotency); shipped Terraform infra on Azure.","target_role":"Backend SWE"}}' | jq
```

Returns `{ "kind": "resume_grill", "data": { summary, probes[...] } }` — each
probe tied to a resume line, with the question an interviewer would ask, what
it tests, and a model answer.

## Call it from React

```ts
import { ForgeClient } from "@forge/sdk";

const forge = new ForgeClient({ baseUrl: import.meta.env.VITE_FORGE_URL, apiKey });
const grill = await forge.resumeGrill({ resume, target_role: "Backend SWE, Stripe" });
// grill.probes -> render <ProbeCard> for each
```

## Roadmap

- [x] Kernel seams (model / render / agent registry)
- [x] Resume-grill agent + typed output + TS SDK
- [x] GitHub App plumbing (auth, sandbox clone, zero-assumption repo profiler)
- [ ] PR agent: context engine (RAG + past-PR mining) → verified draft PRs
- [ ] Skill-graph agent (LeetCode history → mastery map)
- [ ] Calibrated mock-interview agent (adaptive difficulty / IRT)
- [ ] Behavioral agent (STAR/CARL critique from real project context)
- [ ] Budget governor (per-run cost caps, idempotency)
- [ ] React component library for each output kind

## Testing

```bash
go test ./...     # agents tested with a fake model — no API key needed
```

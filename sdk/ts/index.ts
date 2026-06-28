// Forge TypeScript SDK — the thin client React apps (career app, Kronos) use to
// call the kernel API. Every agent returns a typed AgentOutput discriminated by
// `kind`; the frontend renders one component per kind. Add a kind here when you
// add an agent, and the compiler tells every consumer what to render.

export interface ForgeClientOptions {
  /** Base URL of the Forge kernel API, e.g. https://forge.example.com */
  baseUrl: string;
  /** Sent as X-Forge-Key. */
  apiKey?: string;
  fetchImpl?: typeof fetch;
}

// ---- Output types (mirror kernel/render) ----

export interface Probe {
  from_line: string;
  question: string;
  why: string;
  model_answer: string;
  category: "project" | "system_design" | "coding" | "behavioral";
  difficulty: number;
}

export interface ResumeGrill {
  summary: string;
  probes: Probe[];
}

// The discriminated union the UI switches on. Extend as agents are added.
export type AgentOutput =
  | { kind: "resume_grill"; data: ResumeGrill }
  | { kind: "error"; data: { message: string } }
  // forward-declared kinds (typed `unknown` until their schemas land):
  | { kind: "skill_graph"; data: unknown }
  | { kind: "problem_recs"; data: unknown }
  | { kind: "mock_interview"; data: unknown }
  | { kind: "behavioral_feedback"; data: unknown }
  | { kind: "pr_plan"; data: unknown };

export class ForgeClient {
  private baseUrl: string;
  private apiKey?: string;
  private fetchImpl: typeof fetch;

  constructor(opts: ForgeClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/$/, "");
    this.apiKey = opts.apiKey;
    this.fetchImpl = opts.fetchImpl ?? fetch;
  }

  /** List available agent names. */
  async agents(): Promise<string[]> {
    const res = await this.fetchImpl(`${this.baseUrl}/v1/agents`, {
      headers: this.headers(),
    });
    if (!res.ok) throw new Error(`forge agents: ${res.status}`);
    return (await res.json()).agents;
  }

  /**
   * Run an agent. `context` is the agent-specific input; `options` are run
   * knobs. Returns the typed, kind-discriminated output ready to render.
   */
  async run(
    agent: string,
    context: unknown,
    options?: Record<string, unknown>,
  ): Promise<AgentOutput> {
    const res = await this.fetchImpl(
      `${this.baseUrl}/v1/agents/${encodeURIComponent(agent)}/run`,
      {
        method: "POST",
        headers: { ...this.headers(), "content-type": "application/json" },
        body: JSON.stringify({ context, options }),
      },
    );
    const body = (await res.json()) as AgentOutput;
    // The server returns kind:"error" with a 502 on agent failure; surface it
    // as a normal typed result so callers can render an error component.
    return body;
  }

  /** Convenience wrapper for the resume-grill agent. */
  async resumeGrill(input: {
    resume: string;
    target_role?: string;
    focus?: "system_design" | "coding" | "behavioral";
  }): Promise<ResumeGrill> {
    const out = await this.run("resume-grill", input);
    if (out.kind !== "resume_grill") {
      throw new Error(
        out.kind === "error"
          ? (out.data as { message: string }).message
          : `unexpected kind ${out.kind}`,
      );
    }
    return out.data;
  }

  private headers(): Record<string, string> {
    return this.apiKey ? { "X-Forge-Key": this.apiKey } : {};
  }
}

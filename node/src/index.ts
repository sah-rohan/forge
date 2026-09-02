/**
 * Forge — a model kernel for Node.
 *
 * One small library that activates the Azure OpenAI API and gives every
 * consumer the same way to run AI. It is not a service: you import it and call
 * it in-process, so the only network request in a run is the one to the model.
 *
 * Three ideas carry the whole package:
 *
 * - **Modes.** You ask for "fast", "balanced", or "deep" instead of naming a
 *   deployment. Which model serves each mode is configuration, so upgrading a
 *   model never touches a call site.
 * - **Tools.** Declare functions the model may call. `complete` hands back the
 *   calls it wants; `run` executes them and loops.
 * - **Structured output.** Hand it a JSON Schema and get a typed value back —
 *   no prose parsing anywhere.
 *
 * Everything above them (agents, retrieval, chains) is ordinary code you write
 * in your own repo against this interface. The kernel stays opinion-free.
 *
 * The identical API exists for Go in this repo's root package, so a TypeScript
 * service and a Go service run the same prompts against the same modes.
 *
 * ```ts
 * import { Kernel, user, FAST } from "@sah-rohan/forge";
 *
 * const kernel = Kernel.fromEnv();
 * const res = await kernel.complete({
 *   mode: FAST,
 *   system: "You summarize in one sentence.",
 *   messages: [user(text)],
 * });
 * console.log(res.text);
 * ```
 *
 * @packageDocumentation
 */

export { Kernel, DEFAULT_API_VERSION, type Config } from "./kernel.js";

export {
  runAgent,
  tool,
  DEFAULT_MAX_TURNS,
  type Agent,
  type Completer,
  type Result,
  type Step,
  type ToolResult,
} from "./agent.js";

export {
  BALANCED,
  DEEP,
  FAST,
  ForgeError,
  addUsage,
  assistant,
  emptyUsage,
  isRetryable,
  responseMessage,
  schemaOf,
  toolArgs,
  user,
  type Chunk,
  type Message,
  type Mode,
  type Request,
  type Response,
  type Role,
  type Schema,
  type Tool,
  type ToolCall,
  type Usage,
} from "./types.js";

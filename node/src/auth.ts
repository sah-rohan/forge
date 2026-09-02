import { readFile } from "node:fs/promises";

export const DEFAULT_SCOPE = "https://cognitiveservices.azure.com/.default";

const DEFAULT_TOKEN_LIFETIME_MS = 55 * 60 * 1000;
const REFRESH_SKEW_MS = 5 * 60 * 1000;
const TOKEN_TIMEOUT_MS = 10_000;

export interface Credential {
  headers(): Promise<Record<string, string>>;
  invalidate?(): void;
}

export function staticKey(key: string): Credential {
  const headers = { "api-key": key };
  return {
    async headers() {
      if (!key) throw new Error("empty api key");
      return headers;
    },
  };
}

export function keyFile(path: string): Credential {
  return {
    async headers() {
      let raw: string;
      try {
        raw = await readFile(path, "utf8");
      } catch (e) {
        throw new Error(`read key file ${path}: ${(e as Error).message}`);
      }
      const key = raw.trim();
      if (!key) throw new Error(`key file ${path} is empty`);
      return { "api-key": key };
    },
  };
}

interface Bearer {
  value: string;
  expiresAt: number;
  headers: Record<string, string>;
}

function bearer(value: string, expiresAt: number): Bearer {
  return { value, expiresAt, headers: { authorization: `Bearer ${value}` } };
}

type TokenFetch = (signal: AbortSignal) => Promise<Bearer>;

class TokenCredential implements Credential {
  private token?: Bearer;
  private inflight?: Promise<Bearer>;

  constructor(
    private readonly fetchToken: TokenFetch,
    private readonly how: string,
  ) {}

  async headers(): Promise<Record<string, string>> {
    const cached = this.token;
    if (cached && cached.expiresAt - REFRESH_SKEW_MS > Date.now()) {
      return cached.headers;
    }

    this.inflight ??= this.fetchToken(AbortSignal.timeout(TOKEN_TIMEOUT_MS)).finally(() => {
      this.inflight = undefined;
    });

    let token: Bearer;
    try {
      token = await this.inflight;
    } catch (e) {
      throw new Error(`acquire token via ${this.how}: ${(e as Error).message}`);
    }
    this.token = token;
    return token.headers;
  }

  invalidate(): void {
    this.token = undefined;
  }
}

export interface EntraOptions {
  env?: Record<string, string | undefined>;
  fetch?: typeof globalThis.fetch;
}

export function entraID(scope: string = DEFAULT_SCOPE, opts: EntraOptions = {}): Credential {
  const env = opts.env ?? process.env;
  const doFetch = opts.fetch ?? globalThis.fetch;
  const [fetchToken, how] = entraFlow(scope || DEFAULT_SCOPE, env, doFetch);
  return new TokenCredential(fetchToken, how);
}

export function fromTokenProvider(
  fn: (signal?: AbortSignal) => Promise<{ token: string; expiresOnTimestamp?: number }>,
): Credential {
  return new TokenCredential(async (signal) => {
    const { token, expiresOnTimestamp } = await fn(signal);
    if (!token) throw new Error("token provider returned an empty token");
    return bearer(token, expiresOnTimestamp || Date.now() + DEFAULT_TOKEN_LIFETIME_MS);
  }, "token provider");
}

function entraFlow(
  scope: string,
  env: Record<string, string | undefined>,
  doFetch: typeof globalThis.fetch,
): [TokenFetch, string] {
  const tenant = env.AZURE_TENANT_ID ?? "";
  const clientID = env.AZURE_CLIENT_ID ?? "";

  if (env.AZURE_FEDERATED_TOKEN_FILE) {
    const file = env.AZURE_FEDERATED_TOKEN_FILE;
    return [
      async (signal) => {
        let assertion: string;
        try {
          assertion = (await readFile(file, "utf8")).trim();
        } catch (e) {
          throw new Error(`read federated token file ${file}: ${(e as Error).message}`);
        }
        return entraToken(doFetch, signal, tenant, {
          grant_type: "client_credentials",
          scope,
          client_id: clientID,
          client_assertion_type: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
          client_assertion: assertion,
        });
      },
      "workload identity federation",
    ];
  }

  if (env.AZURE_CLIENT_SECRET) {
    const secret = env.AZURE_CLIENT_SECRET;
    return [
      (signal) =>
        entraToken(doFetch, signal, tenant, {
          grant_type: "client_credentials",
          scope,
          client_id: clientID,
          client_secret: secret,
        }),
      "service principal",
    ];
  }

  if (env.IDENTITY_ENDPOINT) {
    return [
      (signal) => appServiceToken(doFetch, signal, env, resourceOf(scope), clientID),
      "App Service managed identity",
    ];
  }

  return [
    (signal) => imdsToken(doFetch, signal, resourceOf(scope), clientID),
    "IMDS managed identity",
  ];
}

export function resourceOf(scope: string): string {
  return scope.endsWith("/.default") ? scope.slice(0, -"/.default".length) : scope;
}

async function entraToken(
  doFetch: typeof globalThis.fetch,
  signal: AbortSignal,
  tenant: string,
  form: Record<string, string>,
): Promise<Bearer> {
  if (!tenant) throw new Error("AZURE_TENANT_ID is not set");
  if (!form.client_id) throw new Error("AZURE_CLIENT_ID is not set");

  const res = await doFetch(
    `https://login.microsoftonline.com/${encodeURIComponent(tenant)}/oauth2/v2.0/token`,
    {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams(form).toString(),
      signal,
    },
  );
  return decodeToken(res);
}

async function imdsToken(
  doFetch: typeof globalThis.fetch,
  signal: AbortSignal,
  resource: string,
  clientID: string,
): Promise<Bearer> {
  const q = new URLSearchParams({ "api-version": "2018-02-01", resource });
  if (clientID) q.set("client_id", clientID);

  try {
    const res = await doFetch(`http://169.254.169.254/metadata/identity/oauth2/token?${q}`, {
      headers: { Metadata: "true" },
      signal,
    });
    return await decodeToken(res);
  } catch (e) {
    throw new Error(
      `${(e as Error).message} — no managed identity is reachable here; ` +
        "set AZURE_OPENAI_KEY (or AZURE_OPENAI_KEY_FILE) for local development",
    );
  }
}

async function appServiceToken(
  doFetch: typeof globalThis.fetch,
  signal: AbortSignal,
  env: Record<string, string | undefined>,
  resource: string,
  clientID: string,
): Promise<Bearer> {
  const header = env.IDENTITY_HEADER;
  if (!header) throw new Error("IDENTITY_ENDPOINT is set but IDENTITY_HEADER is not");

  const q = new URLSearchParams({ "api-version": "2019-08-01", resource });
  if (clientID) q.set("client_id", clientID);

  const res = await doFetch(`${env.IDENTITY_ENDPOINT}?${q}`, {
    headers: { "X-IDENTITY-HEADER": header },
    signal,
  });
  return decodeToken(res);
}

async function decodeToken(res: globalThis.Response): Promise<Bearer> {
  const raw = await res.text();
  let body: Record<string, unknown> = {};
  try {
    body = JSON.parse(raw) as Record<string, unknown>;
  } catch {
    if (res.ok) throw new Error(`decode token response: ${raw.slice(0, 200)}`);
  }

  if (!res.ok) {
    throw new Error(
      `identity endpoint returned ${res.status}: ${String(body.error_description ?? body.error ?? "no detail")}`,
    );
  }

  const token = body.access_token;
  if (typeof token !== "string" || !token) {
    throw new Error("identity endpoint returned no access_token");
  }

  const expiresIn = numeric(body.expires_in);
  if (expiresIn) return bearer(token, Date.now() + expiresIn * 1000);

  const expiresOn = numeric(body.expires_on);
  if (expiresOn) return bearer(token, expiresOn * 1000);

  return bearer(token, Date.now() + DEFAULT_TOKEN_LIFETIME_MS);
}

function numeric(v: unknown): number {
  const n = typeof v === "number" ? v : typeof v === "string" ? Number(v) : NaN;
  return Number.isFinite(n) && n > 0 ? n : 0;
}

export function fromEnv(env: Record<string, string | undefined> = process.env): Credential {
  if (env.AZURE_OPENAI_KEY_FILE) return keyFile(env.AZURE_OPENAI_KEY_FILE);
  if (env.AZURE_OPENAI_KEY) return staticKey(env.AZURE_OPENAI_KEY);
  return entraID(env.AZURE_OPENAI_SCOPE ?? DEFAULT_SCOPE, { env });
}

let shared: Credential | undefined;

export function defaultCredential(): Credential {
  return (shared ??= fromEnv());
}

export function resetDefaultCredential(): void {
  shared = undefined;
}

export function authorizedFetch(
  credential: Credential,
  base: typeof globalThis.fetch = globalThis.fetch,
): typeof globalThis.fetch {
  return async (input, init) => {
    const send = async () => {
      const headers = new Headers(init?.headers as HeadersInit);
      for (const [name, value] of Object.entries(await credential.headers())) {
        headers.set(name, value);
      }
      return base(input, { ...init, headers });
    };

    const res = await send();
    if (res.status !== 401 || !credential.invalidate || !replayable(init?.body)) return res;

    credential.invalidate();
    return send();
  };
}

function replayable(body: BodyInit | null | undefined): boolean {
  return !(body instanceof ReadableStream);
}

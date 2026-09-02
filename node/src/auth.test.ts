import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, it } from "node:test";

import {
  Credential,
  DEFAULT_SCOPE,
  authorizedFetch,
  defaultCredential,
  entraID,
  fromEnv,
  fromTokenProvider,
  keyFile,
  resetDefaultCredential,
  resourceOf,
  staticKey,
} from "./auth.js";

const jsonResponse = (payload: unknown, status = 200) =>
  new globalThis.Response(JSON.stringify(payload), {
    status,
    headers: { "content-type": "application/json" },
  });

function stub(credential: Credential, ...statuses: number[]) {
  const auth: string[] = [];
  const bodies: string[] = [];
  let calls = 0;

  const base: typeof globalThis.fetch = async (_input, init) => {
    const headers = new Headers(init?.headers as HeadersInit);
    auth.push(
      headers.get("authorization")
        ? `authorization: ${headers.get("authorization")}`
        : `api-key: ${headers.get("api-key")}`,
    );
    bodies.push(String(init?.body ?? ""));

    const n = calls++;
    if (n >= statuses.length) throw new Error(`stub got ${n + 1} calls, only ${statuses.length} configured`);
    return new globalThis.Response("{}", { status: statuses[n] });
  };

  return { fetch: authorizedFetch(credential, base), auth, bodies };
}

const post = (f: typeof globalThis.fetch) =>
  f("https://stub.openai.azure.com/", { method: "POST", body: '{"hello":"world"}' });

function rotating(): Credential {
  let n = 0;
  return {
    async headers() {
      return { "api-key": `key-${++n}` };
    },
  };
}

function countingTokens(): Credential {
  let n = 0;
  return fromTokenProvider(async () => ({
    token: `token-${++n}`,
    expiresOnTimestamp: Date.now() + 3_600_000,
  }));
}

describe("authorizedFetch", () => {
  it("asks the credential on every request", async () => {
    const s = stub(rotating(), 200, 200);
    await post(s.fetch);
    await post(s.fetch);
    assert.deepEqual(s.auth, ["api-key: key-1", "api-key: key-2"]);
  });

  it("retries once with a fresh token after a 401", async () => {
    const s = stub(countingTokens(), 401, 200);
    const res = await post(s.fetch);
    assert.equal(res.status, 200);
    assert.deepEqual(s.auth, ["authorization: Bearer token-1", "authorization: Bearer token-2"]);
  });

  it("resends the body on the retry", async () => {
    const s = stub(countingTokens(), 401, 200);
    await post(s.fetch);
    assert.deepEqual(s.bodies, ['{"hello":"world"}', '{"hello":"world"}']);
  });

  it("retries a 401 only once", async () => {
    const s = stub(countingTokens(), 401, 401);
    const res = await post(s.fetch);
    assert.equal(res.status, 401);
    assert.equal(s.auth.length, 2);
  });

  it("does not retry when the credential has nothing to invalidate", async () => {
    const s = stub(staticKey("k"), 401);
    const res = await post(s.fetch);
    assert.equal(res.status, 401);
    assert.equal(s.auth.length, 1);
  });

  it("does not retry a streamed body", async () => {
    const s = stub(countingTokens(), 401);
    const res = await s.fetch("https://stub.openai.azure.com/", {
      method: "POST",
      body: new ReadableStream(),
      // @ts-expect-error duplex is required for a stream body but absent from the DOM types
      duplex: "half",
    });
    assert.equal(res.status, 401);
    assert.equal(s.auth.length, 1);
  });

  it("surfaces a credential failure", async () => {
    const broken: Credential = {
      async headers() {
        throw new Error("identity endpoint unreachable");
      },
    };
    const s = stub(broken);
    await assert.rejects(() => post(s.fetch), /identity endpoint unreachable/);
  });
});

describe("key credentials", () => {
  it("sets the api-key header", async () => {
    assert.equal((await staticKey("abc").headers())["api-key"], "abc");
  });

  it("rejects an empty key", async () => {
    await assert.rejects(() => staticKey("").headers());
  });

  it("re-reads the key file on every request, picking up a rotated key", async () => {
    const path = join(await mkdtemp(join(tmpdir(), "forge-")), "key");
    await writeFile(path, "first\n");

    const s = stub(keyFile(path), 200, 200);
    await post(s.fetch);
    await writeFile(path, "second");
    await post(s.fetch);

    assert.deepEqual(s.auth, ["api-key: first", "api-key: second"]);
  });

  it("errors on a missing key file", async () => {
    await assert.rejects(() => keyFile("/nonexistent/key").headers());
  });

  it("errors on an empty key file", async () => {
    const path = join(await mkdtemp(join(tmpdir(), "forge-")), "key");
    await writeFile(path, "   \n");
    await assert.rejects(() => keyFile(path).headers(), /empty/);
  });
});

describe("token caching", () => {
  it("caches a token across requests", async () => {
    let fetches = 0;
    const cred = fromTokenProvider(async () => {
      fetches++;
      return { token: "tok", expiresOnTimestamp: Date.now() + 3_600_000 };
    });

    for (let i = 0; i < 5; i++) await cred.headers();
    assert.equal(fetches, 1);
  });

  it("refreshes a token inside the refresh skew", async () => {
    let fetches = 0;
    const cred = fromTokenProvider(async () => {
      fetches++;
      return { token: "tok", expiresOnTimestamp: Date.now() + 120_000 };
    });

    for (let i = 0; i < 3; i++) await cred.headers();
    assert.equal(fetches, 3);
  });

  it("coalesces concurrent refreshes into one acquisition", async () => {
    let fetches = 0;
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));

    const cred = fromTokenProvider(async () => {
      fetches++;
      await gate;
      return { token: "tok", expiresOnTimestamp: Date.now() + 3_600_000 };
    });

    const all = Promise.all(Array.from({ length: 20 }, () => cred.headers()));
    release();
    await all;

    assert.equal(fetches, 1);
  });

  it("refetches after invalidate", async () => {
    const cred = countingTokens();
    const first = await cred.headers();
    cred.invalidate?.();
    const second = await cred.headers();
    assert.notEqual(first.authorization, second.authorization);
  });

  it("defaults the expiry when the provider gives none", async () => {
    let fetches = 0;
    const cred = fromTokenProvider(async () => {
      fetches++;
      return { token: "provided" };
    });

    for (let i = 0; i < 3; i++) {
      assert.equal((await cred.headers()).authorization, "Bearer provided");
    }
    assert.equal(fetches, 1);
  });

  it("names the flow in an acquisition error", async () => {
    const cred = entraID(DEFAULT_SCOPE, {
      env: {},
      fetch: async () => {
        throw new Error("connection refused");
      },
      exec: async () => {
        throw new Error("the Azure CLI is not installed");
      },
    });
    await assert.rejects(() => cred.headers(), /managed identity or the Azure CLI/);
  });
});

describe("identity endpoints", () => {
  const shapes: Array<[string, unknown, number]> = [
    ["expires_in as a number", { access_token: "t", expires_in: 3600 }, 3_600_000],
    ["expires_in quoted", { access_token: "t", expires_in: "3600" }, 3_600_000],
    [
      "expires_on as a quoted unix time",
      { access_token: "t", expires_on: String(Math.floor(Date.now() / 1000) + 3600) },
      3_600_000,
    ],
    ["an unparseable expiry", { access_token: "t", expires_on: "6/1/2026 12:00:00 AM +00:00" }, 55 * 60 * 1000],
    ["no expiry at all", { access_token: "t" }, 55 * 60 * 1000],
  ];

  for (const [name, body, lifetimeMs] of shapes) {
    it(`accepts ${name}`, async () => {
      let fetches = 0;
      const cred = entraID(DEFAULT_SCOPE, {
        env: {},
        fetch: async () => {
          fetches++;
          return jsonResponse(body);
        },
      });

      assert.equal((await cred.headers()).authorization, "Bearer t");
      await cred.headers();
      assert.equal(fetches, lifetimeMs > 5 * 60 * 1000 ? 1 : 2);
    });
  }

  it("carries the endpoint's description into the error", async () => {
    const cred = entraID(DEFAULT_SCOPE, {
      env: { AZURE_CLIENT_SECRET: "s", AZURE_TENANT_ID: "t", AZURE_CLIENT_ID: "c" },
      fetch: async () => jsonResponse({ error: "invalid_client", error_description: "secret is expired" }, 400),
    });
    await assert.rejects(() => cred.headers(), /secret is expired/);
  });

  it("rejects a response with no access_token", async () => {
    const cred = entraID(DEFAULT_SCOPE, {
      env: { AZURE_CLIENT_SECRET: "s", AZURE_TENANT_ID: "t", AZURE_CLIENT_ID: "c" },
      fetch: async () => jsonResponse({ token_type: "Bearer" }),
    });
    await assert.rejects(() => cred.headers(), /no access_token/);
  });

  it("selects the identity flow from the environment", async () => {
    const flows: Array<[Record<string, string>, RegExp]> = [
      [{ AZURE_FEDERATED_TOKEN_FILE: "/var/run/token" }, /workload identity federation/],
      [{ AZURE_CLIENT_SECRET: "s" }, /service principal/],
      [{ IDENTITY_ENDPOINT: "http://localhost/token" }, /App Service managed identity/],
      [{}, /managed identity or the Azure CLI/],
    ];

    for (const [env, want] of flows) {
      const cred = entraID(DEFAULT_SCOPE, {
        env,
        fetch: async () => {
          throw new Error("unreachable");
        },
        exec: async () => {
          throw new Error("the Azure CLI is not installed");
        },
      });
      await assert.rejects(() => cred.headers(), want);
    }
  });

  it("strips /.default for the legacy identity endpoints", () => {
    assert.equal(resourceOf(DEFAULT_SCOPE), "https://cognitiveservices.azure.com");
  });
});

describe("environment resolution and the singleton", () => {
  it("prefers a key file over an inline key", async () => {
    const path = join(await mkdtemp(join(tmpdir(), "forge-")), "key");
    await writeFile(path, "from-file");

    const cred = fromEnv({ AZURE_OPENAI_KEY_FILE: path, AZURE_OPENAI_KEY: "inline" });
    assert.equal((await cred.headers())["api-key"], "from-file");
  });

  it("uses an inline key when no file is set", async () => {
    assert.equal((await fromEnv({ AZURE_OPENAI_KEY: "inline" }).headers())["api-key"], "inline");
  });

  it("falls back to Entra ID when no key is configured", () => {
    assert.equal(typeof fromEnv({}).invalidate, "function");
  });

  it("returns the same credential from defaultCredential", () => {
    resetDefaultCredential();
    process.env.AZURE_OPENAI_KEY = "shared";
    try {
      assert.equal(defaultCredential(), defaultCredential());
      assert.notEqual(defaultCredential(), fromEnv());
    } finally {
      delete process.env.AZURE_OPENAI_KEY;
      resetDefaultCredential();
    }
  });
});

describe("azure cli fallback", () => {
  const cliCredential = (exec: (r: string) => Promise<string>) =>
    entraID(DEFAULT_SCOPE, {
      env: {},
      fetch: async () => {
        throw new Error("connection refused");
      },
      exec,
    });

  it("serves a token from the CLI when IMDS is unreachable", async () => {
    const cred = cliCredential(async () =>
      JSON.stringify({
        accessToken: "cli-token",
        expires_on: Math.floor(Date.now() / 1000) + 3600,
        tokenType: "Bearer",
      }),
    );

    assert.equal((await cred.headers()).authorization, "Bearer cli-token");
  });

  it("passes the resource, not the scope, to the CLI", async () => {
    let seen = "";
    const cred = cliCredential(async (resource) => {
      seen = resource;
      return JSON.stringify({ accessToken: "cli-token" });
    });

    await cred.headers();
    assert.equal(seen, "https://cognitiveservices.azure.com");
  });

  it("defaults the expiry when the CLI gives none", async () => {
    let calls = 0;
    const cred = cliCredential(async () => {
      calls++;
      return JSON.stringify({ accessToken: "cli-token" });
    });

    await cred.headers();
    await cred.headers();
    assert.equal(calls, 1);
  });

  it("explains the options when the CLI is missing", async () => {
    const cred = cliCredential(async () => {
      throw new Error("the Azure CLI is not installed");
    });

    await assert.rejects(() => cred.headers(), /run az login/);
    await assert.rejects(() => cred.headers(), /AZURE_OPENAI_KEY/);
  });

  it("rejects CLI output with no token", async () => {
    const cred = cliCredential(async () => JSON.stringify({ tokenType: "Bearer" }));
    await assert.rejects(() => cred.headers(), /no accessToken/);
  });
});

/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// The generated credential scripts, exercised as programs.
//
// These tests exist because the bug they now guard shipped undetected: the
// script carried two credential protocols and picked between them with
// `[ -n "$1" ]`, which is true for BOTH — git passes a credential helper its
// action verb (`get`) in $1. The branch that emitted valid `username=`/
// `password=` output was unreachable, so every git operation the coding agent
// attempted failed to authenticate. Nothing caught it because nothing ran the
// generated script; the dual-protocol claim lived only in a comment.
//
// So: assert against the real interpreter, and for the wiring assert against
// real git. `gitCredentialFill` below is the test that would have failed on day
// one.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import { exec } from "node:child_process";
import { promisify } from "node:util";
import { CREDHELPER_FILE, credHelperScript, ghWrapperScript } from "./credhelper.js";
import { shellQuote } from "./shell.js";

const execAsync = promisify(exec);

const TASK_ID = "6f1a2b3c-4d5e-6f70-8192-a3b4c5d6e7f8";
const GH_TOKEN = "ghs_TESTONLYabcdefghijklmnopqrstuvwxyz01";
const BEARER = "test-platform-bearer";

interface StubResponse {
  token?: string;
  taskId?: string;
  identity?: Record<string, string>;
}

interface Stub {
  url: string;
  /** Refresh requests received — 0 proves a code path returned before the round-trip. */
  requests: () => number;
  authHeaders: () => Array<string | undefined>;
  close: () => Promise<void>;
}

// A stand-in for POST /internal/v1/executions/{id}/credentials/refresh. Port 0
// so concurrent test files can't collide.
async function startStub(body: StubResponse): Promise<Stub> {
  let requests = 0;
  const authHeaders: Array<string | undefined> = [];
  const server = http.createServer((req, res) => {
    requests += 1;
    authHeaders.push(req.headers.authorization);
    req.resume();
    req.on("end", () => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(body));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const addr = server.address();
  if (addr === null || typeof addr === "string") throw new Error("stub failed to bind");
  return {
    url: `http://127.0.0.1:${addr.port}/internal/v1/executions/${TASK_ID}/credentials/refresh`,
    requests: () => requests,
    authHeaders: () => authHeaders,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

interface Fixture {
  dir: string;
  workspace: string;
  helper: string;
  bearerFile: string;
  env: NodeJS.ProcessEnv;
}

// Writes the helper exactly as provisionWorkspace does, plus an empty git repo
// standing in for the cloned work tree.
async function fixture(refreshUrl: string, opts?: { bearer?: string }): Promise<Fixture> {
  const dir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-credhelper-"));
  const workspace = path.join(dir, "workspace");
  await execAsync(`git init -q ${shellQuote(workspace)}`);

  const helper = path.join(dir, CREDHELPER_FILE);
  await fs.promises.writeFile(
    helper,
    credHelperScript({ taskId: TASK_ID, workspaceDir: workspace, refreshUrl }),
    { mode: 0o700 },
  );

  const bearerFile = path.join(dir, "bearer");
  await fs.promises.writeFile(bearerFile, opts?.bearer ?? BEARER, { mode: 0o600 });

  return {
    dir,
    workspace,
    helper,
    bearerFile,
    // A controlled env: PUBLISHER_* must be absent so these tests exercise the
    // bearer-file path deterministically even on a machine that has them set.
    env: {
      PATH: process.env.PATH,
      HOME: dir,
      AEP_BEARER_FILE: bearerFile,
      AEP_CORRELATION_ID: "corr-test-1",
    },
  };
}

interface RunResult {
  code: number;
  stdout: string;
  stderr: string;
}

async function run(cmd: string, env: NodeJS.ProcessEnv): Promise<RunResult> {
  try {
    const { stdout, stderr } = await execAsync(cmd, { env });
    return { code: 0, stdout, stderr };
  } catch (e) {
    const err = e as { code?: number; stdout?: string; stderr?: string };
    return { code: err.code ?? 1, stdout: err.stdout ?? "", stderr: err.stderr ?? "" };
  }
}

// The credential description git writes on stdin, which the helper drains and
// discards (it is config-scoped to one origin, so there is nothing to match).
const GIT_STDIN = "protocol=https\\nhost=github.com\\n\\n";
const withStdin = (script: string, action: string): string =>
  `printf '${GIT_STDIN}' | ${shellQuote(script)} ${action}`;

// ---- the credential-helper protocol ----------------------------------------

test("credhelper get: emits key=value lines git can parse", async () => {
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url);
  try {
    const r = await run(withStdin(f.helper, "get"), f.env);

    assert.equal(r.code, 0, `helper failed: ${r.stderr}`);
    // Exactly the two lines, in git's key=value form. A bare token here is what
    // the old script emitted: git answers it with "warning: invalid credential
    // line" and then fails the operation for want of a username.
    assert.deepEqual(r.stdout.trimEnd().split("\n"), [
      "username=x-access-token",
      `password=${GH_TOKEN}`,
    ]);
    assert.equal(stub.requests(), 1);
    assert.deepEqual(stub.authHeaders(), [`Bearer ${BEARER}`]);
  } finally {
    await stub.close();
  }
});

test("credhelper get: defaults to get when invoked with no action", async () => {
  // Not how git calls it, but it keeps a by-hand invocation useful for debugging
  // instead of silently doing nothing.
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url);
  try {
    const r = await run(`${shellQuote(f.helper)} </dev/null`, f.env);
    assert.equal(r.code, 0, `helper failed: ${r.stderr}`);
    assert.match(r.stdout, new RegExp(`^password=${GH_TOKEN}$`, "m"));
  } finally {
    await stub.close();
  }
});

for (const action of ["store", "erase"]) {
  test(`credhelper ${action}: no output and no refresh round-trip`, async () => {
    const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
    const f = await fixture(stub.url);
    try {
      const r = await run(withStdin(f.helper, action), f.env);

      assert.equal(r.code, 0, `${action} must exit 0: ${r.stderr}`);
      assert.equal(r.stdout, "", `${action} must print nothing`);
      // The token is re-minted per operation and never cached, so there is
      // nothing to store or erase — and no reason to burn a token mint.
      assert.equal(stub.requests(), 0, `${action} must not call the refresh endpoint`);
    } finally {
      await stub.close();
    }
  });
}

test("credhelper: an unknown action is ignored, not failed", async () => {
  // gitcredentials(7) asks helpers to ignore operations they don't implement, so
  // a future git that probes for a new action must not become an auth failure.
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url);
  try {
    const r = await run(withStdin(f.helper, "capability"), f.env);
    assert.equal(r.code, 0);
    assert.equal(r.stdout, "");
    assert.equal(stub.requests(), 0);
  } finally {
    await stub.close();
  }
});

// ---- real git, real protocol -------------------------------------------------

test("git credential fill: git accepts the helper's output for the configured origin", async () => {
  // The end-to-end wiring assertion, and the one that pins the regression: git
  // parses our `credential.<origin>.helper` key, invokes the script with `get`,
  // and accepts what it prints. Everything below the transport is covered.
  //
  // GIT_CONFIG_GLOBAL/SYSTEM must be neutered: an unisolated `credential fill`
  // consults the developer's osxkeychain/gh helper, which answers with a real
  // credential and would make a completely broken helper look like it worked.
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url);
  try {
    const scope = "https://github.com";
    const cfg = shellQuote(`credential.${scope}.helper=${f.helper}`);
    const r = await run(
      `printf 'protocol=https\\nhost=github.com\\n\\n' | git -c ${cfg} credential fill`,
      {
        ...f.env,
        GIT_CONFIG_GLOBAL: "/dev/null",
        GIT_CONFIG_SYSTEM: "/dev/null",
        GIT_TERMINAL_PROMPT: "0",
      },
    );

    assert.equal(r.code, 0, `git credential fill failed: ${r.stderr}`);
    assert.ok(
      !r.stderr.includes("invalid credential line"),
      `git rejected the helper's output: ${r.stderr}`,
    );
    assert.match(r.stdout, /^username=x-access-token$/m);
    assert.match(r.stdout, new RegExp(`^password=${GH_TOKEN}$`, "m"));
  } finally {
    await stub.close();
  }
});

// ---- anti-misroute tripwire (PR D §6.6) -------------------------------------

test("credhelper: refuses when the response's taskId is not the bound task", async () => {
  const stub = await startStub({ token: GH_TOKEN, taskId: "11111111-2222-3333-4444-555555555555" });
  const f = await fixture(stub.url);
  try {
    const r = await run(withStdin(f.helper, "get"), f.env);

    assert.notEqual(r.code, 0, "a misrouted bearer must not yield a credential");
    assert.ok(!r.stdout.includes(GH_TOKEN), `leaked the token anyway: ${r.stdout}`);
    assert.match(r.stderr, /refusing/);
    assert.match(r.stderr, new RegExp(TASK_ID));
  } finally {
    await stub.close();
  }
});

test("credhelper: tolerates a response that echoes no taskId", async () => {
  // Older git-service versions don't echo it; in that mode the bearer's
  // signature is the only credential check.
  const stub = await startStub({ token: GH_TOKEN });
  const f = await fixture(stub.url);
  try {
    const r = await run(withStdin(f.helper, "get"), f.env);
    assert.equal(r.code, 0, `helper failed: ${r.stderr}`);
    assert.match(r.stdout, new RegExp(`^password=${GH_TOKEN}$`, "m"));
  } finally {
    await stub.close();
  }
});

// ---- identity drift (PR D §6.6) ---------------------------------------------

// The wire contract CAPITALIZES Identity field names while the envelope keys are
// lowercase (packages/contracts/api/internal/v1/openapi.yaml). Reading only the
// lowercase form left this whole feature dead in production, so both cases are
// pinned here.
for (const [label, identity] of [
  ["capitalized (the wire contract)", { Login: "aep-bot", Name: "AEP Bot", Email: "bot@aep.dev" }],
  ["lowercase (tolerated)", { login: "aep-bot", name: "AEP Bot", email: "bot@aep.dev" }],
] as const) {
  test(`credhelper: rewrites .git/config identity on drift — ${label}`, async () => {
    const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID, identity });
    const f = await fixture(stub.url);
    try {
      await execAsync(`git -C ${shellQuote(f.workspace)} config user.name 'Stale Name'`);
      await execAsync(`git -C ${shellQuote(f.workspace)} config user.email 'stale@example.com'`);

      const r = await run(withStdin(f.helper, "get"), f.env);
      assert.equal(r.code, 0, `helper failed: ${r.stderr}`);

      const name = await execAsync(`git -C ${shellQuote(f.workspace)} config user.name`);
      const email = await execAsync(`git -C ${shellQuote(f.workspace)} config user.email`);
      assert.equal(name.stdout.trim(), "AEP Bot");
      assert.equal(email.stdout.trim(), "bot@aep.dev");
      assert.match(r.stderr, /identity drift detected/);
    } finally {
      await stub.close();
    }
  });
}

test("credhelper: leaves identity alone when it has not drifted", async () => {
  const stub = await startStub({
    token: GH_TOKEN,
    taskId: TASK_ID,
    identity: { Login: "aep-bot", Name: "AEP Bot", Email: "bot@aep.dev" },
  });
  const f = await fixture(stub.url);
  try {
    await execAsync(`git -C ${shellQuote(f.workspace)} config user.name 'AEP Bot'`);
    await execAsync(`git -C ${shellQuote(f.workspace)} config user.email 'bot@aep.dev'`);

    const r = await run(withStdin(f.helper, "get"), f.env);
    assert.equal(r.code, 0, `helper failed: ${r.stderr}`);
    assert.ok(!r.stderr.includes("identity drift"), `spurious rewrite: ${r.stderr}`);
  } finally {
    await stub.close();
  }
});

// ---- failure diagnostics -----------------------------------------------------

test("credhelper: names the missing bearer instead of failing mutely", async () => {
  // The failure mode that produced an hour of agent self-debugging: git's own
  // message ("could not read Username for …") says nothing about the cause.
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url, { bearer: "" });
  try {
    const r = await run(withStdin(f.helper, "get"), f.env);

    assert.notEqual(r.code, 0);
    assert.equal(r.stdout, "");
    assert.match(r.stderr, /no platform credential/);
    assert.match(r.stderr, /PUBLISHER_\* unset/);
    assert.equal(stub.requests(), 0);
  } finally {
    await stub.close();
  }
});

test("credhelper: names a failed publisher mint when PUBLISHER_* is set", async () => {
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url, { bearer: "" });
  try {
    const r = await run(withStdin(f.helper, "get"), {
      ...f.env,
      PUBLISHER_CLIENT_ID: "id",
      PUBLISHER_CLIENT_SECRET: "sec",
      PUBLISHER_TOKEN_URL: "http://aep-token.invalid/oauth2/token",
    });

    assert.notEqual(r.code, 0);
    assert.equal(r.stdout, "");
    assert.match(r.stderr, /PUBLISHER_\* mint failed/);
    assert.doesNotMatch(r.stderr, /PUBLISHER_\* unset/);
    assert.equal(stub.requests(), 0);
  } finally {
    await stub.close();
  }
});

test("credhelper: names a refresh endpoint that will not answer", async () => {
  // `.invalid` is reserved and never resolves.
  const f = await fixture("http://aep-refresh.invalid/internal/v1/executions/x/credentials/refresh");
  const r = await run(withStdin(f.helper, "get"), f.env);

  assert.notEqual(r.code, 0);
  assert.equal(r.stdout, "");
  assert.match(r.stderr, /credential refresh failed/);
});

test("credhelper: never prints the platform bearer", async () => {
  const stub = await startStub({ token: GH_TOKEN, taskId: "mismatched-on-purpose" });
  const f = await fixture(stub.url);
  try {
    const r = await run(withStdin(f.helper, "get"), f.env);
    assert.ok(
      !`${r.stdout}${r.stderr}`.includes(BEARER),
      `diagnostics leaked the bearer: ${r.stdout}${r.stderr}`,
    );
  } finally {
    await stub.close();
  }
});

// ---- the gh wrapper ----------------------------------------------------------

test("gh wrapper: gets its token from credhelper.sh and writes hosts.yml", async () => {
  // The wrapper used to carry its own copy of the exchange that read
  // $AEP_BEARER_FILE only, so a publisher-cc run served whatever token was
  // minted at pod start and degraded silently once it expired. It now delegates,
  // which is what this asserts: one implementation of the refresh.
  const stub = await startStub({ token: GH_TOKEN, taskId: TASK_ID });
  const f = await fixture(stub.url);
  try {
    const ghConfigDir = path.join(f.dir, "gh-config");
    // A stub "real gh" that reports its argv, proving the wrapper execs through.
    const fakeGh = path.join(f.dir, "real-gh");
    await fs.promises.writeFile(fakeGh, '#!/bin/sh\necho "GH_ARGS:$*"\n', { mode: 0o755 });

    const wrapper = path.join(f.dir, "gh");
    await fs.promises.writeFile(wrapper, ghWrapperScript(fakeGh), { mode: 0o755 });

    const r = await run(`${shellQuote(wrapper)} pr create --title x`, {
      ...f.env,
      GH_CONFIG_DIR: ghConfigDir,
    });

    assert.equal(r.code, 0, `wrapper failed: ${r.stderr}`);
    assert.match(r.stdout, /GH_ARGS:pr create --title x/);

    const hosts = await fs.promises.readFile(path.join(ghConfigDir, "hosts.yml"), "utf-8");
    assert.match(hosts, new RegExp(`oauth_token: ${GH_TOKEN}`));
    assert.equal(stub.requests(), 1, "the wrapper should refresh exactly once, via the helper");
  } finally {
    await stub.close();
  }
});

test("gh wrapper: still execs gh when the refresh fails", async () => {
  // hosts.yml from an earlier call may still be valid; gh reports its own auth
  // error if it isn't. A hard failure here would break `gh` for reasons
  // unrelated to what the agent was doing.
  const f = await fixture("http://aep-refresh.invalid/internal/v1/executions/x/credentials/refresh");
  const fakeGh = path.join(f.dir, "real-gh");
  await fs.promises.writeFile(fakeGh, '#!/bin/sh\necho "GH_RAN"\n', { mode: 0o755 });
  const wrapper = path.join(f.dir, "gh");
  await fs.promises.writeFile(wrapper, ghWrapperScript(fakeGh), { mode: 0o755 });

  const r = await run(`${shellQuote(wrapper)} auth status`, {
    ...f.env,
    GH_CONFIG_DIR: path.join(f.dir, "gh-config"),
  });

  assert.equal(r.code, 0, `wrapper should not hard-fail: ${r.stderr}`);
  assert.match(r.stdout, /GH_RAN/);
  assert.match(r.stderr, /credential refresh failed/);
});

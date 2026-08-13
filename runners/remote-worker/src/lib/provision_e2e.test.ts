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

// Provisioning end-to-end, against a REAL authenticated git remote
// (`git-http-backend` behind Basic auth). Nothing is mocked below the credential
// layer: provisionWorkspace clones, then this drives the exact git operations the
// coding agent performs, all authenticating through the credential helper.
//
// This test exists because the bug it guards lived in the COMBINATION, which is
// what the unit tests around it each miss a piece of: the clone's `git -c`
// wiring, the durable `.git/config` entry, and the helper's protocol only add up
// to a working run together. The original break — a helper that dispatched on
// `[ -n "$1" ]` and so never answered git's `get` — left the clone working and
// every agent operation failing, and shipped because nothing ran the real path.
//
// Host isolation is mandatory here, not hygiene: the 401 this server returns makes
// git consult every configured credential helper, and Homebrew's git ships
// `credential.helper=osxkeychain` in system config. Without GIT_CONFIG_SYSTEM
// neutered, a developer running this gets keychain prompts and git's post-success
// `store` writes the test token into their real keychain.

import { after, test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import { exec, spawn } from "node:child_process";
import { promisify } from "node:util";

const execAsync = promisify(exec);

const TASK_ID = "6f1a2b3c-4d5e-6f70-8192-a3b4c5d6e7f8";
const GH_TOKEN = "ghs_TESTONLYabcdefghijklmnopqrstuv01";
const BEARER = "test-platform-bearer";
const EXPECTED_BASIC = Buffer.from(`x-access-token:${GH_TOKEN}`).toString("base64");

function restoreEnvVar(name: string, previous: string | undefined): void {
  if (previous === undefined) delete process.env[name];
  else process.env[name] = previous;
}

// Neuter host git config BEFORE anything imports or runs git. buildCloneInvocation
// spreads process.env into the clone child, so setting it here covers that too.
process.env.GIT_CONFIG_GLOBAL = "/dev/null";
process.env.GIT_CONFIG_SYSTEM = "/dev/null";
process.env.GIT_CONFIG_NOSYSTEM = "1";
process.env.GIT_TERMINAL_PROMPT = "0";
// This suite exercises the AEP credhelper → refresh path. A developer machine
// with GITHUB_TOKEN/GH_TOKEN set would take the gh-token provision branch and
// skip the helper under test. Capture both before deleting so suite cleanup
// can restore the host environment.
const suitePrevGithubToken = process.env.GITHUB_TOKEN;
const suitePrevGhToken = process.env.GH_TOKEN;
delete process.env.GITHUB_TOKEN;
delete process.env.GH_TOKEN;

after(() => {
  restoreEnvVar("GITHUB_TOKEN", suitePrevGithubToken);
  restoreEnvVar("GH_TOKEN", suitePrevGhToken);
});

// config.workspaceBasePath is captured at module load and defaults to the
// developer's homedir, so it must be set before workspace.js is imported —
// hence the dynamic import below rather than a static one.
const WORKSPACE_ROOT = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-e2e-ws-"));
process.env.WORKSPACE_BASE_PATH = WORKSPACE_ROOT;
const { provisionWorkspace } = await import("./workspace.js");

const GIT_EXEC_PATH = (await execAsync("git --exec-path")).stdout.trim();
const HAS_HTTP_BACKEND = fs.existsSync(path.join(GIT_EXEC_PATH, "git-http-backend"));

interface GitServer {
  port: number;
  authed: () => number;
  rejected: () => number;
  close: () => Promise<void>;
}

// A git HTTP server that DEMANDS Basic auth, so clone/fetch/push succeed only if
// the credential helper actually supplied a credential.
function startGitServer(projectRoot: string): Promise<GitServer> {
  let authed = 0;
  let rejected = 0;
  const server = http.createServer((req, res) => {
    if (req.headers.authorization !== `Basic ${EXPECTED_BASIC}`) {
      rejected += 1;
      req.resume();
      res.writeHead(401, { "WWW-Authenticate": 'Basic realm="git"' }).end("auth required");
      return;
    }
    authed += 1;

    const url = new URL(req.url ?? "/", "http://localhost");
    const cgi = spawn(path.join(GIT_EXEC_PATH, "git-http-backend"), [], {
      env: {
        PATH: process.env.PATH,
        GIT_PROJECT_ROOT: projectRoot,
        GIT_HTTP_EXPORT_ALL: "1",
        REQUEST_METHOD: req.method ?? "GET",
        PATH_INFO: url.pathname,
        QUERY_STRING: url.search.replace(/^\?/, ""),
        CONTENT_TYPE: req.headers["content-type"] ?? "",
        CONTENT_LENGTH: req.headers["content-length"] ?? "",
        REMOTE_USER: "x-access-token",
      },
    });
    req.pipe(cgi.stdin);

    const chunks: Buffer[] = [];
    cgi.stdout.on("data", (c: Buffer) => chunks.push(c));
    cgi.on("close", () => {
      // git-http-backend speaks CGI: headers, blank line, body.
      const out = Buffer.concat(chunks);
      const split = out.indexOf("\r\n\r\n");
      const body = split === -1 ? out : out.subarray(split + 4);
      const headers: Record<string, string> = {};
      let status = 200;
      for (const line of (split === -1 ? "" : out.subarray(0, split).toString()).split("\r\n")) {
        const i = line.indexOf(":");
        if (i === -1) continue;
        const k = line.slice(0, i).trim();
        const v = line.slice(i + 1).trim();
        if (k.toLowerCase() === "status") status = Number.parseInt(v, 10) || 200;
        else headers[k] = v;
      }
      res.writeHead(status, headers).end(body);
    });
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (addr === null || typeof addr === "string") throw new Error("git server bind failed");
      resolve({
        port: addr.port,
        authed: () => authed,
        rejected: () => rejected,
        close: () => new Promise<void>((r) => {
          server.closeAllConnections();
          server.close(() => r());
        }),
      });
    });
  });
}

interface RefreshStub {
  url: string;
  calls: () => number;
  close: () => Promise<void>;
}

function startRefreshStub(taskId: string): Promise<RefreshStub> {
  let calls = 0;
  const server = http.createServer((req, res) => {
    calls += 1;
    req.resume();
    req.on("end", () => {
      res.writeHead(200, { "Content-Type": "application/json" }).end(
        JSON.stringify({
          token: GH_TOKEN,
          expiresAt: "2099-01-01T00:00:00Z",
          taskId,
          // CAPITALIZED, as the wire contract specifies.
          identity: { Name: "AEP Bot", Email: "bot@aep.dev", Login: "aep-bot" },
        }),
      );
    });
  });
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (addr === null || typeof addr === "string") throw new Error("stub bind failed");
      resolve({
        url: `http://127.0.0.1:${addr.port}/internal/v1/executions/${taskId}/credentials/refresh`,
        calls: () => calls,
        close: () => new Promise<void>((r) => {
          server.closeAllConnections();
          server.close(() => r());
        }),
      });
    });
  });
}

test(
  "provisioning e2e: clone, fetch and push all authenticate through the credential helper",
  {
    skip: HAS_HTTP_BACKEND ? false : "git-http-backend not available",
    // Bounded on purpose: a broken credential path must FAIL this suite, never
    // hang it. node:test applies no default per-test timeout.
    timeout: 90_000,
  },
  async () => {
    // Belt and braces on the isolation: if a helper is still reachable, this test
    // would read (and git's `store` would write) the developer's real keychain.
    const inherited = await execAsync("git config --get-all credential.helper || true");
    assert.equal(inherited.stdout.trim(), "", "host credential helpers must be isolated");

    const root = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-e2e-"));
    const serveRoot = path.join(root, "serve");
    const origin = path.join(serveRoot, "store.git");
    await fs.promises.mkdir(serveRoot, { recursive: true });
    await execAsync(`git init --bare -q "${origin}"`);
    await execAsync(`git -C "${origin}" symbolic-ref HEAD refs/heads/main`);
    await execAsync(`git -C "${origin}" config http.receivepack true`);

    const seed = path.join(root, "seed");
    await execAsync(`git init -q "${seed}"`);
    await fs.promises.writeFile(path.join(seed, "README.md"), "seed\n");
    await execAsync(`git -C "${seed}" add .`);
    await execAsync(`git -C "${seed}" -c user.name=T -c user.email=t@e.com commit -qm seed`);
    await execAsync(`git -C "${seed}" push -q "${origin}" HEAD:refs/heads/main`);

    const gitServer = await startGitServer(serveRoot);
    const stub = await startRefreshStub(TASK_ID);

    try {
      const layout = await provisionWorkspace({
        orgId: "default",
        projectId: "store",
        taskId: TASK_ID,
        repoUrl: `http://127.0.0.1:${gitServer.port}/store.git`,
        bearer: BEARER,
        identity: { name: "AEP Bot", email: "bot@aep.dev", login: "aep-bot" },
        gitServiceUrl: `http://127.0.0.1:${gitServer.port}`,
        refreshUrl: stub.url,
        correlationId: "e2e-corr-1",
      });

      // ---- provisioning ---------------------------------------------------
      assert.ok(fs.existsSync(path.join(layout.workspace, "README.md")), "clone should check out");
      assert.ok(gitServer.rejected() > 0, "the origin must actually have demanded auth");
      assert.ok(stub.calls() > 0, "the helper must have performed the exchange");
      assert.equal((await fs.promises.stat(layout.helperBin)).mode & 0o777, 0o700);
      assert.equal((await fs.promises.stat(layout.bearerFile)).mode & 0o777, 0o600);
      assert.ok(!fs.existsSync(`${layout.workspace}.stage`), "staging dir must be cleaned up");

      const cfg = await fs.promises.readFile(path.join(layout.workspace, ".git", "config"), "utf-8");
      assert.ok(
        cfg.includes(`[credential "http://127.0.0.1:${gitServer.port}"]`),
        `durable helper must be scoped to the origin:\n${cfg}`,
      );
      assert.ok(cfg.includes(layout.helperBin), "durable helper must point at .aep/credhelper.sh");
      assert.match(cfg, /\[credential\]\s*\n\s*helper =\s*\n/, "must reset inherited helpers");
      assert.ok(!cfg.includes(GH_TOKEN), "no credential at rest in .git/config");

      // ---- the operations that failed in production ------------------------
      const agentEnv = {
        PATH: `${layout.aepDir}:${process.env.PATH ?? ""}`,
        HOME: root,
        GH_CONFIG_DIR: layout.ghConfigDir,
        AEP_BEARER_FILE: layout.bearerFile,
        AEP_CORRELATION_ID: "e2e-corr-1",
        GIT_TERMINAL_PROMPT: "0",
        GIT_CONFIG_GLOBAL: "/dev/null",
        GIT_CONFIG_SYSTEM: "/dev/null",
        GIT_CONFIG_NOSYSTEM: "1",
      };
      const g = (cmd: string) => execAsync(`git -C "${layout.workspace}" ${cmd}`, { env: agentEnv });

      // SKILL.md branch-identity discovery: the first thing a run does, and the
      // exact pair that produced "could not read Username for 'https://github.com'".
      await g("fetch origin");
      await g('ls-remote --heads origin "aep/m1-*"');

      // SKILL.md per-issue commit + push.
      await g("checkout -q -b aep/m1-c1");
      await fs.promises.writeFile(path.join(layout.workspace, "feature.txt"), "work\n");
      await g("add feature.txt");
      await g('commit -qm "feat: add feature (#1)"');
      await g("push -q -u origin HEAD");

      const refs = await execAsync(`git -C "${origin}" for-each-ref --format='%(refname)'`);
      assert.match(refs.stdout, /refs\/heads\/aep\/m1-c1/, "the pushed branch must land on origin");

      // Identity came from the refresh response's CAPITALIZED fields — the path
      // that was dead for the whole of its first life.
      const author = await g("log -1 --format='%an <%ae>'");
      assert.equal(author.stdout.trim(), "AEP Bot <bot@aep.dev>");

      // ---- hygiene --------------------------------------------------------
      const offenders: string[] = [];
      const walk = async (d: string): Promise<void> => {
        for (const e of await fs.promises.readdir(d, { withFileTypes: true })) {
          const full = path.join(d, e.name);
          if (e.isDirectory()) await walk(full);
          else if (e.isFile() && (await fs.promises.readFile(full)).includes(GH_TOKEN)) {
            offenders.push(full);
          }
        }
      };
      await walk(layout.workspace);
      assert.deepEqual(offenders, [], `token found at rest in: ${offenders.join(", ")}`);
    } finally {
      await gitServer.close();
      await stub.close();
      await fs.promises.rm(root, { recursive: true, force: true });
    }
  },
);

test(
  "provisioning e2e (GITHUB_TOKEN): clone and push use gh auth git-credential, not refresh",
  {
    skip: HAS_HTTP_BACKEND ? false : "git-http-backend not available",
    timeout: 90_000,
  },
  async () => {
    const root = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-e2e-gh-"));
    const binDir = path.join(root, "bin");
    await fs.promises.mkdir(binDir, { recursive: true });
    // Minimal `gh` that implements the credential-helper protocol git expects
    // from `gh auth git-credential` — same shape setup-git installs.
    await fs.promises.writeFile(
      path.join(binDir, "gh"),
      `#!/usr/bin/env bash
set -e
if [ "$1" = "auth" ] && [ "$2" = "git-credential" ]; then
  cat >/dev/null || true
  echo "username=x-access-token"
  echo "password=\${GITHUB_TOKEN}"
  exit 0
fi
echo "fake-gh: unexpected args: $*" >&2
exit 1
`,
      { mode: 0o755 },
    );

    const serveRoot = path.join(root, "serve");
    const origin = path.join(serveRoot, "store.git");
    await fs.promises.mkdir(serveRoot, { recursive: true });
    await execAsync(`git init --bare -q "${origin}"`);
    await execAsync(`git -C "${origin}" symbolic-ref HEAD refs/heads/main`);
    await execAsync(`git -C "${origin}" config http.receivepack true`);

    const seed = path.join(root, "seed");
    await execAsync(`git init -q "${seed}"`);
    await fs.promises.writeFile(path.join(seed, "README.md"), "seed\n");
    await execAsync(`git -C "${seed}" add .`);
    await execAsync(`git -C "${seed}" -c user.name=T -c user.email=t@e.com commit -qm seed`);
    await execAsync(`git -C "${seed}" push -q "${origin}" HEAD:refs/heads/main`);

    const gitServer = await startGitServer(serveRoot);
    const stub = await startRefreshStub(TASK_ID);

    const prevPath = process.env.PATH;
    const prevGithubToken = process.env.GITHUB_TOKEN;
    const prevGhToken = process.env.GH_TOKEN;
    process.env.PATH = `${binDir}:${prevPath ?? ""}`;
    process.env.GITHUB_TOKEN = GH_TOKEN;
    delete process.env.GH_TOKEN;

    try {
      const layout = await provisionWorkspace({
        orgId: "default",
        projectId: "store-gh",
        taskId: TASK_ID,
        repoUrl: `http://127.0.0.1:${gitServer.port}/store.git`,
        bearer: BEARER,
        identity: { name: "Token User", email: "u@e.com", login: "token-user" },
        gitServiceUrl: `http://127.0.0.1:${gitServer.port}`,
        refreshUrl: stub.url,
        correlationId: "e2e-gh-1",
      });

      assert.ok(fs.existsSync(path.join(layout.workspace, "README.md")), "clone should check out");
      assert.ok(gitServer.rejected() > 0, "origin must demand auth");
      assert.equal(stub.calls(), 0, "credentials/refresh must not be called when GITHUB_TOKEN is set");
      assert.ok(!fs.existsSync(layout.helperBin), "credhelper.sh must not be installed in gh-token mode");

      const cfg = await fs.promises.readFile(path.join(layout.workspace, ".git", "config"), "utf-8");
      assert.ok(cfg.includes("auth git-credential"), `durable helper must be gh:\n${cfg}`);
      assert.ok(!cfg.includes("credhelper"), `must not wire AEP credhelper:\n${cfg}`);
      assert.ok(!cfg.includes(GH_TOKEN), "no credential at rest in .git/config");

      const wrapper = await fs.promises.readFile(layout.ghWrapper, "utf-8");
      assert.match(wrapper, /Env-token mode/);
      assert.ok(!wrapper.includes("credhelper.sh"), wrapper);

      const agentEnv = {
        PATH: `${layout.aepDir}:${binDir}:${prevPath ?? ""}`,
        HOME: root,
        GH_CONFIG_DIR: layout.ghConfigDir,
        GITHUB_TOKEN: GH_TOKEN,
        GIT_TERMINAL_PROMPT: "0",
        GIT_CONFIG_GLOBAL: "/dev/null",
        GIT_CONFIG_SYSTEM: "/dev/null",
        GIT_CONFIG_NOSYSTEM: "1",
      };
      const g = (cmd: string) => execAsync(`git -C "${layout.workspace}" ${cmd}`, { env: agentEnv });

      await g("checkout -q -b aep/m1-c1");
      await fs.promises.writeFile(path.join(layout.workspace, "feature.txt"), "work\n");
      await g("add feature.txt");
      await g('commit -qm "feat: add feature (#1)"');
      await g("push -q -u origin HEAD");

      assert.equal(stub.calls(), 0, "push must not call credentials/refresh");
      const refs = await execAsync(`git -C "${origin}" for-each-ref --format='%(refname)'`);
      assert.match(refs.stdout, /refs\/heads\/aep\/m1-c1/);
    } finally {
      process.env.PATH = prevPath;
      restoreEnvVar("GITHUB_TOKEN", prevGithubToken);
      restoreEnvVar("GH_TOKEN", prevGhToken);
      await gitServer.close();
      await stub.close();
      await fs.promises.rm(root, { recursive: true, force: true });
    }
  },
);

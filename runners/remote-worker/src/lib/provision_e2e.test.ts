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
// coding agent performs, all authenticating through `gh auth git-credential`
// with the Job's mounted GITHUB_TOKEN.
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
// Each test sets (or clears) the token it needs; capture the host's pair so
// suite cleanup can restore it.
const suitePrevGithubToken = process.env.GITHUB_TOKEN;
const suitePrevGhToken = process.env.GH_TOKEN;

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

test(
  "provisioning e2e: clone, fetch and push authenticate through gh auth git-credential",
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
        correlationId: "e2e-gh-1",
      });

      assert.ok(fs.existsSync(path.join(layout.workspace, "README.md")), "clone should check out");
      assert.ok(gitServer.rejected() > 0, "origin must demand auth");
      assert.ok(gitServer.authed() > 0, "the clone must have authenticated");
      assert.ok(!fs.existsSync(path.join(layout.aepDir, "credhelper.sh")), "no AEP credhelper is installed");

      const cfg = await fs.promises.readFile(path.join(layout.workspace, ".git", "config"), "utf-8");
      assert.ok(cfg.includes("auth git-credential"), `durable helper must be gh:\n${cfg}`);
      assert.ok(!cfg.includes("credhelper"), `must not wire AEP credhelper:\n${cfg}`);
      assert.ok(!cfg.includes(GH_TOKEN), "no credential at rest in .git/config");

      const wrapper = await fs.promises.readFile(layout.ghWrapper, "utf-8");
      assert.match(wrapper, /^# Passthrough: exec the real gh binary/m);
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

      // SKILL.md branch-identity discovery: the first thing a run does.
      await g("fetch origin");
      await g('ls-remote --heads origin "aep/m1-*"');

      await g("checkout -q -b aep/m1-c1");
      await fs.promises.writeFile(path.join(layout.workspace, "feature.txt"), "work\n");
      await g("add feature.txt");
      await g('commit -qm "feat: add feature (#1)"');
      await g("push -q -u origin HEAD");

      const refs = await execAsync(`git -C "${origin}" for-each-ref --format='%(refname)'`);
      assert.match(refs.stdout, /refs\/heads\/aep\/m1-c1/);
    } finally {
      process.env.PATH = prevPath;
      restoreEnvVar("GITHUB_TOKEN", prevGithubToken);
      restoreEnvVar("GH_TOKEN", prevGhToken);
      await gitServer.close();
      await fs.promises.rm(root, { recursive: true, force: true });
    }
  },
);

test("provisioning without GITHUB_TOKEN fails fast with exit 2 and makes no HTTP call", async () => {
  // The Job mounts the org's gitpat as GITHUB_TOKEN; without it there is no git
  // credential. Provisioning must say so before it touches the network.
  // oneshot.ts maps any provisioning throw to exit code 2.
  const root = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-e2e-none-"));
  const gitServer = await startGitServer(root);
  const prevGithubToken = process.env.GITHUB_TOKEN;
  const prevGhToken = process.env.GH_TOKEN;
  delete process.env.GITHUB_TOKEN;
  delete process.env.GH_TOKEN;
  try {
    await assert.rejects(
      provisionWorkspace({
        orgId: "default",
        projectId: "store-none",
        taskId: TASK_ID,
        repoUrl: `http://127.0.0.1:${gitServer.port}/store.git`,
        bearer: BEARER,
        identity: { name: "AEP Bot", email: "bot@aep.dev" },
      }),
      { message: "GITHUB_TOKEN (or GH_TOKEN) is required: the coding Job mounts the org's gitpat" },
    );
    assert.equal(gitServer.authed() + gitServer.rejected(), 0, "no HTTP call may be made");
  } finally {
    restoreEnvVar("GITHUB_TOKEN", prevGithubToken);
    restoreEnvVar("GH_TOKEN", prevGhToken);
    await gitServer.close();
    await fs.promises.rm(root, { recursive: true, force: true });
  }
});

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

// The clone's credential wiring. That the helper git is pointed at actually
// speaks git's protocol is covered by credhelper.test.ts, which drives it with
// real git; these tests cover the invocation this module builds.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { exec } from "node:child_process";
import { promisify } from "node:util";
import { buildCloneInvocation, cloneCredentialScope, cloneWithHelper } from "./git_clone.js";
import { shellQuote } from "./shell.js";

const execAsync = promisify(exec);

const HELPER = "/home/aep/aep-workspace/default/store/abc.stage/credhelper.sh";
const BEARER_FILE = "/home/aep/aep-workspace/default/store/abc.stage/bearer";

async function tmpDir(): Promise<string> {
  return fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-clone-test-"));
}

// ---- cloneCredentialScope ---------------------------------------------------

test("cloneCredentialScope: scheme + host, so the helper is offered to one origin", () => {
  assert.equal(
    cloneCredentialScope("https://github.com/asdlc-repos/store.git"),
    "https://github.com",
  );
  // Host-scoped rather than a bare credential.helper: a redirect to another host
  // cannot draw the credential out of the helper.
  assert.equal(cloneCredentialScope("https://ghe.corp.example/o/r.git"), "https://ghe.corp.example");
  assert.equal(cloneCredentialScope("http://127.0.0.1:8080/o/r.git"), "http://127.0.0.1:8080");
});

test("cloneCredentialScope: undefined for origins that need no credential", () => {
  assert.equal(cloneCredentialScope("/tmp/origin.git"), undefined);
  assert.equal(cloneCredentialScope("file:///tmp/origin.git"), undefined);
  assert.equal(cloneCredentialScope("git@github.com:o/r.git"), undefined);
});

// ---- buildCloneInvocation (pure seam) ---------------------------------------

test("buildCloneInvocation: wires the helper in, carries no credential anywhere", () => {
  const { cmd, env } = buildCloneInvocation({
    repoUrl: "https://github.com/asdlc-repos/store-handmade-ceramics.git",
    destDir: "/home/aep/aep-workspace/default/store/abc",
    helperPath: HELPER,
    bearerFile: BEARER_FILE,
    baseEnv: {},
  });

  // `git -c` BEFORE the subcommand: applies to this command only, and is not
  // written into the cloned repo's config (`git clone -c` would persist it).
  //
  // The empty `credential.helper=` must come FIRST. Git takes the first helper
  // that answers, so a helper inherited from system config (Homebrew's git ships
  // `credential.helper=osxkeychain`) would authenticate the clone instead of
  // ours, and would be handed our token by git's post-success `store`.
  assert.equal(
    cmd,
    `git -c credential.helper= -c 'credential.https://github.com.helper=${HELPER}' ` +
      "clone 'https://github.com/asdlc-repos/store-handmade-ceramics.git' " +
      "'/home/aep/aep-workspace/default/store/abc'",
  );
  assert.ok(!cmd.includes("x-access-token"), "command must not carry URL userinfo");

  // The bearer travels as a PATH, never a value: the helper reads the file.
  assert.equal(env.AEP_BEARER_FILE, BEARER_FILE);
  assert.equal(env.GIT_TERMINAL_PROMPT, "0");
});

test("buildCloneInvocation: --depth 1 when requested", () => {
  const { cmd } = buildCloneInvocation({
    repoUrl: "https://github.com/acme/org-skills.git",
    destDir: "/tmp/skills",
    helperPath: HELPER,
    bearerFile: BEARER_FILE,
    depth1: true,
    baseEnv: {},
  });
  assert.ok(cmd.startsWith("git -c credential.helper= -c "), cmd);
  assert.match(cmd, /clone --depth 1 'https:\/\/github\.com\/acme\/org-skills\.git' '\/tmp\/skills'$/);
});

test("buildCloneInvocation: gh shell helper carries no AEP_BEARER_FILE", () => {
  const helper = "!/usr/bin/gh auth git-credential";
  const { cmd, env } = buildCloneInvocation({
    repoUrl: "https://github.com/o/r.git",
    destDir: "/tmp/dest",
    helperPath: helper,
    bearerFile: "",
    baseEnv: { GITHUB_TOKEN: "ghs_test" },
  });
  assert.equal(
    cmd,
    `git -c credential.helper= -c 'credential.https://github.com.helper=${helper}' ` +
      "clone 'https://github.com/o/r.git' '/tmp/dest'",
  );
  assert.equal(env.AEP_BEARER_FILE, undefined);
  assert.equal(env.GITHUB_TOKEN, "ghs_test");
});

test("buildCloneInvocation: no helperPath configures no helper at all", () => {
  // So a genuinely missing credential surfaces as git's own error rather than as
  // a helper that answers with nothing.
  const { cmd, env } = buildCloneInvocation({
    repoUrl: "https://github.com/o/r.git",
    destDir: "/tmp/dest",
    helperPath: "",
    bearerFile: "",
    baseEnv: {},
  });
  assert.equal(cmd, "git clone 'https://github.com/o/r.git' '/tmp/dest'");
  assert.equal(env.AEP_BEARER_FILE, undefined);
});

test("buildCloneInvocation: an origin needing no credential gets no helper", () => {
  const { cmd, env } = buildCloneInvocation({
    repoUrl: "/tmp/origin.git",
    destDir: "/tmp/dest",
    helperPath: HELPER,
    bearerFile: BEARER_FILE,
    baseEnv: {},
  });
  assert.equal(cmd, "git clone '/tmp/origin.git' '/tmp/dest'");
  assert.equal(env.AEP_BEARER_FILE, undefined);
});

test("buildCloneInvocation: does not mutate the caller's base env", () => {
  const baseEnv: NodeJS.ProcessEnv = { PATH: "/usr/bin" };
  buildCloneInvocation({
    repoUrl: "https://example.invalid/r.git",
    destDir: "/tmp/d",
    helperPath: HELPER,
    bearerFile: BEARER_FILE,
    baseEnv,
  });
  assert.deepEqual(baseEnv, { PATH: "/usr/bin" });
});

// ---- the reported leak ------------------------------------------------------

test("cloneWithHelper: a failed clone's error message carries no credential", async () => {
  // Regression for the credential leak: the runner logs this error and the BFF
  // forwards it into the console build log. `.invalid` is reserved and never
  // resolves, reproducing the exact "Could not resolve host" failure that
  // surfaced the token. With the helper doing the exchange there is no token in
  // the process at all, but the command must still be safe to print verbatim.
  const dir = await tmpDir();
  await assert.rejects(
    cloneWithHelper({
      repoUrl: "https://aep-does-not-exist.invalid/asdlc-repos/store.git",
      destDir: path.join(dir, "dest"),
      helperPath: path.join(dir, "credhelper.sh"),
      bearerFile: path.join(dir, "bearer"),
    }),
    (err: unknown) => {
      const text = err instanceof Error ? `${err.stack ?? ""}${err.message}` : String(err);
      assert.ok(text.includes("git "), `expected the clone command in the error, got: ${text}`);
      assert.ok(!text.includes("x-access-token"), `clone error leaked URL userinfo: ${text}`);
      return true;
    },
  );
});

test("cloneWithHelper: a successful clone leaves no credential at rest", async () => {
  // The .git/config analogue of gitfs/hygiene_test.go. Two things are asserted:
  // the clone works, and `git -c` did NOT persist the helper into the cloned
  // config — workspace.ts installs the durable one itself, pointed at the
  // helper's FINAL path, not the staging path used here.
  const dir = await tmpDir();
  const origin = path.join(dir, "origin.git");
  const seed = path.join(dir, "seed");

  await execAsync(`git init --bare -q ${shellQuote(origin)}`);
  await fs.promises.mkdir(seed, { recursive: true });
  await execAsync(`git init -q ${shellQuote(seed)}`);
  await fs.promises.writeFile(path.join(seed, "README.md"), "hello\n");
  await execAsync(`git -C ${shellQuote(seed)} add .`);
  await execAsync(
    `git -C ${shellQuote(seed)} -c user.name=T -c user.email=t@example.com commit -qm seed`,
  );
  await execAsync(`git -C ${shellQuote(seed)} push -q ${shellQuote(origin)} HEAD:refs/heads/main`);
  // `git init --bare` points HEAD at the local init.defaultBranch, which need
  // not be `main`; without this the clone checks out nothing.
  await execAsync(`git -C ${shellQuote(origin)} symbolic-ref HEAD refs/heads/main`);

  const dest = path.join(dir, "clone");
  const stageHelper = path.join(dir, "stage", "credhelper.sh");
  await cloneWithHelper({
    repoUrl: origin,
    destDir: dest,
    helperPath: stageHelper,
    bearerFile: path.join(dir, "stage", "bearer"),
  });

  assert.ok(fs.existsSync(path.join(dest, "README.md")), "clone should have checked out the tree");

  // runner.ts spreads process.env into the agent's child env, so anything the
  // clone needed must have stayed on the child's env object.
  assert.equal(
    process.env.AEP_BEARER_FILE,
    undefined,
    "the clone's bearer path must never be written to process.env",
  );

  const clonedConfig = await fs.promises.readFile(path.join(dest, ".git", "config"), "utf-8");
  assert.ok(
    !clonedConfig.includes(stageHelper),
    `git -c leaked the staging helper into the cloned config:\n${clonedConfig}`,
  );
});

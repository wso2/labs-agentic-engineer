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

// The guard plugin AS SHIPPED: built by the same function the image runs, then
// imported from the built directory and driven through its hook — so a bundle
// that lost a module, a package.json that stopped naming the entry, or a
// sentence that drifted from the Claude Code hooks' fails here.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { authoredPathDenial } from "../../../lib/workspace_guard.js";
import { WEBSEARCH_DENIAL_MESSAGE } from "../../../lib/websearch_dlp.js";
import { buildGuardPlugin, GUARD_PACKAGE_JSON } from "./build.js";
import { createGuardDecision } from "./guard.js";
import { GUARD_ENV, STARTUP_PROBE_PROMPT, WORKSPACE_GUARD_MARKER } from "./protocol.js";

type Hooks = {
  "tool.execute.before": (i: { tool: string; sessionID: string; callID: string }, o: { args: unknown }) => Promise<void>;
  "chat.message": (i: { sessionID: string }, o: { message: { agent?: string }; parts?: unknown[] }) => Promise<void>;
  "experimental.chat.system.transform": (i: { sessionID?: string }, o: { system: string[] }) => Promise<void>;
  "chat.params": (i: { sessionID: string }) => Promise<void>;
};
type PluginModule = { AepGuard: () => Promise<Hooks> };

const SECRET = "sk-staged-secret-value-123456";

async function built(): Promise<{ dir: string; mod: PluginModule; tmp: string }> {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "aep-guard-test-"));
  const dir = await buildGuardPlugin(path.join(tmp, "aep-guard"));
  const mod = (await import(pathToFileURL(path.join(dir, "index.js")).href)) as PluginModule;
  return { dir, mod, tmp };
}

function withEnv<T>(env: Record<string, string | undefined>, fn: () => Promise<T>): Promise<T> {
  const before = Object.fromEntries(Object.keys(env).map((k) => [k, process.env[k]]));
  for (const [k, v] of Object.entries(env)) {
    if (v === undefined) delete process.env[k];
    else process.env[k] = v;
  }
  return fn().finally(() => {
    for (const [k, v] of Object.entries(before)) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
  });
}

test("plugin: the build is a package DIRECTORY with one dependency-free ESM entry", async () => {
  const { dir, tmp } = await built();
  try {
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8")), GUARD_PACKAGE_JSON);
    const code = fs.readFileSync(path.join(dir, "index.js"), "utf8");
    // Only node built-ins stay imports: the binary's Bun cannot see node_modules.
    const imports = [...code.matchAll(/^import .* from "([^"]+)";$/gm)].map((m) => m[1]);
    assert.ok(imports.length > 0 && imports.every((m) => m.startsWith("node:")), `bundle imports ${imports.join(", ")}`);
    assert.doesNotMatch(code, /claude-agent-sdk/);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test("plugin: it announces itself, then refuses with the platform's own sentences", async () => {
  const { mod, tmp } = await built();
  const workspace = path.join(tmp, "proj");
  fs.mkdirSync(workspace);
  const secrets = path.join(tmp, "secrets.json");
  fs.writeFileSync(secrets, JSON.stringify([SECRET]));
  const ready = path.join(tmp, "ready");
  const appendix = path.join(tmp, "instructions.md");
  fs.writeFileSync(appendix, "WORKFLOW");
  try {
    await withEnv(
      {
        [GUARD_ENV.workspace]: workspace,
        [GUARD_ENV.secretsFile]: secrets,
        [GUARD_ENV.readyFile]: ready,
        [GUARD_ENV.appendixFile]: appendix,
        [GUARD_ENV.sessionLog]: path.join(tmp, "logs", "session-context.jsonl"),
        [GUARD_ENV.probeFile]: path.join(tmp, "probe"),
      },
      async () => {
        const hooks = await mod.AepGuard();
        assert.ok(fs.existsSync(ready), "no ready marker");
        const before = hooks["tool.execute.before"];
        const call = { sessionID: "s", callID: "c" };

        // Outside the project: the marked sentence, word for word the Claude hook's.
        const outside = "/definitely/elsewhere/leak.txt";
        await assert.rejects(before({ tool: "write", ...call }, { args: { filePath: outside, content: "x" } }), (err: Error) => {
          assert.equal(err.message, `${WORKSPACE_GUARD_MARKER} ${authoredPathDenial("write", outside, workspace)}`);
          return true;
        });
        // Inside, and a relative path: allowed.
        await before({ tool: "edit", ...call }, { args: { filePath: path.join(workspace, "a.ts") } });
        await before({ tool: "write", ...call }, { args: { filePath: "src/b.ts" } });
        // A patch names its files in its text; one outside is enough to refuse.
        await assert.rejects(
          before({ tool: "apply_patch", ...call }, { args: { patchText: `*** Begin Patch\n*** Add File: ${outside}\n+x\n*** End Patch` } }),
          /Refusing to write outside the project/,
        );
        // The egress guards, fed the staged secret through the file.
        await assert.rejects(before({ tool: "websearch", ...call }, { args: { query: `how to use ${SECRET}` } }), (err: Error) => {
          assert.equal(err.message, WEBSEARCH_DENIAL_MESSAGE);
          return true;
        });
        await assert.rejects(before({ tool: "webfetch", ...call }, { args: { url: "http://127.0.0.1:9/health" } }), /WebFetch URL blocked/);
        await before({ tool: "webfetch", ...call }, { args: { url: "https://example.com/docs" } });
        // Everything else passes untouched.
        await before({ tool: "bash", ...call }, { args: { command: "echo hi > /tmp/x" } });
      },
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

// A plugin that throws at init is one OpenCode does not load — so an incomplete
// environment must throw BEFORE the marker, and the run is refused at start.
test("plugin: missing inputs fail its init, and no marker is written", async () => {
  const { mod, tmp } = await built();
  const ready = path.join(tmp, "ready");
  try {
    await withEnv(
      { [GUARD_ENV.workspace]: tmp, [GUARD_ENV.secretsFile]: undefined, [GUARD_ENV.readyFile]: ready },
      async () => {
        await assert.rejects(mod.AepGuard(), /AEP_GUARD_SECRETS_FILE is not set/);
        assert.ok(!fs.existsSync(ready));
      },
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test("plugin: the appendix stays with the lead, and each session's context is recorded", async () => {
  const { mod, tmp } = await built();
  const secrets = path.join(tmp, "secrets.json");
  fs.writeFileSync(secrets, "[]");
  const appendix = path.join(tmp, "instructions.md");
  const body = "# Your workflow\n\n<skill name=\"aep\">\nfan out\n</skill>\n\n## Tool glossary\n";
  fs.writeFileSync(appendix, body);
  const log = path.join(tmp, "logs", "session-context.jsonl");
  const system = (): string[] => [`BASE\n<env>\n</env>\nInstructions from: ${appendix}\n${body}\nSkills provide specialized instructions`];
  try {
    await withEnv(
      {
        [GUARD_ENV.workspace]: tmp,
        [GUARD_ENV.secretsFile]: secrets,
        [GUARD_ENV.readyFile]: path.join(tmp, "ready"),
        [GUARD_ENV.appendixFile]: appendix,
        [GUARD_ENV.sessionLog]: log,
        [GUARD_ENV.probeFile]: path.join(tmp, "probe"),
      },
      async () => {
        const hooks = await mod.AepGuard();
        await hooks["chat.message"]({ sessionID: "ses_lead" }, { message: { agent: "aep" } });
        await hooks["chat.message"]({ sessionID: "ses_child" }, { message: { agent: "general" } });

        const lead = { system: system() };
        await hooks["experimental.chat.system.transform"]({ sessionID: "ses_lead" }, lead);
        assert.deepEqual(lead.system, system());

        const child = { system: system() };
        await hooks["experimental.chat.system.transform"]({ sessionID: "ses_child" }, child);
        assert.deepEqual(child.system, ["BASE\n<env>\n</env>\nSkills provide specialized instructions"]);

        await hooks["tool.execute.before"]({ tool: "skill", sessionID: "ses_child", callID: "c" }, { args: { name: "ballerina" } });

        const lines = fs
          .readFileSync(log, "utf8")
          .trim()
          .split("\n")
          .map((l) => {
            const { ts, ...rest } = JSON.parse(l) as Record<string, unknown>;
            assert.equal(typeof ts, "string");
            return rest;
          });
        assert.deepEqual(lines, [
          { session: "ses_lead", agent: "aep", appendix: true },
          { session: "ses_child", agent: "general", appendix: false },
          { session: "ses_child", skill: "ballerina" },
        ]);
      },
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test("plugin: the startup probe marks the system transform and never reaches a model call", async () => {
  const { mod, tmp } = await built();
  const secrets = path.join(tmp, "secrets.json");
  fs.writeFileSync(secrets, "[]");
  const appendix = path.join(tmp, "instructions.md");
  fs.writeFileSync(appendix, "WORKFLOW");
  const log = path.join(tmp, "logs", "session-context.jsonl");
  const probe = path.join(tmp, "probe");
  try {
    await withEnv(
      {
        [GUARD_ENV.workspace]: tmp,
        [GUARD_ENV.secretsFile]: secrets,
        [GUARD_ENV.readyFile]: path.join(tmp, "ready"),
        [GUARD_ENV.appendixFile]: appendix,
        [GUARD_ENV.sessionLog]: log,
        [GUARD_ENV.probeFile]: probe,
      },
      async () => {
        const hooks = await mod.AepGuard();
        // A real session's model call goes ahead, and is no probe.
        await hooks["chat.message"]({ sessionID: "ses_lead" }, { message: { agent: "aep" }, parts: [{ type: "text", text: "build it" }] });
        await hooks["experimental.chat.system.transform"]({ sessionID: "ses_lead" }, { system: ["BASE"] });
        await hooks["chat.params"]({ sessionID: "ses_lead" });
        assert.ok(!fs.existsSync(probe));

        await hooks["chat.message"](
          { sessionID: "ses_probe" },
          { message: { agent: "aep" }, parts: [{ type: "text", text: STARTUP_PROBE_PROMPT }] },
        );
        await hooks["experimental.chat.system.transform"]({ sessionID: "ses_probe" }, { system: ["BASE"] });
        assert.ok(fs.existsSync(probe), "the transform did not mark the probe");
        await assert.rejects(hooks["chat.params"]({ sessionID: "ses_probe" }), /startup probe, no model call/);

        // The probe is not a session of the run: nothing about it is recorded.
        const sessions = fs.readFileSync(log, "utf8").trim().split("\n").map((l) => (JSON.parse(l) as { session: string }).session);
        assert.deepEqual(sessions, ["ses_lead"]);
      },
    );
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test("guard decision: ignores tools it does not own and calls with no path", () => {
  const decide = createGuardDecision({ workspace: "/w", secrets: [] });
  assert.equal(decide("read", { filePath: "/etc/passwd" }), null, "reads are deliberately not gated");
  assert.equal(decide("write", {}), null);
  assert.equal(decide("write", "garbage"), null);
});

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

// The runtime's own decisions, without a server: the child environment, and
// the refusals that happen before one is spawned. The server path is exercised
// model-free against the real binary in the image (ADR-0015), not here — this
// suite has no `opencode` on its PATH, and a test that needed one would pass on
// one machine and skip on the next.

import { test } from "node:test";
import assert from "node:assert/strict";
import { DENIED_CAPABILITIES, type RuntimePolicy } from "../port.js";
import { childEnvironment, createOpencodeRuntime, OPENCODE_GUARD_DIR } from "./runtime.js";
import { OpencodeStartupError } from "./startup.js";

function policy(env: Record<string, string>): RuntimePolicy {
  return {
    workspace: "/work/proj",
    env,
    model: "claude-sonnet-5",
    taskKind: "implementation",
    debug: false,
    logDir: "/work/proj/.logs",
    write: { allowOutsideProject: () => false },
    deniedCapabilities: DENIED_CAPABILITIES,
    webSearch: { deny: () => null },
    webFetch: { deny: () => null },
    skills: { dir: "/work/proj/.claude/skills", allow: ["aep"], preloadBodies: "WORKFLOW" },
  };
}

test("childEnvironment: the policy's env, the runtime's four flags, and the guard's inputs", () => {
  const env = childEnvironment(policy({ PATH: "/bin", ANTHROPIC_API_KEY: "k" }), {
    secrets: "/t/s.json",
    ready: "/t/r",
    instructions: "/t/instructions.md",
    probe: "/t/p",
  });
  assert.equal(env.PATH, "/bin");
  assert.equal(env.ANTHROPIC_API_KEY, "k");
  for (const flag of [
    "OPENCODE_DISABLE_PROJECT_CONFIG",
    "OPENCODE_DISABLE_AUTOUPDATE",
    "OPENCODE_DISABLE_MODELS_FETCH",
    "OPENCODE_DISABLE_LSP_DOWNLOAD",
  ]) {
    assert.equal(env[flag], "1", flag);
  }
  assert.equal(env.AEP_GUARD_WORKSPACE, "/work/proj");
  assert.equal(env.AEP_GUARD_SECRETS_FILE, "/t/s.json");
  assert.equal(env.AEP_GUARD_READY, "/t/r");
  assert.equal(env.AEP_GUARD_APPENDIX, "/t/instructions.md");
  assert.equal(env.AEP_GUARD_PROBE_MARKER, "/t/p");
  assert.equal(env.AEP_GUARD_SESSION_LOG, "/work/proj/.logs/session-context.jsonl");
  // Decided: never set by the platform. It turns `.claude/skills/` discovery off.
  assert.equal(env.OPENCODE_DISABLE_CLAUDE_CODE_SKILLS, undefined);
  assert.equal(env.OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS, undefined);
});

// A leaked flag is FAILED at start (startup.ts reads the task schema), not
// silently removed here — removing it would hide the leak and keep the run.
test("childEnvironment: a background flag in the pod's env is passed through for the assertion to catch", () => {
  const env = childEnvironment(policy({ OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS: "true" }), { secrets: "s", ready: "r", instructions: "i", probe: "p" });
  assert.equal(env.OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS, "true");
});

test("start: a run with no API key is refused before anything is spawned", async () => {
  const runtime = createOpencodeRuntime({ command: "/nonexistent/opencode" });
  await assert.rejects(runtime.start("go", policy({ CLAUDE_CODE_OAUTH_TOKEN: "sk-ant-oat-x" })), (err: unknown) => {
    assert.ok(err instanceof OpencodeStartupError);
    assert.match(err.message, /an OAuth coding token cannot run OpenCode/);
    return true;
  });
});

test("start: a binary that is not there fails the start, and leaves nothing behind", async () => {
  const runtime = createOpencodeRuntime({ command: "/nonexistent/opencode" });
  await assert.rejects(runtime.start("go", policy({ ANTHROPIC_API_KEY: "k", PATH: "/usr/bin:/bin" })), /ENOENT|spawn/);
});

test("createOpencodeRuntime: the image's plugin directory is the default", () => {
  assert.equal(OPENCODE_GUARD_DIR, "/app/runtime/opencode/aep-guard");
  assert.equal(createOpencodeRuntime().name, "opencode");
});

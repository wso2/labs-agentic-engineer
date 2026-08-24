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

import { test } from "node:test";
import assert from "node:assert/strict";
import os from "node:os";
import fs from "node:fs";
import path from "node:path";
import {
  AGENT_SETTING_SOURCES,
  DISALLOWED_TOOLS,
  alwaysOnSkills,
  buildMcpOptions,
  contractReferencePath,
  debugQueryOptions,
  onDemandSkills,
  promptWithProjectRoot,
} from "./runner.js";
import { MissingWorkflowSkillError, requireWorkflowBodies } from "./skills_presence.js";

// D9 secure search (Task 12) — WebSearch joins the base tool set (gated by
// the PreToolUse DLP hook wired in runClaudeQuery; see websearch_dlp.ts).
// WebFetch joins it too (see webfetch_guard.ts's PreToolUse SSRF + secret
// guard, wired the same way) — fail-closed, so this is safe to enable.
// Agent joins it for the milestone run loop's subagent fan-out (design §9.3).
// It is `Agent`, not `Task`: SDK 0.3.220 declares AgentInput and no TaskInput,
// so the old name named nothing — and because bypassPermissions ignores this
// list entirely, that mismatch could not fail loudly. Hence the pin.
const BASE_TOOLS = ["Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "Agent"];
const MCP_TOOLS = [
  "mcp__aep__list_org_component_endpoints",
  "mcp__aep__get_remote_git_file_contents",
  "mcp__aep__search_remote_git_code",
];

test("buildMcpOptions: registers the aep MCP server and tools when both envs are set", () => {
  const result = buildMcpOptions("https://bff.example.com/internal/v1/mcp", "mcp-token-xyz");

  assert.deepEqual(result.mcpServers, {
    aep: {
      type: "http",
      url: "https://bff.example.com/internal/v1/mcp",
      headers: { Authorization: "Bearer mcp-token-xyz" },
    },
  });
  assert.deepEqual(result.allowedTools, [...BASE_TOOLS, ...MCP_TOOLS]);
});

test("buildMcpOptions: omits mcpServers and MCP tools when the token is missing", () => {
  const result = buildMcpOptions("https://bff.example.com/internal/v1/mcp", undefined);

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: omits mcpServers and MCP tools when the url is missing", () => {
  const result = buildMcpOptions(undefined, "mcp-token-xyz");

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: omits mcpServers and MCP tools when both are empty strings", () => {
  const result = buildMcpOptions("", "");

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: omits mcpServers and MCP tools when both are undefined", () => {
  const result = buildMcpOptions(undefined, undefined);

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: allowedTools includes both WebSearch and WebFetch (D9)", () => {
  const result = buildMcpOptions(undefined, undefined);

  assert.ok(result.allowedTools.includes("WebSearch"));
  assert.ok(result.allowedTools.includes("WebFetch"));
});

// The milestone run loop fans big, independent issues out to subagents; without
// Agent in allowedTools the `aep` skill's fan-out section names a tool the
// intended surface does not include.
test("buildMcpOptions: allowedTools includes Agent, with and without MCP", () => {
  assert.ok(buildMcpOptions(undefined, undefined).allowedTools.includes("Agent"));
  assert.ok(
    buildMcpOptions("https://bff.example.com/internal/v1/mcp", "mcp-token-xyz").allowedTools.includes("Agent"),
  );
  // The retired name must not creep back: it is the one that silently named
  // nothing for a whole SDK generation.
  assert.ok(!buildMcpOptions(undefined, undefined).allowedTools.includes("Task"));
});

// Subagents inherit the parent's allowedTools, so the git tools stay in the set
// and the main-agent-is-sole-git-writer rule is enforced by the skill's
// deny-list, not by the tool list. Pinned so a future "just drop Bash for
// subagents" idea has to confront that the seam does not exist here.
test("buildMcpOptions: Bash stays in the base set alongside Agent", () => {
  const tools = buildMcpOptions(undefined, undefined).allowedTools;
  assert.ok(tools.includes("Bash"));
  assert.ok(tools.includes("Agent"));
});

// --- alwaysOnSkills: the run's own workflow is not the design's to choose ----

// Every other skill a build reads is a `skillsPinned` entry someone put in a
// design.json. This list is not: no design decides whether a coding run follows
// the coding workflow, and a validation run's workflow REPLACES it rather than
// adding to it.
test("alwaysOnSkills: an implementation run is steered by aep, a validation run by both", () => {
  assert.deepEqual(alwaysOnSkills("implementation"), ["aep"]);
  assert.deepEqual(alwaysOnSkills("validation"), ["aep", "aep-validation"]);
});

// playwright-cli carries the browser mechanics a validation run reaches for, and
// `aep-validation` names it by description. Paying for its body on every turn of
// every validation run is what NOT listing it here buys.
test("alwaysOnSkills: playwright-cli is left to on-demand loading", () => {
  assert.ok(!alwaysOnSkills("validation").includes("playwright-cli"));
});

// The other half of that sentence. `skills:` is an allowlist, so a skill in
// NEITHER list is not deferred — it is unreachable, and the Skill tool rejects
// the load `aep-validation` instructs. Absent from always-on AND present here is
// the pair that means "loadable, but not on every turn".
test("onDemandSkills: a validation run may load playwright-cli", () => {
  assert.deepEqual(onDemandSkills("validation"), ["playwright-cli"]);
  assert.ok(!alwaysOnSkills("validation").includes("playwright-cli"));
});

// An implementation run gets the whole mirror instead (oneshot's else branch):
// it may need any stack skill a design.json pinned, which is not a list this
// module can know. Naming anything here would be a second, competing source.
test("onDemandSkills: an implementation run names nothing", () => {
  assert.deepEqual(onDemandSkills("implementation"), []);
});

// --- requireWorkflowBodies: a run with no procedure must not start -----------

function withMirror<T>(skills: Record<string, string>, fn: (workspace: string) => T): T {
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "aep-mirror-"));
  try {
    for (const [name, body] of Object.entries(skills)) {
      fs.mkdirSync(path.join(workspace, ".claude", "skills", name), { recursive: true });
      fs.writeFileSync(path.join(workspace, ".claude", "skills", name, "SKILL.md"), body);
    }
    return fn(workspace);
  } finally {
    fs.rmSync(workspace, { recursive: true, force: true });
  }
}

// The mirror's writes are best-effort by design — none of them may fail a project
// creation, a publish or a dispatch. So the one case that cannot degrade is
// caught here instead: a session with no workflow skill does not do a smaller
// version of the job, it improvises one and reports success.
test("requireWorkflowBodies: a mirror with no aep skill is fatal", () => {
  withMirror({ go: "---\nname: go\n---\n\nGo rules\n" }, (workspace) => {
    assert.throws(() => requireWorkflowBodies(workspace, ["aep"]), (err: unknown) => {
      assert.ok(err instanceof MissingWorkflowSkillError);
      assert.deepEqual(err.missing, ["aep"]);
      // The message has to name the cause, because the person reading it in a
      // build log is not the person who broke the sync.
      assert.match(err.message, /skill sync did not reach/);
      return true;
    });
  });
});

test("requireWorkflowBodies: a validation run missing only aep-validation is still fatal", () => {
  withMirror({ aep: "---\nname: aep\n---\n\nThe run\n" }, (workspace) => {
    assert.throws(
      () => requireWorkflowBodies(workspace, ["aep", "aep-validation"]),
      (err: unknown) => err instanceof MissingWorkflowSkillError && err.missing.length === 1,
    );
  });
});

// The bodies are what actually reach the model — `skills:` carries names and
// descriptions only, so this string IS the delivery mechanism for the procedure.
test("requireWorkflowBodies: present skills come back fenced and labelled as loaded", () => {
  withMirror(
    {
      aep: "---\nname: aep\n---\n\nCODEWORD-RUN\n",
      "aep-validation": "---\nname: aep-validation\n---\n\nCODEWORD-VALIDATION\n",
    },
    (workspace) => {
      const out = requireWorkflowBodies(workspace, ["aep", "aep-validation"]);
      assert.match(out, /CODEWORD-RUN/);
      assert.match(out, /CODEWORD-VALIDATION/);
      assert.match(out, /<skill name="aep">/);
      assert.match(out, /<skill name="aep-validation">/);
      // Without this the agent re-invokes the Skill tool for guidance it already
      // has and pays for the body twice.
      assert.match(out, /ALREADY in your context/);
    },
  );
});

// --- DISALLOWED_TOOLS: the boundary that survives bypassPermissions ---------

// allowedTools restricts nothing in this run (bypassPermissions +
// allowDangerouslySkipPermissions allow every harness tool), so this list is the
// only real boundary. Pinned because the failure it prevents is quiet: a run
// reached for ScheduleWakeup to wait on its own detached subagents, spent a turn
// on a schema error, and exited anyway.
test("DISALLOWED_TOOLS: blocks the session-management tools a one-shot pod cannot use", () => {
  for (const name of ["ScheduleWakeup", "Monitor", "AskUserQuestion", "Workflow", "CronCreate", "SendMessage"]) {
    assert.ok(DISALLOWED_TOOLS.includes(name), `${name} must stay disallowed`);
  }
});

// The run is the agent doing the work; blocking its working tools would end it.
test("DISALLOWED_TOOLS: never blocks a tool the run needs", () => {
  for (const name of buildMcpOptions(undefined, undefined).allowedTools) {
    assert.ok(!DISALLOWED_TOOLS.includes(name), `${name} is both allowed and disallowed`);
  }
});

// --- promptWithProjectRoot -------------------------------------------------

test("promptWithProjectRoot: names the absolute root and keeps the caller's prompt intact", () => {
  const out = promptWithProjectRoot("Work the issues in this project. Follow the `aep` skill", "/workspace/project");
  assert.match(out, /\/workspace\/project/);
  // The caller's prompt is the subject of the run; prefixing must not reword it.
  assert.ok(out.endsWith("Work the issues in this project. Follow the `aep` skill"));
});

test("promptWithProjectRoot: the platform's own workspace shape survives it", () => {
  // WORKSPACE_BASE_PATH/<org>/<project>/<taskId> — the value only exists after
  // provisionWorkspace, which is why neither prompt builder can state it.
  const root = "/aep-workspace/acme/todo/11111111-2222-3333-4444-555555555555";
  assert.match(promptWithProjectRoot("Work the issues for milestone 4", root), new RegExp(root));
});

// A fan-out subagent has no skill of its own, so the lead hands it the contract
// as an absolute path. A lead that has to TRANSCRIBE one gets it wrong: the first
// playground run of the reference split pasted `/run/base-plugin/…` to one of two
// subagents, dropping the workspace prefix — the read failed and the subagent fell
// to scanning `/` for the file. The prompt now carries the exact string to copy.
test("promptWithProjectRoot: states the contract path for the lead to hand on", () => {
  const contract = contractReferencePath("/workspace/project");
  // Inside the mirror, like every other skill file — so a subagent reads the same
  // bytes the lead does, and a developer who clones the repo can read them too.
  assert.equal(contract, path.join("/workspace/project", ".claude", "skills", "aep", "references", "component-contract.md"));
  const out = promptWithProjectRoot("Work the issues", "/workspace/project", contract);
  assert.match(out, /\.claude\/skills\/aep\/references\/component-contract\.md/);
  assert.match(out, /hand that exact path to every subagent/);
  assert.ok(out.endsWith("Work the issues"));
});

test("promptWithProjectRoot: omitting the contract path leaves the prompt as it was", () => {
  // The platform's Go prompt builder and the playground's both go through
  // runClaudeQuery, which always passes it — but the seam stays optional so a
  // caller with no mirror cannot be broken by this.
  const out = promptWithProjectRoot("Work the issues", "/workspace/project");
  assert.ok(!out.includes("component-contract.md"));
  assert.ok(out.endsWith("Work the issues"));
});

test("AGENT_SETTING_SOURCES admits the project source, and only that one", () => {
  // Verified against the real SDK: with [] a skill in the clone's
  // .claude/skills/ is absent from the init message's resolved list; with
  // ["project"] it is present. Dropping 'project' silently un-ships the whole
  // mirror, so this is the guard, not a restatement.
  assert.deepEqual([...AGENT_SETTING_SOURCES], ["project"]);
  // 'user' is a developer's ~/.claude and 'local' their personal overrides —
  // neither belongs in a dispatched container run.
  assert.ok(!AGENT_SETTING_SOURCES.includes("user" as never));
  assert.ok(!AGENT_SETTING_SOURCES.includes("local" as never));
});

test("debugQueryOptions: a normal run carries NONE of the developer options", () => {
  // The boundary this whole split exists for. debugFile holds prompt text and
  // includePartialMessages multiplies the message count by the token count, so
  // "absent by default" is the property worth pinning — and an integration test
  // against a live session could not assert an absence.
  assert.deepEqual(debugQueryOptions(undefined), {});
});

test("debugQueryOptions: a debug run wires every developer option at the sinks it was given", () => {
  const written: string[] = [];
  const opts = debugQueryOptions({
    debugFilePath: "/run/.logs/claude-debug.log",
    onStderr: (c) => written.push(c),
    close: () => {},
  });
  assert.equal(opts.includePartialMessages, true);
  assert.equal(opts.debugFile, "/run/.logs/claude-debug.log");
  // Routed through the sink rather than to a stream of its own, which is what
  // gets it scrubbed on the way to disk.
  opts.stderr?.("boom");
  assert.deepEqual(written, ["boom"]);
});

test("debugQueryOptions: the reasoning pair is on together, or not at all", () => {
  // They answer one question between them — what did this run think, including
  // the subagents that did the work — and either alone leaves the transcript
  // unable to answer it: no display gives signed-but-empty blocks, and no
  // forwarding gives them for the lead session only. ADR-0002 decision 16.
  const opts = debugQueryOptions({
    debugFilePath: "/run/.logs/claude-debug.log",
    onStderr: () => {},
    close: () => {},
  });
  assert.deepEqual(opts.thinking, { type: "adaptive", display: "summarized" });
  assert.equal(opts.forwardSubagentText, true);

  const normal = debugQueryOptions(undefined);
  assert.equal(normal.thinking, undefined);
  assert.equal(normal.forwardSubagentText, undefined);
});

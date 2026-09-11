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
import { debugQueryOptions } from "../../lib/logger.js";
import {
  AGENT_SETTING_SOURCES,
  CLAUDE_CODE_DEFAULT_MODEL,
  createClaudeCodeRuntime,
  openPromptStream,
  watchHook,
} from "./runtime.js";
import { BASE_ALLOWED_TOOLS, buildMcpOptions, deniedTools, namespacedMcpTool } from "./tools.js";
import { DENIED_CAPABILITIES, type DeniedCapability } from "../port.js";

// D9 secure search (Task 12) — WebSearch joins the base tool set (gated by the
// PreToolUse DLP hook this adapter wires from `RuntimePolicy.webSearch`; see
// websearch_dlp.ts). WebFetch joins it too (webfetch_guard.ts's SSRF + secret
// guard, wired the same way) — fail-closed, so this is safe to enable.
// Agent joins it for the milestone run loop's subagent fan-out (design §9.3).
// It is `Agent`, not `Task`: SDK 0.3.220 declares AgentInput and no TaskInput,
// so the old name named nothing — and because bypassPermissions ignores this
// list entirely, that mismatch could not fail loudly. Hence the pin.
const BASE_TOOLS = ["Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "Agent"];
// The bare names the platform's MCP server exposes, as `lib/runner.ts` states
// them on the policy. Namespacing them is this adapter's convention, which is
// the whole reason the port passes them bare.
const MCP_BARE_TOOLS = [
  "list_org_component_endpoints",
  "get_remote_git_file_contents",
  "search_remote_git_code",
];
const MCP_TOOLS = MCP_BARE_TOOLS.map(namespacedMcpTool);

test("buildMcpOptions: registers the aep MCP server and tools when both envs are set", () => {
  const result = buildMcpOptions("https://bff.example.com/internal/v1/mcp", "mcp-token-xyz", MCP_BARE_TOOLS);

  assert.deepEqual(result.mcpServers, {
    aep: {
      type: "http",
      url: "https://bff.example.com/internal/v1/mcp",
      headers: { Authorization: "Bearer mcp-token-xyz" },
    },
  });
  assert.deepEqual(result.allowedTools, [...BASE_TOOLS, ...MCP_TOOLS]);
});

// `mcp__<server>__<tool>` is the SDK's own spelling, and an allowlist entry that
// misses it matches nothing — silently, because bypassPermissions ignores the
// allowlist anyway. Pinned so the rendering cannot drift from the convention.
test("buildMcpOptions: the platform's bare tool names are namespaced for this runtime", () => {
  assert.equal(namespacedMcpTool("search_remote_git_code"), "mcp__aep__search_remote_git_code");
  const result = buildMcpOptions("https://bff.example.com/internal/v1/mcp", "t", MCP_BARE_TOOLS);
  for (const bare of MCP_BARE_TOOLS) {
    assert.ok(!result.allowedTools.includes(bare), `${bare} must not reach the allowlist un-namespaced`);
    assert.ok(result.allowedTools.includes(`mcp__aep__${bare}`));
  }
});

test("buildMcpOptions: omits mcpServers and MCP tools when the token is missing", () => {
  const result = buildMcpOptions("https://bff.example.com/internal/v1/mcp", undefined, MCP_BARE_TOOLS);

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: omits mcpServers and MCP tools when the url is missing", () => {
  const result = buildMcpOptions(undefined, "mcp-token-xyz", MCP_BARE_TOOLS);

  assert.equal(result.mcpServers, undefined);
  assert.deepEqual(result.allowedTools, BASE_TOOLS);
});

test("buildMcpOptions: omits mcpServers and MCP tools when both are empty strings", () => {
  const result = buildMcpOptions("", "", MCP_BARE_TOOLS);

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
    buildMcpOptions("https://bff.example.com/internal/v1/mcp", "mcp-token-xyz", MCP_BARE_TOOLS).allowedTools.includes(
      "Agent",
    ),
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

// The allowlist is documentation of intent, and a caller must not be able to
// mutate the shared constant through it.
test("buildMcpOptions: hands back its own array, not the shared base list", () => {
  const tools = buildMcpOptions(undefined, undefined).allowedTools;
  tools.push("Mutated");
  assert.ok(!BASE_ALLOWED_TOOLS.includes("Mutated"));
});

// --- the deny list: the boundary that survives bypassPermissions ------------

// allowedTools restricts nothing in this run (bypassPermissions +
// allowDangerouslySkipPermissions allow every harness tool), so this list is the
// only real boundary. Pinned because the failure it prevents is quiet: a run
// reached for ScheduleWakeup to wait on its own detached subagents, spent a turn
// on a schema error, and exited anyway.
test("deniedTools: blocks the session-management tools a one-shot pod cannot use", () => {
  const denied = deniedTools(DENIED_CAPABILITIES);
  for (const name of ["ScheduleWakeup", "Monitor", "AskUserQuestion", "Workflow", "CronCreate", "SendMessage"]) {
    assert.ok(denied.includes(name), `${name} must stay disallowed`);
  }
});

// The run is the agent doing the work; blocking its working tools would end it.
test("deniedTools: never blocks a tool the run needs", () => {
  const denied = deniedTools(DENIED_CAPABILITIES);
  for (const name of buildMcpOptions(undefined, undefined).allowedTools) {
    assert.ok(!denied.includes(name), `${name} is both allowed and disallowed`);
  }
});

// The port names CLASSES so the platform's policy can be stated without naming a
// runtime; this table is the only thing that turns one into names. A class with
// no entry would be a policy clause that silently enforces nothing.
test("deniedTools: every capability class this runtime is asked about denies something", () => {
  for (const capability of DENIED_CAPABILITIES) {
    assert.ok(deniedTools([capability]).length > 0, `${capability} maps to no Claude Code tool`);
  }
});

// A run that denied nothing would be indistinguishable from one whose policy was
// dropped on the way through, and `disallowedTools` is the option where that
// mistake is invisible.
test("deniedTools: an empty policy denies nothing, and the whole policy denies more", () => {
  assert.deepEqual(deniedTools([]), []);
  const one = deniedTools(["scheduling"] as DeniedCapability[]);
  assert.ok(deniedTools(DENIED_CAPABILITIES).length > one.length);
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

// --- the runtime itself ------------------------------------------------------

test("createClaudeCodeRuntime: answers to the name the org setting and the env use", () => {
  // One spelling across the contract's AgentRuntime enum, AEP_AGENT_RUNTIME, the
  // glossary table's key and this — a mismatch anywhere is a session with no
  // glossary, which fails as prose rather than as an error.
  assert.equal(createClaudeCodeRuntime().name, "claude-code");
});

// The default has to be a model `model_rates` prices, or every run made by an
// org that never opened the setting loses its cost stamp — `modelcost.SumCost`
// is all-or-nothing across a capture.
test("createClaudeCodeRuntime: the default model is the one the platform seeds a rate for", () => {
  assert.equal(createClaudeCodeRuntime().defaultModel, "claude-sonnet-5");
  assert.equal(CLAUDE_CODE_DEFAULT_MODEL, "claude-sonnet-5");
});

// The `aep` skill points at the glossary BY POSITION, and this runtime is what
// supplies its text. Every role the workflow's prose defers to has to be bound,
// or the agent resolves it by guessing a tool name.
test("createClaudeCodeRuntime: the glossary binds the fan-out, wait and task-list roles", () => {
  const glossary = createClaudeCodeRuntime().toolGlossary();

  assert.match(glossary, /fan-out tool.*`Agent`/);
  assert.match(glossary, /`run_in_background: true`/);
  assert.match(glossary, /wait tool.*`TaskOutput`/);
  assert.match(glossary, /task list.*`TaskCreate`/);
  // The skill says "the fast model" and "the default one" and leaves the aliases
  // to this table; a lead that guesses one spends a turn on a schema error.
  assert.match(glossary, /`haiku` \(the fast model\)/);
  assert.match(glossary, /`sonnet` \(the default\)/);
  // And ONLY models the platform can price. modelcost.SumCost is all-or-nothing:
  // one slice whose model has no rate row makes the whole cycle's cost null. So
  // offering an alias with no seeded rate turns the skill's own "pick the model
  // for the job" into a silent way to lose a cycle's spend. This offered `opus`
  // when only sonnet and haiku were seeded.
  assert.doesNotMatch(glossary, /opus/i);
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
    stderrFilePath: "/run/.logs/claude-stderr.log",
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
    stderrFilePath: "/run/.logs/claude-stderr.log",
    onStderr: () => {},
    close: () => {},
  });
  assert.deepEqual(opts.thinking, { type: "adaptive", display: "summarized" });
  assert.equal(opts.forwardSubagentText, true);
});


// --- the prompt stream ----------------------------------------------------------
//
// The SDK ends the CLI's stdin when a string prompt's first result arrives, and
// stdin is the hook channel. Held open as a stream, it ends only when released.

test("openPromptStream: yields the prompt once and stays open until released", async () => {
  const { stream, release } = openPromptStream("build it");
  const it = stream[Symbol.asyncIterator]();
  const first = await it.next();
  assert.equal(first.done, false);
  assert.deepEqual(first.value, { type: "user", message: { role: "user", content: "build it" }, parent_tool_use_id: null, session_id: "" });

  const pending = Symbol("pending");
  const second = it.next();
  const raced = await Promise.race([second, new Promise((r) => setTimeout(() => r(pending), 30))]);
  assert.equal(raced, pending, "the second read must not settle on its own");

  release();
  release(); // idempotent
  assert.equal((await second).done, true);
});

// --- watchHook --------------------------------------------------------------

const preToolUse = (toolName = "Bash") =>
  ({ hook_event_name: "PreToolUse", tool_name: toolName, tool_input: {}, tool_use_id: "tu_1" }) as never;

const fireHook = (hook: ReturnType<typeof watchHook>, input: unknown = preToolUse()) =>
  hook(input as never, undefined, { signal: undefined } as never);

// The regression pin for `RuntimeObservers.toolUse` returning a promise. The
// validation status line posts from that seam, and its whole value is that the
// line explaining a twenty-minute silence lands BEFORE the silence — which only
// holds while this adapter awaits the watcher, because the SDK dispatches the
// tool call the moment this callback resolves.
test("watchHook: a watcher that reaches the outside world is awaited before the call", async () => {
  let posted = false;
  const hook = watchHook(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10));
    posted = true;
  });

  await fireHook(hook);

  assert.equal(posted, true, "the call would have been dispatched while the line was still unposted");
});

// A watcher is not a decision — see RuntimeObservers — so whatever it answers,
// the hook answers the SDK with an empty decision.
test("watchHook: watching never decides", async () => {
  const seen: string[] = [];
  const hook = watchHook((toolName) => void seen.push(toolName));

  assert.deepEqual(await fireHook(hook, preToolUse("Write")), {});
  assert.deepEqual(seen, ["Write"]);
});

// Every other hook event reaches the same callback, and a watcher derived from
// one would report a tool call that is not happening.
test("watchHook: a non-PreToolUse event is not a tool call", async () => {
  let calls = 0;
  const hook = watchHook(() => void (calls += 1));

  await fireHook(hook, { hook_event_name: "PostToolUse", tool_name: "Bash", tool_use_id: "tu_1" });

  assert.equal(calls, 0);
});

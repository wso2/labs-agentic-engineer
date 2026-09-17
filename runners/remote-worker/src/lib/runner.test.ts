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
  alwaysOnSkills,
  contractReferencePath,
  onDemandSkills,
  promptWithProjectRoot,
  systemPromptAppend,
} from "./runner.js";
import { toolGlossary } from "./tool_glossary.js";
import { MissingWorkflowSkillError, requireWorkflowBodies } from "./skills_presence.js";
import type { DispatchRequest } from "./types.js";

// What is NOT here any more: the MCP option builder, the deny list, the setting
// sources and the debug options all moved to `runtime/claude/runtime.test.ts`
// with the code they pin. This file is what `lib/runner.ts` still decides —
// which is, deliberately, only things a second runtime would decide the same
// way.

// --- the issue status line: three ways to have none, all of them normal ------

// --- alwaysOnSkills: the run's own workflow is not the design's to choose ----

// Every other skill a build reads is a `skillsPinned` entry someone put in a
// design.json. This list is not: no design decides whether a coding run follows
// the coding workflow, and a validation run's workflow REPLACES it rather than
// adding to it.
test("alwaysOnSkills: an implementation run is steered by aep, a validation run by both", () => {
  assert.deepEqual(alwaysOnSkills("implementation"), ["aep"]);
  assert.deepEqual(alwaysOnSkills("validation"), ["aep", "acceptance-run"]);
});

// agent-browser carries the browser mechanics a validation run reaches for, and
// `acceptance-run` names it by description. Paying for its body on every turn of
// every validation run is what NOT listing it here buys.
test("alwaysOnSkills: agent-browser is left to on-demand loading", () => {
  assert.ok(!alwaysOnSkills("validation").includes("agent-browser"));
});

// The other half of that sentence. `skills:` is an allowlist, so a skill in
// NEITHER list is not deferred — it is unreachable, and the Skill tool rejects
// the load `acceptance-run` instructs. Absent from always-on AND present here is
// the pair that means "loadable, but not on every turn".
test("onDemandSkills: a validation run may load agent-browser", () => {
  assert.deepEqual(onDemandSkills("validation"), ["agent-browser"]);
  assert.ok(!alwaysOnSkills("validation").includes("agent-browser"));
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

test("requireWorkflowBodies: a validation run missing only acceptance-run is still fatal", () => {
  withMirror({ aep: "---\nname: aep\n---\n\nThe run\n" }, (workspace) => {
    assert.throws(
      () => requireWorkflowBodies(workspace, ["aep", "acceptance-run"]),
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
      "acceptance-run": "---\nname: acceptance-run\n---\n\nCODEWORD-VALIDATION\n",
    },
    (workspace) => {
      const out = requireWorkflowBodies(workspace, ["aep", "acceptance-run"]);
      assert.match(out, /CODEWORD-RUN/);
      assert.match(out, /CODEWORD-VALIDATION/);
      assert.match(out, /<skill name="aep">/);
      assert.match(out, /<skill name="acceptance-run">/);
      // Without this the agent re-invokes the Skill tool for guidance it already
      // has and pays for the body twice.
      assert.match(out, /ALREADY in your context/);
    },
  );
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
  // startCodingRun, which always passes it — but the seam stays optional so a
  // caller with no mirror cannot be broken by this.
  const out = promptWithProjectRoot("Work the issues", "/workspace/project");
  assert.ok(!out.includes("component-contract.md"));
  assert.ok(out.endsWith("Work the issues"));
});

// --- systemPromptAppend: the workflow's roles, bound at startup -------------

// The `aep` skill names ROLES — "the fan-out tool", "the wait tool" — because one
// authored library steers every org and a body naming `Agent`/`TaskOutput` would
// be a Claude Code document. The glossary is what resolves them, and the skill
// points at it BY POSITION ("the tool glossary at the end of your instructions"),
// so anything appended after it makes that pointer a lie.
test("systemPromptAppend: workflow first, pins next, the glossary last", () => {
  const appended = systemPromptAppend("WORKFLOW", "PINS", toolGlossary());

  assert.equal(appended, `WORKFLOW\n\nPINS\n\n${toolGlossary()}`);
  assert.ok(appended.endsWith(toolGlossary()), "the glossary is not at the end of the instructions");
});

// A run with nothing pinned is the ordinary case, and it must not open a gap
// where the pins would have been — the same reason readSkillBodies returns "".
test("systemPromptAppend: an unpinned run still gets the glossary, with no empty gap", () => {
  assert.equal(systemPromptAppend("WORKFLOW", "", toolGlossary()), `WORKFLOW\n\n${toolGlossary()}`);
});

// Every role the workflow's prose defers to has to be bound here, or the agent
// resolves it by guessing a tool name.
test("systemPromptAppend: the glossary names the fan-out, wait and task-list tools", () => {
  const glossary = toolGlossary();

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


// --- the policy the runner hands to a runtime -------------------------------

// The point of the runtime port is that `runner.ts` states the platform's rules
// ONCE and a runtime enforces them. These tests are the other half of that
// claim: they start a run against a stand-in runtime and read the policy it was
// given, which is the only way to see the whole statement in one place — the
// real adapter turns it into SDK options nothing outside a live session can
// inspect.
import { DENIED_CAPABILITIES, type Runtime, type RuntimePolicy } from "../runtime/port.js";
import { allowsWriteOutsideProject } from "./workspace_guard.js";
import { buildMcpPolicy, startCodingRun } from "./runner.js";
import { createRunTerminator } from "./run_loop.js";
import type { TaskLog } from "./logger.js";
import type { WorkspaceLayout } from "./workspace.js";

const STAGED_SECRET = "staged-secret-value-123456";

/**
 * A workspace whose mirror carries both workflow skills, and nothing else.
 *
 * Both, because `alwaysOnSkills` names `acceptance-run` for a validation run and
 * a mirror missing it is fatal by design — see requireWorkflowBodies.
 */
function mirrorWorkspace(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "aep-policy-"));
  for (const name of ["aep", "acceptance-run"]) {
    const skill = path.join(dir, ".claude", "skills", name);
    fs.mkdirSync(skill, { recursive: true });
    fs.writeFileSync(path.join(skill, "SKILL.md"), `# ${name}\nWORKFLOW BODY\n`, "utf8");
  }
  return dir;
}

function layoutFor(workspace: string): WorkspaceLayout {
  return {
    workspace,
    ghConfigDir: path.join(workspace, ".gh"),
    bearerFile: path.join(workspace, ".bearer"),
    aepDir: path.join(workspace, ".aep"),
    helperBin: path.join(workspace, ".aep", "credhelper"),
    ghWrapper: path.join(workspace, ".aep", "gh"),
  };
}

const silentLog: TaskLog = { write: () => {}, close: () => {}, dir: os.tmpdir() };

/**
 * A runtime that records what it was asked to run and then ends at once.
 *
 * The empty stream is deliberate: the run settles as "ended without result",
 * which is the loop's own business and is asserted where the loop is tested.
 * What this exists to capture is the POLICY.
 */
function recordingRuntime(): { runtime: Runtime; calls: { prompt: string; policy: RuntimePolicy }[] } {
  const calls: { prompt: string; policy: RuntimePolicy }[] = [];
  const runtime: Runtime = {
    name: "claude-code",
    defaultModel: "model-from-runtime",
    toolGlossary: () => "GLOSSARY",
    start: async (prompt, policy) => {
      calls.push({ prompt, policy });
      return {
        stream: { messages: (async function* () {})(), stopTask: async () => {} },
        translate: () => [],
        artifacts: async () => [],
        close: async () => {},
      };
    },
  };
  return { runtime, calls };
}

function dispatch(over: Partial<DispatchRequest> = {}): DispatchRequest {
  return {
    taskId: "11111111-2222-3333-4444-555555555555",
    orgId: "acme",
    projectId: "todo",
    componentName: "aep-milestone",
    repoUrl: "https://github.com/acme/todo.git",
    bearer: "",
    identity: { name: "AEP", email: "aep@example.com" },
    gitServiceUrl: "https://git.example.com",
    prompt: "Work the issues in this project.",
    taskKind: "implementation",
    ...over,
  };
}

/**
 * Run once against the stand-in runtime and hand back the policy it got.
 *
 * `emit` writes the feed straight to stdout, which is the test runner's own
 * channel here, so it is captured for the duration — a settle line interleaved
 * into TAP is not a failure worth debugging twice.
 */
async function policyFor(req: DispatchRequest, envOverrides: Record<string, string> = {}): Promise<RuntimePolicy> {
  const workspace = mirrorWorkspace();
  const { runtime, calls } = recordingRuntime();
  const original = process.stdout.write.bind(process.stdout);
  const restoreEnv: [string, string | undefined][] = Object.entries({
    AEP_TEST_STAGED_SECRET: STAGED_SECRET,
    ...envOverrides,
  }).map(([k, v]) => {
    const before = process.env[k];
    process.env[k] = v;
    return [k, before] as [string, string | undefined];
  });
  process.stdout.write = (() => true) as typeof process.stdout.write;
  try {
    const started = await startCodingRun(req, layoutFor(workspace), silentLog, undefined, undefined, runtime);
    await started.completion;
  } finally {
    process.stdout.write = original;
    for (const [k, before] of restoreEnv) {
      if (before === undefined) delete process.env[k];
      else process.env[k] = before;
    }
    fs.rmSync(workspace, { recursive: true, force: true });
  }
  assert.equal(calls.length, 1, "the runner started exactly one session");
  return calls[0].policy;
}

// Every clause the port carries, stated in one place. A guard that silently
// stopped being passed would leave the run unguarded with nothing to fail: the
// enforcement is the runtime's, so nothing in this package would notice.
test("startCodingRun: the policy states every guard the platform owns", async () => {
  const policy = await policyFor(dispatch());

  // The write rule is the PLATFORM's — tmp plus any dot-directory under $HOME,
  // as one rule — not a list this file could get out of step with.
  assert.equal(policy.write.allowOutsideProject, allowsWriteOutsideProject);
  // A one-shot pod has no interactive user, scheduler, durable session or peer,
  // whatever the runtime; the runtime maps the classes to its own names.
  assert.deepEqual([...policy.deniedCapabilities], [...DENIED_CAPABILITIES]);
  // Both egress guards are built from the SAME staged-secret list, which is the
  // env this run was actually given.
  assert.ok(policy.webSearch.deny(`how do I use ${STAGED_SECRET}`));
  assert.equal(policy.webSearch.deny("how do I use the ballerina http module"), null);
  assert.ok(policy.webFetch.deny(`https://example.com/?k=${STAGED_SECRET}`));
  assert.ok(policy.webFetch.deny("http://169.254.169.254/latest/meta-data/"));
  assert.equal(policy.webFetch.deny("https://ballerina.io/learn/"), null);
});

test("startCodingRun: the policy points the session at the workspace and its mirror", async () => {
  const policy = await policyFor(dispatch());

  assert.equal(policy.skills.dir, path.join(policy.workspace, ".claude", "skills"));
  // The one env var a skill needs to invoke something by absolute path.
  assert.equal(policy.env.AEP_SKILLS_DIR, policy.skills.dir);
  assert.equal(policy.taskKind, "implementation");
  assert.equal(policy.debug, false);
});

// The glossary is the runtime's text and it must be LAST — the `aep` skill
// points at it by position. The runner asks the runtime for it rather than
// looking one up, which is what lets a second runtime steer the same library.
test("startCodingRun: the preloaded appendix ends with the runtime's own glossary", async () => {
  const policy = await policyFor(dispatch());

  assert.match(policy.skills.preloadBodies, /WORKFLOW BODY/);
  assert.ok(policy.skills.preloadBodies.endsWith("GLOSSARY"));
});

// A run cannot derive its own project root, and a run that guessed built a whole
// component in the wrong tree, green.
test("startCodingRun: the prompt names the absolute project root and the contract path", async () => {
  const workspace = mirrorWorkspace();
  const { runtime, calls } = recordingRuntime();
  const original = process.stdout.write.bind(process.stdout);
  process.stdout.write = (() => true) as typeof process.stdout.write;
  try {
    const started = await startCodingRun(dispatch(), layoutFor(workspace), silentLog, undefined, undefined, runtime);
    await started.completion;
  } finally {
    process.stdout.write = original;
    fs.rmSync(workspace, { recursive: true, force: true });
  }
  assert.match(calls[0].prompt, new RegExp(`Your project root — the current working directory — is ${workspace}`));
  assert.match(calls[0].prompt, /component-contract\.md/);
  assert.ok(calls[0].prompt.endsWith("Work the issues in this project."));
});

// The organization's setting reaches the pod as an env var, and an org that
// never opened the page must get exactly the run it had.
test("startCodingRun: the model is the org's setting, or the runtime's default", async () => {
  assert.equal((await policyFor(dispatch())).model, "model-from-runtime");
  assert.equal((await policyFor(dispatch(), { AEP_AGENT_MODEL: "claude-haiku-4-5" })).model, "claude-haiku-4-5");
  // A blank stamp is the same as no stamp — a dispatcher that sends "" for an
  // unset setting must not pin the model to nothing.
  assert.equal((await policyFor(dispatch(), { AEP_AGENT_MODEL: "" })).model, "model-from-runtime");
});

// Watching the authoring tools costs a hook on every call, so it is registered
// only where something reads it.
// The `observe` seam is deliberately unregistered on BOTH kinds while real-time
// validation progress is deferred. The watchers it used to carry matched
// Playwright file writes and spec names, so against an agent driving a browser
// they matched nothing at all and every criterion rendered `not_validated` —
// which reads as a verdict, not as an empty state. Pinned so re-registering one
// is a deliberate act rather than a merge's side effect.
test("startCodingRun: no run registers tool watchers while progress is deferred", async () => {
  assert.equal((await policyFor(dispatch())).observe, undefined);
  assert.equal((await policyFor(dispatch({ taskKind: "validation" }))).observe, undefined);
});

// A URL with no token must omit the server rather than register it
// unauthenticated, and the platform's tool names travel BARE — namespacing is
// the runtime's convention.
test("startCodingRun: MCP is stated only when both the url and a token arrived", async () => {
  assert.equal((await policyFor(dispatch())).mcp, undefined);
  assert.equal((await policyFor(dispatch({ mcpUrl: "https://bff.example.com/internal/v1/mcp" }))).mcp, undefined);
  assert.equal((await policyFor(dispatch({ mcpToken: "tok" }))).mcp, undefined);

  const policy = await policyFor(
    dispatch({ mcpUrl: "https://bff.example.com/internal/v1/mcp", mcpToken: "tok" }),
  );
  assert.equal(policy.mcp?.url, "https://bff.example.com/internal/v1/mcp");
  assert.deepEqual([...(policy.mcp?.tools ?? [])], [
    "list_org_component_endpoints",
    "get_remote_git_file_contents",
    "search_remote_git_code",
  ]);
  assert.equal(await policy.mcp?.token(), "tok");
  // No publisher credentials were mounted, so there is nothing to remint — and
  // "can remint" is the same fact as "there is a stale token worth discarding".
  assert.equal(policy.mcp?.invalidate, undefined);
});

// --- the MCP policy's fatal: the loop settles, this callback does not --------

// The defect, pinned where it lived. `onFatal` used to emit a `run_settled` of
// its own and then hard-exit, while `consumeRun` was still reading — so one run
// could carry two settles, and every consumer treats the first as terminal
// (`buildCrew` settles every agent it never heard close on one). It now only
// states the reason; the loop stops the live tasks and writes the single settle
// (`run_loop.test.ts` counts them).
test("buildMcpPolicy: a fatal auth failure trips the terminator and puts nothing on the feed", async () => {
  const terminator = createRunTerminator();
  const policy = buildMcpPolicy(
    dispatch({ mcpUrl: "https://bff.example.com/internal/v1/mcp", mcpToken: "tok" }),
    layoutFor("/workspace/project"),
    terminator,
  );

  // `emit` writes straight to stdout, so this is how "emits nothing" is checked
  // rather than asserted about a mock that could drift from the real emitter.
  const lines: string[] = [];
  const original = process.stdout.write.bind(process.stdout);
  process.stdout.write = ((chunk: string | Uint8Array): boolean => {
    lines.push(String(chunk));
    return true;
  }) as typeof process.stdout.write;
  try {
    policy.mcp?.onFatal?.(new Error("refresh failed: 401"));
  } finally {
    process.stdout.write = original;
  }
  assert.deepEqual(lines, [], "the fatal writes no event of its own");

  const reason = await terminator.requested;
  assert.equal(reason.source, "mcp auth", "so the feed line reads [mcp auth] terminated — …");
  assert.match(reason.why, /can no longer be renewed: refresh failed: 401/);
  // The wording the old settle carried, kept: it is what a console shows as the
  // run's error, and it is now the LOOP that writes it there.
  assert.equal(reason.error, "mcp auth: refresh failed: 401");
});

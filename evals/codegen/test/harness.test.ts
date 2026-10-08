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
import { parse } from "yaml";
import { ChecklistSchema, CaseSchema, RunConfigSchema, parseYaml, unknownRoles, type Checklist } from "../src/case.js";
import { assertNotApiKey, CredentialError, oauthTokenFrom, playEnv, sdkEnv } from "../src/credentials.js";
import { chooseSource, isRendering, renderChecklist, type TreeFacts } from "../src/save.js";
import { plannerPrompt, planProblems } from "../src/planner.js";
import { walkerPrompt } from "../src/walker.js";
import { WIRED_AUTH_SEMANTICS } from "../src/wired-auth.js";
import { dirtyPaths } from "../src/provenance.js";
import { resolveCodingRun, usageByAgent } from "../src/log.js";
import { PATHS, PROVENANCE } from "../src/config.js";
import { baseUrl, isStopped, parseReady } from "../src/play.js";
import { BrowserLane, browserClock, confirmEachAction, guardTool, normalizeWalk, shellWords, walkerProblem } from "../src/walker.js";
import { bandFor, scoreAttempt, type Judgement } from "../src/score.js";
import { countEvents, lastResultCost, readRunSettled } from "../src/metrics.js";
import { attemptLine, compare, failureText, pickBaseline, renderReport, stat, summarize, type Summary } from "../src/report.js";
import { excludedFromProject, rewalkDirs, type AttemptRecord } from "../src/attempt.js";
import { mkdirSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Not a real credential: the prefix is the whole contract under test.
const TOKEN = "sk-ant-oat01-test-not-a-real-token";

const item = (id: string, extra: Record<string, unknown> = {}): Record<string, unknown> => ({
  id,
  role: "Employee",
  screen: "Home",
  flow: "f",
  steps: "do it",
  expect: "it shows",
  ...extra,
});

// --- credentials ------------------------------------------------------------

test("credentials: only an sk-ant-oat token is accepted, and the refusal never echoes the value", () => {
  assert.equal(oauthTokenFrom({ AEP_CODING_ANTHROPIC_KEY: ` ${TOKEN} ` }), TOKEN);
  assert.throws(() => oauthTokenFrom({}), CredentialError);
  assert.throws(() => oauthTokenFrom({ AEP_CODING_ANTHROPIC_KEY: "  " }), CredentialError);
  const apiKey = "sk-ant-api03-secret-value";
  assert.throws(
    () => oauthTokenFrom({ AEP_CODING_ANTHROPIC_KEY: apiKey }),
    (e: unknown) => e instanceof CredentialError && !e.message.includes(apiKey),
  );
});

test("credentials: a play child gets an EMPTY api key (loadEnvFile cannot refill it) and no OAuth var", () => {
  const parent = {
    PATH: "/bin",
    ANTHROPIC_API_KEY: "sk-ant-api-x",
    CLAUDE_CODE_OAUTH_TOKEN: "stray",
    ANTHROPIC_AUTH_TOKEN: "stray",
    AEP_MODEL_FORMAT: "openai-compatible",
  };
  const env = playEnv(parent, TOKEN, { runtime: "claude-code", model: "claude-sonnet-5" });
  assert.equal(env.ANTHROPIC_API_KEY, "");
  assert.ok(!("CLAUDE_CODE_OAUTH_TOKEN" in env));
  assert.ok(!("ANTHROPIC_AUTH_TOKEN" in env));
  assert.equal(env.AEP_CODING_ANTHROPIC_KEY, TOKEN);
  assert.equal(env.PATH, "/bin");
  assert.equal(env.AEP_AGENT_RUNTIME, "claude-code");
  assert.equal(env.AEP_AGENT_MODEL, "claude-sonnet-5");
  // A shell-exported connection would move a Claude Code run onto it.
  assert.equal(env.AEP_MODEL_FORMAT, "");
  assert.equal(parent.ANTHROPIC_API_KEY, "sk-ant-api-x", "the parent env is not mutated");
});

test("credentials: an opencode config runs on its connection and the token is blanked", () => {
  const env = playEnv(
    { ANTHROPIC_API_KEY: "k" },
    TOKEN,
    {
      runtime: "opencode",
      model: "m",
      connection: { format: "openai-compatible", baseUrl: "https://x/v1", authScheme: "bearer", apiKeyEnv: "CONN_KEY" },
    },
    { CONN_KEY: "conn-secret" },
  );
  assert.equal(env.AEP_CODING_ANTHROPIC_KEY, "");
  assert.equal(env.ANTHROPIC_API_KEY, "");
  assert.equal(env.AEP_MODEL_API_KEY, "conn-secret");
  assert.equal(env.AEP_MODEL_BASE_URL, "https://x/v1");
  assert.throws(
    () => playEnv({}, TOKEN, { runtime: "opencode", model: "m", connection: { format: "f", baseUrl: "b", apiKeyEnv: "NOPE" } }),
    CredentialError,
  );
});

test("credentials: SDK sessions get the token as CLAUDE_CODE_OAUTH_TOKEN and no api key at all", () => {
  const env = sdkEnv({ ANTHROPIC_API_KEY: "k", ANTHROPIC_AUTH_TOKEN: "a", HOME: "/h" }, TOKEN);
  assert.ok(!("ANTHROPIC_API_KEY" in env));
  assert.ok(!("ANTHROPIC_AUTH_TOKEN" in env));
  assert.equal(env.CLAUDE_CODE_OAUTH_TOKEN, TOKEN);
  assert.equal(env.HOME, "/h");
});

test("credentials: a surrounding Claude Code session's variables reach no child", () => {
  const parent = { CLAUDECODE: "1", CLAUDE_CODE_MESSAGING_TOKEN: "t", CLAUDE_CODE_SESSION_ID: "s", HOME: "/h" };
  for (const env of [sdkEnv(parent, TOKEN), playEnv(parent, TOKEN, { runtime: "claude-code", model: "m" })]) {
    assert.ok(!("CLAUDECODE" in env));
    assert.ok(!("CLAUDE_CODE_MESSAGING_TOKEN" in env));
    assert.ok(!("CLAUDE_CODE_SESSION_ID" in env));
    assert.equal(env.HOME, "/h");
  }
});

test("credentials: init's apiKeySource refuses every API-key source and passes an OAuth session", () => {
  assert.doesNotThrow(() => assertNotApiKey("none"));
  for (const source of ["ANTHROPIC_API_KEY", "apiKeyHelper", "/login managed key"]) {
    assert.throws(() => assertNotApiKey(source), CredentialError);
  }
});

// --- case schemas -----------------------------------------------------------

test("case: unknown keys are refused, weight defaults to 1, duplicate ids are refused", () => {
  const ok = ChecklistSchema.parse({ items: [item("a")] });
  assert.equal(ok.items[0]?.weight, 1);
  assert.deepEqual(ok.extras, { mustCover: [], mustNot: [] });
  assert.throws(() => ChecklistSchema.parse({ items: [item("a", { wieght: 2 })] }));
  assert.throws(() => ChecklistSchema.parse({ items: [item("a")], extras: { mustNto: [] } }));
  assert.throws(() => ChecklistSchema.parse({ items: [item("a")], extras: { mustNot: [{ id: "a", description: "x" }] } }));
  assert.throws(() => ChecklistSchema.parse({ items: [item("Not_Kebab")] }));
  assert.throws(() => CaseSchema.parse({ name: "x-y", description: "", source: { project: "p", snapshot: "s", savedAt: "t" }, extra: 1 }));
});

test("case: an opencode config needs a connection; a claude-code one may not carry one", () => {
  assert.throws(() => RunConfigSchema.parse({ id: "o", runtime: "opencode", model: "m" }));
  assert.throws(() =>
    RunConfigSchema.parse({ id: "c", runtime: "claude-code", model: "m", connection: { format: "f", baseUrl: "b", apiKeyEnv: "K" } }),
  );
  assert.doesNotThrow(() => RunConfigSchema.parse({ id: "sonnet", runtime: "claude-code", model: "claude-sonnet-5" }));
});

test("case: roles are checked against wire's list plus the two special callers", () => {
  const checklist = ChecklistSchema.parse({
    items: [item("a"), item("b", { role: "no role" }), item("c", { role: "signed out" }), item("d", { role: "Ghost" })],
  });
  assert.deepEqual(unknownRoles(checklist, ["Employee"]), ['d: role "Ghost"']);
  assert.deepEqual(planProblems(checklist.items, ["Employee"]).length, 1);
});

test("case: the YAML loader names the file and the path of each issue", () => {
  assert.throws(() => parseYaml("items: [{id: a}]", ChecklistSchema, "x.yaml"), /x\.yaml:[\s\S]*items\.0\.role/);
});

// --- save -------------------------------------------------------------------

const facts = (dir: string, entries: string[], agentWritten: string[] = []): TreeFacts => ({ dir, entries, agentWritten });

test("save: a clean pre-code project is used as it stands", () => {
  const choice = chooseSource(facts("/p", ["issues", "specs"]), [facts("/p/.aep-playground/undo/1", ["issues", "specs"])]);
  assert.deepEqual(choice, { ok: true, dir: "/p", snapshot: "current" });
});

test("save: a coded project falls back to the OLDEST clean snapshot", () => {
  const choice = chooseSource(facts("/p", ["expense-api", "issues", "specs"]), [
    facts("/u/2026-09-17T10-19-40-648Z", ["issues", "specs"]),
    facts("/u/2026-09-17T09-24-32-848Z", ["issues", "specs"]),
  ]);
  assert.deepEqual(choice, { ok: true, dir: "/u/2026-09-17T09-24-32-848Z", snapshot: "2026-09-17T09-24-32-848Z" });
});

test("save: agent-written issue sections disqualify a tree, the current one and snapshots alike", () => {
  const choice = chooseSource(facts("/p", ["issues", "specs"], ["issues/1.md"]), [
    facts("/u/2026-01-01T00-00-00-000Z", ["issues", "specs"], ["issues/2.md"]),
    facts("/u/2026-01-02T00-00-00-000Z", ["issues", "specs"]),
  ]);
  assert.deepEqual(choice, { ok: true, dir: "/u/2026-01-02T00-00-00-000Z", snapshot: "2026-01-02T00-00-00-000Z" });
});

test("save: nothing pre-code anywhere is refused with what was seen", () => {
  const choice = chooseSource(facts("/p", ["api", "issues", "specs"]), [facts("/u/1", ["api", "issues", "specs"])]);
  assert.equal(choice.ok, false);
  assert.match(choice.ok ? "" : choice.reason, /api, issues, specs/);
});

test("save: the rendered checklist carries its header and gaps as comments and re-parses to the same list", () => {
  const checklist: Checklist = ChecklistSchema.parse({ items: [item("a", { weight: 2 }), item("b")] });
  const text = renderChecklist(checklist, { gaps: ["no UI assigns a manager"] }, ["Employee"]);
  assert.match(text, /^# .*HAND-EDITABLE/m);
  assert.match(text, /# {2}- no UI assigns a manager/);
  assert.deepEqual(ChecklistSchema.parse(parse(text)), checklist);
});

// --- play -------------------------------------------------------------------

test("play: READY parsing takes wire's line and nothing that merely mentions it", () => {
  assert.equal(parseReady("READY http://localhost:5173/?role=Employee"), "http://localhost:5173/?role=Employee");
  assert.equal(parseReady("  READY http://127.0.0.1:5174/  "), "http://127.0.0.1:5174/");
  assert.equal(parseReady("not READY yet"), null);
  assert.equal(parseReady("READY"), null);
  assert.equal(baseUrl("http://localhost:5173/?role=Employee"), "http://localhost:5173/");
  assert.ok(isStopped("STOPPED"));
  assert.ok(!isStopped("  ✓ STOPPED something"));
});

// --- walker guard -----------------------------------------------------------

test("walker: shellWords honours quotes", () => {
  assert.deepEqual(shellWords(`agent-browser fill @e3 "Taxi to client" --json`), ["agent-browser", "fill", "@e3", "Taxi to client", "--json"]);
});

test("walker: Bash is one agent-browser call, nothing else", () => {
  const dir = "/w/walk";
  assert.deepEqual(guardTool("Bash", { command: "agent-browser snapshot -i" }, dir), { allow: true });
  assert.deepEqual(guardTool("Bash", { command: `agent-browser fill @e2 "route 66"` }, dir), { allow: true });
  for (const command of [
    "ls",
    "agent-browser snapshot | head",
    "agent-browser open x; rm -rf /",
    "agent-browser open $(whoami)",
    "agent-browser screenshot > out",
    "agent-browser close --all",
    "agent-browser --session other snapshot",
    "agent-browser eval document.body.innerHTML",
    "agent-browser network route http://x --body {}",
    "agent-browser chat hello",
    "agent-browser open x\nagent-browser open y",
  ]) {
    assert.equal(guardTool("Bash", { command }, dir).allow, false, command);
  }
  assert.deepEqual(guardTool("Bash", { command: "agent-browser network requests" }, dir), { allow: true });
});

test("walker: every command inside a batch is held to the same rules", () => {
  const dir = "/w/walk";
  for (const command of [
    `agent-browser batch "eval document.title"`,
    `agent-browser batch "get url" "eval document.title"`,
    `agent-browser batch --bail "network route http://x --body {}"`,
    `agent-browser batch "close --all"`,
    `agent-browser batch "snapshot -i --session other"`,
    `agent-browser batch "webmcp list"`,
    `agent-browser batch 'batch "eval 1"'`,
  ]) {
    assert.equal(guardTool("Bash", { command }, dir).allow, false, command);
  }
  assert.match(
    (guardTool("Bash", { command: `agent-browser batch "eval document.title"` }, dir) as { reason: string }).reason,
    /eval is not available/,
  );
  // A batch that goes on after a failed click sends its keys to the wrong element.
  assert.match(
    (guardTool("Bash", { command: `agent-browser batch "click @e4" "press 2"` }, dir) as { reason: string }).reason,
    /--bail/,
  );
  assert.deepEqual(
    guardTool("Bash", { command: `agent-browser batch --bail "click @e4" "press 2" "press ArrowRight" "fill @e5 'route 66'"` }, dir),
    { allow: true },
  );
});

test("walker: the pinned agent-browser is the runner image's version, installed where the walk looks", () => {
  const dockerfile = readFileSync(join(PATHS.repoRoot, "runners", "remote-worker", "Dockerfile"), "utf8");
  const imageVersion = /^ARG AGENT_BROWSER_VERSION=(\S+)$/m.exec(dockerfile)?.[1];
  assert.ok(imageVersion, "the Dockerfile lost its AGENT_BROWSER_VERSION ARG");
  const pkg = JSON.parse(readFileSync(join(PATHS.packageRoot, "package.json"), "utf8")) as {
    devDependencies?: Record<string, string>;
  };
  assert.equal(pkg.devDependencies?.["agent-browser"], imageVersion);
  assert.equal(walkerProblem(), undefined);
});

test("walker: Read and Write stay inside walk/", () => {
  const dir = "/w/walk";
  assert.equal(guardTool("Read", { file_path: "shots/a.png" }, dir).allow, true);
  assert.equal(guardTool("Read", { file_path: "/w/walk/shots/a.png" }, dir).allow, true);
  assert.equal(guardTool("Read", { file_path: "/w/project/src/App.tsx" }, dir).allow, false);
  assert.equal(guardTool("Write", { file_path: "../x" }, dir).allow, false);
  assert.equal(guardTool("Write", { file_path: "/w/walk-evil/x" }, dir).allow, false);
});

test("save: a case leaves out the compiled renderings, keeps their sources", () => {
  assert.equal(isRendering("specs/design/components/web/wireframes.excalidraw"), true);
  assert.equal(isRendering("specs/design/cell-diagram.gen.json"), true);
  assert.equal(isRendering("specs/design/components/web/wireframes.dsl"), false);
  assert.equal(isRendering("specs/design/design.cell"), false);
  assert.equal(isRendering("specs/design/components/api/design.json"), false);
});

test("walker: one browser command runs at a time; a finished one frees the lane", () => {
  const lane = new BrowserLane();
  assert.deepEqual(lane.enter("t1"), { allow: true });
  const overlapping = lane.enter("t2");
  assert.equal(overlapping.allow, false);
  assert.match((overlapping as { reason: string }).reason, /batch/);
  lane.leave("t2"); // a refused call never held the lane
  assert.equal(lane.enter("t3").allow, false);
  lane.leave("t1");
  assert.deepEqual(lane.enter("t3"), { allow: true });
});

test("walker: unreported items become blocked/not reached; unknown ids are dropped", () => {
  const items = ChecklistSchema.parse({ items: [item("a"), item("b")] }).items;
  const result = normalizeWalk(items, {
    items: [
      { id: "zz", verdict: "pass", observed: "", steps_taken: "", screenshots: [], console_errors: [], failed_requests: [] },
      { id: "a", verdict: "pass", observed: "ok", steps_taken: "", screenshots: [], console_errors: [], failed_requests: [] },
    ],
    notes: "n",
  });
  assert.deepEqual(result.items.map((i) => [i.id, i.verdict, i.observed]), [["a", "pass", "ok"], ["b", "blocked", "not reached"]]);
});

// --- score ------------------------------------------------------------------

test("score: weighted ratio; blocked and unjudged items fail; mustNot caps pass at review", () => {
  const items = ChecklistSchema.parse({ items: [item("a", { weight: 2 }), item("b"), item("c"), item("d")] }).items;
  const judgement: Judgement = {
    items: [
      { id: "a", verdict: "pass", symptom: "" },
      { id: "b", verdict: "pass", symptom: "" },
      { id: "c", verdict: "fail", symptom: "nothing happens" },
    ],
    mustNot: [],
    summary: "",
  };
  const walk = new Map<string, "pass" | "fail" | "blocked">([["b", "blocked"]]);
  const s = scoreAttempt(items, judgement, walk);
  assert.equal(s.score, 40); // 2 of 5
  assert.equal(s.band, "fail");
  assert.deepEqual(s.failing.map((f) => f.id).sort(), ["b", "c", "d"]);
  assert.equal(s.failing.find((f) => f.id === "d")?.symptom, "not judged");

  const allPass: Judgement = { items: items.map((i) => ({ id: i.id, verdict: "pass", symptom: "" })), mustNot: [{ id: "x", violated: true, evidence: "e" }], summary: "" };
  const capped = scoreAttempt(items, allPass, new Map());
  assert.equal(capped.score, 100);
  assert.equal(capped.band, "review");
  assert.equal(capped.capped, true);
});

test("score: band edges", () => {
  assert.equal(bandFor(75, false).band, "pass");
  assert.equal(bandFor(74, false).band, "review");
  assert.equal(bandFor(50, false).band, "review");
  assert.equal(bandFor(49, false).band, "fail");
  assert.deepEqual(bandFor(40, true), { band: "fail", capped: false });
});

// --- metrics ----------------------------------------------------------------

test("metrics: the last run_settled and the last result cost; a torn line is skipped", () => {
  const feed = [
    `{"kind":"run_started"}`,
    `{"kind":"run_settled","outcome":"failed","usage":{"inputTokens":1}}`,
    `{"kind":"run_settled","outcome":"success","usage":{"inputTokens":5,"outputTokens":6,"cacheReadTokens":7,"cacheCreationTokens":8}}`,
    `{"kind":"run_sett`,
  ].join("\n");
  assert.deepEqual(readRunSettled(feed), { outcome: "success", tokens: { input: 5, output: 6, cacheRead: 7, cacheCreation: 8 } });
  assert.equal(readRunSettled(`{"kind":"run_started"}`), null);
  assert.equal(countEvents(feed), 3);
  assert.equal(countEvents(""), 0);
  assert.equal(lastResultCost(`{"type":"result","total_cost_usd":1}\n{"type":"assistant"}\n{"type":"result","total_cost_usd":2.5}`), 2.5);
  assert.equal(lastResultCost(`{"type":"assistant"}`), null);
});

// --- report -----------------------------------------------------------------

const record = (over: Partial<AttemptRecord>): AttemptRecord => ({
  sweepId: "s",
  case: "c",
  config: "sonnet",
  attempt: 1,
  status: "scored",
  score: 80,
  band: "pass",
  capped: false,
  failing: [],
  violated: [],
  phases: {},
  coding: { outcome: "success", minutes: 40, costUsd: 3, tokens: null },
  walk: null,
  judge: null,
  archive: "evals/codegen/.runs/s/c/sonnet/attempt-1",
  ...over,
});

test("report: harness errors are counted but excluded; hard fails score 0 and count", () => {
  const [s] = summarize([
    record({ attempt: 1, score: 80 }),
    record({ attempt: 2, status: "hard-fail", score: 0, band: "fail", coding: { outcome: "failed", minutes: 10, costUsd: null, tokens: null } }),
    record({ attempt: 3, status: "harness-error", score: null, band: null }),
    record({ attempt: 4, score: 60 }),
  ]);
  assert.ok(s);
  assert.equal(s.n, 3);
  assert.equal(s.hardFails, 1);
  assert.equal(s.harnessErrors, 1);
  assert.deepEqual(s.score, { median: 60, min: 0, max: 80, spread: 80 });
  assert.equal(s.codingMinutes, 40);
  assert.equal(s.codingCostUsd, 3);
});

test("report: stat on even and empty inputs", () => {
  assert.deepEqual(stat([1, 3]), { median: 2, min: 1, max: 3, spread: 2 });
  assert.deepEqual(stat([]), { median: 0, min: 0, max: 0, spread: 0 });
});

const summary = (over: Partial<Summary>): Summary => ({
  key: "c × sonnet",
  case: "c",
  config: "sonnet",
  n: 3,
  score: stat([60, 70, 80]),
  hardFails: 0,
  harnessErrors: 0,
  codingMinutes: null,
  codingCostUsd: null,
  ...over,
});

test("report: a delta inside the wider spread, or with n=1 on either side, is inconclusive", () => {
  assert.match(compare(summary({ score: stat([75, 85, 95]) }), summary({})), /inconclusive — spread is 20/);
  assert.match(compare(summary({ score: stat([95, 96, 97]) }), summary({ score: stat([60, 61, 62]) })), /^\+35 \(was 61\)$/);
  assert.match(compare(summary({ n: 1, score: stat([95]) }), summary({ score: stat([60, 61, 62]) })), /n=1/);
  assert.equal(compare(summary({}), undefined), "—");
});

test("report: the baseline is the newest EARLIER sweep with the row and no harness errors in it", () => {
  const sweeps: Record<string, Summary[]> = {
    "2026-01-01": [summary({})],
    "2026-01-02": [summary({ harnessErrors: 1 })],
    "2026-01-03": [summary({ key: "other × sonnet" })],
    "2026-01-05": [summary({})],
  };
  const base = pickBaseline("c × sonnet", "2026-01-04", Object.keys(sweeps), (id) => sweeps[id]);
  assert.equal(base?.sweep, "2026-01-01");
});

test("report: a failed attempt's line names its phase, whose failure it was, and why; an old record its symptom", () => {
  const harness = record({
    status: "harness-error",
    score: null,
    band: null,
    failure: { phase: "walk", cause: "environment", reason: "browser unresponsive: 3 agent-browser commands in a row ran to their timeout" },
  });
  assert.match(attemptLine(harness), /harness-error · walk failed \(environment\): browser unresponsive/);
  const hard = record({ status: "hard-fail", score: 0, band: "fail", failure: { phase: "wire", cause: "app", reason: "unwireable: api started and exited with code 1" } });
  assert.match(attemptLine(hard), /hard-fail · 0 fail · wire failed \(app\): unwireable/);
  assert.equal(failureText(record({ symptom: "unwireable: legacy" })), "unwireable: legacy");
  assert.equal(failureText(record({})), "");
});

test("report: an attempt line names status, score, top failing items and the archive", () => {
  const line = attemptLine(
    record({ score: 40, band: "fail", failing: [1, 2, 3, 4].map((n) => ({ id: `i${String(n)}`, weight: 1, symptom: "" })) }),
  );
  assert.match(line, /scored · 40 fail · failing: i1, i2, i3 \(\+1\) — `evals\/codegen\/\.runs\/s\/c\/sonnet\/attempt-1`/);
});

// --- archive ----------------------------------------------------------------

test("archive: dependencies, the session's secrets, undo and run dirs stay out of project/", () => {
  for (const rel of [
    "expense-webapp/node_modules/react/index.js",
    "node_modules",
    ".aep-playground/wire/secrets.json",
    ".aep-playground/wire/tokens.json",
    ".aep-playground/wire/key.pem",
    ".aep-playground/undo/2026/specs/x",
    ".aep-playground/runs/2026-code/progress.ndjson",
  ]) {
    assert.ok(excludedFromProject(rel), rel);
  }
  for (const rel of ["expense-webapp/src/App.tsx", ".aep-playground/wire/plan.json", ".aep-playground/wire/compose.yaml", "issues/1.md"]) {
    assert.ok(!excludedFromProject(rel), rel);
  }
});

test("archive: build output at an App Path's root stays out; the same names anywhere else are sources", () => {
  const appPaths = ["onboarding-api", "onboarding-webapp"];
  for (const rel of ["onboarding-api/target", "onboarding-api/target/bin/app.jar", "onboarding-webapp/dist/index.html"]) {
    assert.ok(excludedFromProject(rel, appPaths), rel);
  }
  for (const rel of [
    "onboarding-api/main.bal",
    "onboarding-api/targets.bal",
    "onboarding-webapp/src/dist/format.ts",
    "target/notes.md",
    "specs/design/dist",
    "onboarding-webapp/build/vite.config.ts",
  ]) {
    assert.ok(!excludedFromProject(rel, appPaths), rel);
  }
  assert.ok(!excludedFromProject("onboarding-api/target/bin/app.jar"));
});


// --- rewalks ----------------------------------------------------------------

test("rewalk: rewalks never enter the statistics, and are listed under their own heading", () => {
  const records = [record({ score: 80 }), record({ kind: "rewalk", rewalk: 1, score: 10, band: "fail" })];
  const [s] = summarize(records);
  assert.equal(s?.n, 1);
  assert.equal(s?.score.median, 80);
  const text = renderReport({
    sweepId: "s",
    facts: {},
    notes: [],
    summaries: summarize(records),
    records: [records[0]!],
    rewalks: [records[1]!],
    baselines: new Map(),
  });
  assert.match(text, /## Rewalks — excluded from every statistic/);
  assert.match(text, /#1 rewalk-1\*\* scored · 10 fail/);
});

test("rewalk: numbering takes the next free rewalk-<n> and stages under a short, unique name", () => {
  const dir = mkdtempSync(join(tmpdir(), "codegen-rewalk-"));
  const parent = record({ case: "expense-claims", config: "sonnet", attempt: 1 });
  assert.equal(rewalkDirs(dir, parent).n, 1);
  mkdirSync(join(dir, "rewalk-1"));
  mkdirSync(join(dir, "rewalk-3"));
  const next = rewalkDirs(dir, parent);
  assert.equal(next.n, 4);
  assert.equal(next.archive, join(dir, "rewalk-4"));
  assert.match(next.stage, /\/rewalk\/expense-claims-sonnet-1r4$/);
});

// --- prompts ----------------------------------------------------------------

test("prompts: planner and walker both carry the wired-mode auth semantics; the planner the DSL-controls rule", () => {
  const planner = plannerPrompt(["Employee"]);
  assert.ok(planner.includes(WIRED_AUTH_SEMANTICS));
  assert.match(planner, /READ-ONLY/);
  assert.match(planner, /the DSL wins for the walk/);
  const walker = walkerPrompt({ baseUrl: "http://localhost:5173/", roles: ["Employee"], items: [], mustNot: [] });
  assert.ok(walker.includes(WIRED_AUTH_SEMANTICS));
});

test("prompts: the walker knows the browser's clock", () => {
  assert.equal(browserClock(new Date("2026-10-04T14:51:00Z"), "Asia/Colombo"), "2026-10-04 20:21 (Asia/Colombo)");
  assert.equal(browserClock(new Date("2026-10-04T23:05:00Z"), "UTC"), "2026-10-04 23:05 (UTC)");
  const walker = walkerPrompt({ baseUrl: "http://localhost:5173/", roles: ["Employee"], items: [], mustNot: [], now: new Date("2026-10-04T14:51:00Z") });
  assert.match(walker, /browser's clock read 2026-10-04 \d\d:\d\d/);
});

test("prompts: the walker confirms actions by the agent-browser skill's own section, not a copy", () => {
  const section = confirmEachAction();
  assert.match(section, /^## Confirm each action\n/);
  assert.doesNotMatch(section, /\n## /, "the section stops at the next heading");
  const walker = walkerPrompt({ baseUrl: "http://localhost:5173/", roles: ["Employee"], items: [], mustNot: [] });
  assert.ok(walker.includes(section));
  assert.doesNotMatch(walker, /select @e7/, "no second list of CLI verbs beside the skill's");
});


// --- provenance -------------------------------------------------------------

test("provenance: dirty paths are filtered to the harness-relevant roots; renames count by new path", () => {
  const porcelain = [
    " M skills/aep/SKILL.md",
    "?? skills/aep/references/repo-gitignore.md",
    " M go.work.sum",
    "M  playground/src/cli.ts",
    "R  runners/remote-worker/old.ts -> runners/remote-worker/new.ts",
    '?? "evals/codegen/src/a b.ts"',
    " M services/aep-api/main.go",
    "",
  ].join("\n");
  assert.deepEqual(dirtyPaths(porcelain, PROVENANCE.dirtyRoots), [
    "evals/codegen/src/a b.ts",
    "playground/src/cli.ts",
    "runners/remote-worker/new.ts",
    "skills/aep/SKILL.md",
    "skills/aep/references/repo-gitignore.md",
  ]);
  assert.deepEqual(dirtyPaths("", PROVENANCE.dirtyRoots), []);
});

// --- log --------------------------------------------------------------------

test("log: the newest archived coding run is read; a rewalk points at its parent; no run says so", () => {
  const attempt = mkdtempSync(join(tmpdir(), "codegen-log-"));
  const none = resolveCodingRun(attempt);
  assert.equal(none.ok, false);
  assert.match(none.ok ? "" : none.reason, /archived no coding run/);

  mkdirSync(join(attempt, "coding", "2026-10-03T10-00-00-000Z-code"), { recursive: true });
  mkdirSync(join(attempt, "coding", "2026-10-03T11-00-00-000Z-code"), { recursive: true });
  const found = resolveCodingRun(attempt);
  assert.deepEqual(found, { ok: true, runDir: join(attempt, "coding", "2026-10-03T11-00-00-000Z-code") });

  const rewalk = join(attempt, "rewalk-2");
  mkdirSync(rewalk);
  const pointer = resolveCodingRun(rewalk);
  assert.equal(pointer.ok, false);
  assert.match(pointer.ok ? "" : pointer.reason, new RegExp(`log --attempt ${attempt.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`));
});

test("log: usageByAgent names subagents by their fan-out description and counts each message's cache tokens once", () => {
  const lines = [
    { type: "assistant", message: { id: "m1", usage: { cache_read_input_tokens: 10, cache_creation_input_tokens: 1 }, content: [{ type: "tool_use", id: "t-agent", name: "Agent", input: { description: "Build api issue #1" } }] } },
    { type: "assistant", parent_tool_use_id: "t-agent", message: { id: "m2", usage: { cache_read_input_tokens: 5, cache_creation_input_tokens: 7 }, content: [{ type: "thinking" }] } },
    { type: "assistant", parent_tool_use_id: "t-agent", message: { id: "m2", usage: { cache_read_input_tokens: 5, cache_creation_input_tokens: 7 }, content: [{ type: "tool_use", id: "t2", name: "Bash", input: {} }] } },
    // The SDK's own count wins over the forwarded calls seen.
    { type: "system", subtype: "task_notification", tool_use_id: "t-agent", usage: { total_tokens: 99, tool_uses: 3, duration_ms: 120000 } },
  ].map((l) => JSON.stringify(l)).join("\n") + "\nnot json\n";
  assert.deepEqual(usageByAgent(lines), [
    { agent: "lead", toolCalls: 1, cacheRead: 10, cacheCreation: 1 },
    { agent: "Build api issue #1", toolCalls: 3, durationMs: 120000, cacheRead: 5, cacheCreation: 7 },
  ]);
});

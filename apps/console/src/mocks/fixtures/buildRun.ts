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

import { featureScenarios, parseFeatureFile } from "@aep/ui-acceptance-view";
import type { components } from "../../generated/aep-api";

// One build's run, scripted: what today's platform would say about it, frame
// by frame, compressed into about 25 seconds. A run is scheduled, not
// simulated (as a chat turn is, chatServer.ts): it has a start time and a
// timed script, so its run row, its ledger line, its progress stream and its
// validation report are all worked out from the clock, and a reload lands in
// the middle of it and reattaches.
//
// The shape follows the old console's fixtures (classic-console tag, src/mocks/fixtures/
// run-progress.ts and validation.ts): a coding cycle whose lead agent reads
// the spec, writes its plan (`work_item`, source plan) and works it entry by
// entry; then a validation cycle that judges each criterion and commits
// report.json (schemaVersion 2) against the acceptance files the design wrote.

type RunProgressEvent = components["schemas"]["RunProgressEvent"];
type RunEvent = components["schemas"]["RunEvent"];
type RunCycleView = components["schemas"]["RunCycleView"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type BuildSummary = components["schemas"]["BuildSummary"];
type ValidationSnapshot = components["schemas"]["ValidationSnapshot"];

export interface RunInput {
  version: string;
  runId: string;
  milestoneNumber: number;
  /** The features it builds, in spec order. */
  features: { id: string; name: string }[];
  /** A repair build: the version it fixes and the stories that failed there. */
  repair: { of: string; stories: string[] } | null;
  /** The acceptance files of the features it builds, as the design wrote them. */
  criteria: { path: string; content: string }[];
  /** The story whose scenarios fail validation; null when every one passes. */
  failing: string | null;
  startedAt: number;
}

interface TimedFrame {
  at: number;
  frame: RunProgressEvent;
}

interface CycleSpan {
  id: string;
  kind: RunCycleView["kind"];
  created: number;
  ended: number;
}

export interface RunScript {
  input: RunInput;
  frames: TimedFrame[];
  coding: CycleSpan;
  validation: CycleSpan;
  /** When the run settles, ms after it started. */
  end: number;
}

type EventBody = Omit<RunEvent, "v" | "seq" | "ts" | "agentId"> & { agentId?: string };

const MODEL = "claude-opus-4";
const PLANNING_MS = 800;
const PLAN_MS = 2_000;
const ENTRY_MS = 2_500;
const FIX_ENTRY_MS = 2_500;
const SCENARIO_MS = 550;

function slug(text: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

function pascal(text: string): string {
  return text.replace(/(^|[^a-zA-Z0-9]+)([a-zA-Z0-9])/g, (_, __, c: string) => c.toUpperCase());
}

interface Entry {
  id: string;
  title: string;
  work: EventBody[];
}

const write = (path: string): EventBody => ({ kind: "tool_use", tool: "Write", summary: path });
const edit = (path: string): EventBody => ({ kind: "tool_use", tool: "Edit", summary: path });
const test = (command: string, passed: number): EventBody[] => [
  { kind: "tool_use", tool: "Bash", summary: command, toolUseId: command },
  { kind: "tool_result", tool: "Bash", ok: true, exitCode: 0, durationMs: 6_000 + passed * 200, summary: `${passed} passed`, toolUseId: command },
];

function buildEntries(features: RunInput["features"]): Entry[] {
  const foundation: Entry = {
    id: "foundation",
    title: "Foundation · API",
    work: [
      write("api/src/app.ts"),
      write("api/src/auth/sso.ts"),
      write("api/src/audit/log.ts"),
      write("api/src/jobs/retention.ts"),
      ...test("pnpm --filter api test", 18),
    ],
  };
  const perFeature = features.flatMap((f): Entry[] => {
    const s = slug(f.name);
    return [
      {
        id: `${f.id}-api`,
        title: `${f.id} ${f.name} · API`,
        work: [write(`api/src/${s}/routes.ts`), write(`api/src/${s}/store.ts`), write(`api/src/${s}/rules.ts`), ...test(`pnpm --filter api test -- ${s}`, 21)],
      },
      {
        id: `${f.id}-web`,
        title: `${f.id} ${f.name} · Web app`,
        work: [write(`web/src/pages/${pascal(f.name)}.tsx`), write(`web/src/api/${s}.ts`), ...test(`pnpm --filter web test -- ${s}`, 9)],
      },
    ];
  });
  return [foundation, ...perFeature];
}

function fixEntries(features: RunInput["features"], stories: string[]): Entry[] {
  const ids = new Set(stories.map((s) => s.split(".")[0]));
  return features
    .filter((f) => ids.has(f.id))
    .map((f) => ({
      id: `${f.id}-api-fix`,
      title: `${f.id} ${f.name} · API fix`,
      work: [edit(`api/src/${slug(f.name)}/queue.ts`), write(`api/src/${slug(f.name)}/queue.test.ts`), ...test(`pnpm --filter api test -- ${slug(f.name)}`, 22)],
    }));
}

interface Scenario {
  feature: string;
  file: string;
  rule: string;
  name: string;
  story: string | null;
  line: number;
  steps: { keyword: string; text: string }[];
}

/** Every scenario of the acceptance files, with the story its rule is tagged with. */
function scenariosOf(criteria: RunInput["criteria"]): Scenario[] {
  return criteria.flatMap((file) => {
    const feature = parseFeatureFile(file.path, file.content);
    if (!feature) return [];
    return featureScenarios(feature).map(({ rule, scenario }) => {
      const tag = [...scenario.tags, ...rule.tags].find((t) => t.startsWith("@story-"));
      return {
        feature: feature.name,
        file: file.path,
        rule: rule.text,
        name: scenario.name,
        story: tag ? tag.slice("@story-".length) : null,
        line: scenario.line,
        steps: scenario.steps.map((s) => ({ keyword: s.keyword, text: s.text })),
      };
    });
  });
}

// What the run recorded for the scenario that fails, step by step: the
// deputy's queue is empty because the approval queue never consults a
// deputy. Any other failing scenario fails on its first Then.
const DEPUTY_EVIDENCE: Record<string, { command: string; exit: number; observed?: string }> = {
  "Sam is on leave and named Lee as his deputy": { command: 'PUT /managers/sam/deputy {"deputy":"lee","onLeave":true}', exit: 0 },
  "claim 43 is waiting for Sam": { command: 'POST /claims {"employee":"priya","amountCents":18000}', exit: 0 },
  "Lee opens Pending approvals": { command: "agent-browser open /approvals --as lee", exit: 0 },
  "claim 43 is in Lee's queue": {
    command: 'agent-browser wait --text "Claim 43" --timeout 3000',
    exit: 1,
    observed: "an empty queue",
  },
};

function reportSteps(scenario: Scenario, fails: boolean) {
  if (!fails) return scenario.steps.map((s) => ({ ...s, exit: 0 }));
  const decider = scenario.steps.findIndex((s) => s.keyword === "Then");
  return scenario.steps.slice(0, decider + 1).map((s, i) => {
    const evidence = DEPUTY_EVIDENCE[s.text];
    if (evidence) return { ...s, ...evidence };
    return i === decider ? { ...s, exit: 1, observed: "not what the scenario expects" } : { ...s, exit: 0 };
  });
}

function reportFor(input: RunInput, scenarios: Scenario[]): string {
  return JSON.stringify(
    {
      schemaVersion: 2,
      generatedAt: new Date(input.startedAt).toISOString(),
      commit: `${input.runId}-validation`,
      baseUrl: "https://app--development.localhost/",
      isolation: "Every scenario creates the claims it asserts on, with a run-unique employee, and asserts only on them.",
      scenarios: scenarios.map((s) => {
        const fails = s.story !== null && s.story === input.failing;
        return {
          feature: s.feature,
          featureFile: s.file,
          line: s.line,
          rule: s.rule,
          scenario: s.name,
          tags: [],
          outcome: fails ? "failed" : "passed",
          steps: reportSteps(s, fails),
        };
      }),
    },
    null,
    2,
  );
}

/** The run, frame by frame, from its input. Deterministic: the same input gives the same script. */
export function runScript(input: RunInput): RunScript {
  const frames: TimedFrame[] = [];
  const codingId = `${input.runId}-c1`;
  const validationId = `${input.runId}-c2`;
  let seq = 0;
  let cycleId = codingId;
  const push = (at: number, body: EventBody) => {
    const { agentId = "lead", ...rest } = body;
    const event = { v: 2, seq: seq++, ts: new Date(input.startedAt + at).toISOString(), agentId, ...rest } as RunEvent;
    frames.push({ at, frame: { type: "event", cycleId, attempt: 1, event } });
  };
  const spread = (start: number, span: number, bodies: EventBody[]) =>
    bodies.forEach((b, i) => push(start + Math.round(((i + 1) * span) / (bodies.length + 1)), b));

  // The coding cycle: the plan, then its entries one after another.
  const entries = input.repair ? fixEntries(input.features, input.repair.stories) : buildEntries(input.features);
  const codingStart = PLANNING_MS;
  frames.push({ at: codingStart, frame: { type: "cycle", cycle: cycleRecord(input, { id: codingId, kind: input.repair ? "fix" : "coding", created: codingStart, ended: Infinity }, 0) } });
  const reads: EventBody[] = input.repair
    ? [
        { kind: "tool_use", tool: "Read", summary: "tests/acceptance/report.json" },
        ...input.criteria.map((c): EventBody => ({ kind: "tool_use", tool: "Read", summary: c.path })),
      ]
    : [
        { kind: "tool_use", tool: "Read", summary: "specs/requirements/prd.md" },
        { kind: "tool_use", tool: "Read", summary: "specs/requirements/product-wide.md" },
        ...input.features.map((f): EventBody => ({ kind: "tool_use", tool: "Read", summary: `specs/requirements/features/${f.id}-${slug(f.name)}.md` })),
        { kind: "tool_use", tool: "Read", summary: "specs/design/architecture.cell" },
      ];
  spread(codingStart, PLAN_MS * 0.7, [
    { kind: "run_started", taskKind: "implementation", runtime: "claude-code", model: MODEL },
    ...reads,
    { kind: "agent_progress", phrase: `Planning ${input.version}` },
  ]);
  // The plan is written in one go, as the agent's task list is.
  for (const e of entries) {
    push(codingStart + PLAN_MS * 0.75, { kind: "work_item", source: "plan", itemId: e.id, title: e.title, itemStatus: "pending" });
  }
  const entryMs = input.repair ? FIX_ENTRY_MS : ENTRY_MS;
  let at = codingStart + PLAN_MS;
  for (const entry of entries) {
    push(at, { kind: "work_item", source: "plan", itemId: entry.id, title: entry.title, itemStatus: "in_progress" });
    spread(at, entryMs - 100, entry.work);
    push(at + entryMs - 50, { kind: "work_item", source: "plan", itemId: entry.id, title: entry.title, itemStatus: "completed" });
    at += entryMs;
  }
  push(at + 100, { kind: "git_commit", sha: "4f2a9c1e", files: entries.length * 3 });
  push(at + 200, { kind: "git_push", branch: `aep/${input.version}` });
  const coding: CycleSpan = { id: codingId, kind: input.repair ? "fix" : "coding", created: codingStart, ended: at + 400 };
  frames.push({ at: coding.ended, frame: { type: "cycle", cycle: cycleRecord(input, coding, coding.ended) } });

  // The validation cycle: every criterion planned, then judged one by one.
  const scenarios = scenariosOf(input.criteria);
  const vStart = coding.ended + 300;
  const validation: CycleSpan = { id: validationId, kind: "validation", created: vStart, ended: vStart + 600 + scenarios.length * SCENARIO_MS + 300 };
  frames.push({ at: vStart, frame: { type: "cycle", cycle: cycleRecord(input, { ...validation, ended: Infinity }, vStart) } });
  cycleId = validationId;
  push(vStart + 100, { kind: "run_started", taskKind: "validation", runtime: "claude-code", model: MODEL });
  scenarios.forEach((_, i) => push(vStart + 300, { kind: "work_item", source: "criterion", itemId: `AC-${i + 1}`, itemStatus: "planned" }));
  scenarios.forEach((s, i) => {
    const t = vStart + 600 + i * SCENARIO_MS;
    const fails = s.story !== null && s.story === input.failing;
    const id = `v${i}`;
    push(t, { kind: "work_item", source: "criterion", itemId: `AC-${i + 1}`, itemStatus: "running" });
    push(t + 100, { kind: "tool_use", tool: "Bash", summary: `validate @story-${s.story ?? "?"} ${s.name}`, toolUseId: id });
    push(t + SCENARIO_MS - 100, {
      kind: "tool_result",
      tool: "Bash",
      ok: !fails,
      exitCode: fails ? 1 : 0,
      durationMs: SCENARIO_MS * 4,
      ...(fails ? { error: "expected step failed" } : {}),
      toolUseId: id,
    });
    push(t + SCENARIO_MS - 50, { kind: "work_item", source: "criterion", itemId: `AC-${i + 1}`, itemStatus: fails ? "fail" : "pass" });
  });
  push(validation.ended - 200, { kind: "git_commit", sha: "9b1d07aa", files: 1 });
  frames.push({ at: validation.ended, frame: { type: "cycle", cycle: cycleRecord(input, validation, validation.ended) } });
  const end = validation.ended + 200;
  frames.push({ at: end, frame: { type: "done", state: "succeeded" } });
  frames.sort((a, b) => a.at - b.at);
  return { input, frames, coding, validation, end };
}

function iso(input: RunInput, at: number): string {
  return new Date(input.startedAt + at).toISOString();
}

function cycleRecord(input: RunInput, span: CycleSpan, now: number): RunCycleView {
  const ended = now >= span.ended;
  const verdict = input.failing ? "failed" : "passed";
  return {
    id: span.id,
    kind: span.kind,
    attempts: 1,
    createdAt: iso(input, span.created),
    endedAt: ended ? iso(input, span.ended) : null,
    recording: ended ? "complete" : "recording",
    ...(ended ? { mergeSha: `${span.id}-merge`, prNumber: span.kind === "validation" ? 12 : 11 } : {}),
    ...(ended && span.kind === "validation" ? { validationVerdict: verdict } : {}),
  };
}

/** The frames that have happened `t` ms after the run started. */
export function framesUntil(script: RunScript, t: number): TimedFrame[] {
  return script.frames.filter((f) => f.at <= t);
}

/** The run row at `t`: planning, then running, then settled with its verdict. */
export function runAt(script: RunScript, t: number): MilestoneRunView {
  const { input, coding, validation, end } = script;
  const cycles = [coding, validation].filter((c) => t >= c.created).map((c) => cycleRecord(input, c, t));
  const settled = t >= end;
  return {
    id: input.runId,
    kind: "dev",
    origin: "spec-build",
    milestoneNumber: input.milestoneNumber,
    milestoneTitle: input.version,
    createdAt: iso(input, 0),
    startedAt: iso(input, 0),
    endedAt: settled ? iso(input, end) : null,
    state: settled ? "succeeded" : t < coding.created ? "planning" : "running",
    budgets: { buildRetriggers: 0, conflictCycles: 0, cycleCeiling: 6, cyclesTotal: cycles.length, fixCycles: 0, validationCycles: t >= validation.created ? 1 : 0 },
    cycles,
    validation: settled
      ? { verdict: input.failing ? "failed" : "passed", reportPath: "tests/acceptance/report.json" }
      : {},
  };
}

/** The version's ledger line at `t`. */
export function summaryAt(script: RunScript, t: number): BuildSummary {
  const settled = t >= script.end;
  return {
    tag: script.input.version,
    milestoneNumber: script.input.milestoneNumber,
    startedAt: iso(script.input, 0),
    completedAt: settled ? iso(script.input, script.end) : null,
    status: settled ? "completed" : "in_progress",
  };
}

/**
 * One validation attempt's evidence at `t`: the acceptance files it judges,
 * and its report once the attempt has committed one. Null for a cycle that is
 * not this run's validation cycle, or has not started.
 */
export function snapshotAt(script: RunScript, t: number, cycleId: string): ValidationSnapshot | null {
  const { validation, input } = script;
  if (cycleId !== validation.id || t < validation.created) return null;
  const settled = t >= validation.ended;
  return {
    commit: settled ? `${validation.id}-merge` : "",
    criteria: input.criteria,
    report: settled ? reportFor(input, scenariosOf(input.criteria)) : null,
  };
}

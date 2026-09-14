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

// The crew model, tested against RUNS THAT HAPPENED.
//
// Two recordings, converted once by the two producers that convert for real (see
// fixtures/README.md). Hand-written events agree with whatever the model does;
// these do not — the 73-second gap the amber rule is pinned to, the 41-minute
// stretch the lead spent blocked, and the agent whose caption is a raw phase id
// are all things a real run produced and none of them would have been invented.

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import {
  buildCrew,
  crewTone,
  isCrewSettled,
  LEAD_AGENT_ID,
  planTone,
  STALL_MS,
  type Crew,
  type CrewMember,
  type RunEventView,
} from "../src/index.js";

function fixture(name: string): RunEventView[] {
  return fs
    .readFileSync(new URL(`./fixtures/${name}`, import.meta.url), "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as RunEventView);
}

/** The 17-second probe: three backgrounded subagents, one with a child. */
const PROBE = fixture("probe1-background-fanout.v2.ndjson");
/** The real 55-minute coding run, lifted from its v1 recording. */
const RUN = fixture("run-2026-09-04.v2.ndjson");

/** The agent ids the runtime itself declared in the probe. */
const P = {
  alpha: "ac7f4459090cb6845",
  beta: "a8562a681e0fcee7f",
  gamma: "a6e8d6b9cd56b107f",
  gammaChild: "a174ace70ca17fabb",
};

/** The two subagents of the real run, keyed by the tool call that spawned them. */
const R = {
  webapp: "toolu_01H8anDGmAxeWNekRqPU4w8n",
  walk: "toolu_01FiKFBATWHmkQRRFCmVEmvN",
};

/** When an event was emitted, by its `seq`. */
function tsOf(events: readonly RunEventView[], seq: number): number {
  const found = events.find((e) => e.seq === seq);
  assert.ok(found?.ts, `no timestamped event with seq ${String(seq)}`);
  return Date.parse(found.ts);
}

/** The instant the last thing in a recording was said. */
function endOf(events: readonly RunEventView[]): number {
  const last = events.filter((e) => e.ts).at(-1);
  assert.ok(last?.ts);
  return Date.parse(last.ts);
}

function memberOf<E>(crew: Crew<E>, id: string): CrewMember<E> {
  const found = crew.members.find((m) => m.id === id);
  assert.ok(found, `no crew member ${id}`);
  return found;
}

/** A lane as a reader sees it: which stretches, and how many seconds each ran. */
function laneShape(member: CrewMember<RunEventView>): [string, number][] {
  return member.spans.map((s) => [s.kind, Math.round((s.endMs - s.startMs) / 1000)]);
}

// --- the tree, as the runtime declared it -----------------------------------

test("crew: a real background fan-out reads as the tree the runtime declared", () => {
  const crew = buildCrew(PROBE, endOf(PROBE));

  // Five agents: the lead, the three it launched in one turn, and the child one
  // of those launched. Depth-first from the lead, which is both the lane order
  // and the order the tree is read down.
  assert.deepEqual(
    crew.members.map((m) => [m.id, m.depth]),
    [
      ["lead", 0],
      [P.alpha, 1],
      [P.beta, 1],
      [P.gamma, 1],
      [P.gammaChild, 2],
    ],
  );
  assert.equal(crew.agents, 5);

  // Parentage is DECLARED, not guessed: the depth-2 child names gamma, and v1
  // could not describe this shape at all — it filed a grandchild under the lead.
  assert.equal(memberOf(crew, P.gammaChild).parentId, P.gamma);
  assert.equal(memberOf(crew, P.gamma).children.map((c) => c.id).join(), P.gammaChild);
  assert.deepEqual(
    crew.lead.children.map((c) => c.agent.label),
    ["probe alpha", "probe beta", "probe gamma"],
  );

  // All three were launched with `run_in_background: true` and the platform's
  // forcing did not touch them — which is a fact about this recording, and the
  // reason their parent never stops for them (see the blocking test below).
  assert.deepEqual(
    crew.lead.children.map((c) => c.background),
    [true, true, true],
  );
  assert.equal(memberOf(crew, P.gammaChild).background, false);
});

test("crew: a settled agent's sub-line is its own report, and its figures are the runtime's", () => {
  const crew = buildCrew(PROBE, endOf(PROBE));

  // The report is the ONLY copy: a spawned agent's transcript never reaches this
  // feed and dies with its pod.
  assert.deepEqual(
    crew.members.map((m) => m.caption),
    [
      // The lead never emitted an `agent_settled`, so it has no report — and an
      // empty caption is a surface rendering no sub-line, never a blank one.
      "",
      "REPORT: alpha done, 1 file",
      "REPORT: beta done, 1 file",
      "REPORT: gamma done",
      "DONE",
    ],
  );

  // Reported, never re-derived: nothing on this feed can reconstruct a spawned
  // agent's own total, and its individual edits need not reach the feed at all.
  assert.equal(memberOf(crew, P.alpha).elapsedMs, 9660);
  assert.equal(memberOf(crew, P.gammaChild).elapsedMs, 4873);
  // The lead has no reported total, so its life is measured off its own events.
  assert.equal(crew.lead.elapsedMs, 15_746);

  assert.equal(crew.running, 0);
  assert.equal(crew.outcome, "success");
  for (const m of crew.members) assert.ok(isCrewSettled(m.state), `${m.id} is ${m.state}`);
});

test("crew: a run that settled settles the agents it never heard from", () => {
  // The v1 runner announced no agents and settled only the two it inferred; the
  // LEAD has no `agent_settled` anywhere in 761 events. A lead still reading
  // `running` on a build that finished an hour ago is the clearest way to make a
  // reader mistrust the whole surface, so the run's own outcome is the last word.
  const settled = RUN.filter((e) => e.kind === "agent_settled");
  assert.deepEqual(settled.map((e) => e.agentId), [R.webapp, R.walk]);

  const crew = buildCrew(RUN, endOf(RUN) + 3_600_000);
  assert.equal(crew.outcome, "success");
  assert.equal(crew.lead.state, "done");
  assert.equal(crew.running, 0);
  // Its lane ends where the RUN ended, not at the caller's clock — otherwise a
  // finished run's lead would grow a longer bar every second the page is open.
  assert.equal(crew.lead.spans.at(-1)?.endMs, endOf(RUN));
});

// --- where the time went ----------------------------------------------------

test("crew: the lead's lane splits into the work it did and the agents it waited on", () => {
  const crew = buildCrew(RUN, endOf(RUN));

  // The headline this view exists for. The lead looks like the slowest thing in
  // a 55-minute run until you see that 41 of those minutes are ONE unbroken
  // stretch of it blocked inside the agent building a web app — at which point
  // "where did the time go" has an answer, and it is not the lead.
  assert.deepEqual(laneShape(crew.lead), [
    ["working", 288],
    ["waiting", 2466],
    ["working", 13],
    ["waiting", 467],
    ["working", 24],
  ]);

  // The two waits are exactly the two subagents' own lanes — the parent is
  // charged for their time on its row and for nothing else.
  const webapp = memberOf(crew, R.webapp);
  const walk = memberOf(crew, R.walk);
  assert.deepEqual(laneShape(webapp), [["working", 2466]]);
  assert.deepEqual(laneShape(walk), [["working", 467]]);
  assert.equal(crew.lead.spans[1]?.startMs, webapp.spans[0]?.startMs);
  assert.equal(crew.lead.spans[3]?.endMs, walk.spans.at(-1)?.endMs);

  // Every lane is drawn against ONE axis: the run's first word to its last.
  assert.equal(crew.window.startMs, Date.parse("2026-09-04T09:25:39.580Z"));
  assert.equal(crew.window.endMs, Date.parse("2026-09-04T10:19:57.373Z"));
});

test("crew: a foreground child splits its parent's lane; a background one never does", () => {
  const crew = buildCrew(PROBE, endOf(PROBE));

  // gamma stopped for the child it spawned in the foreground, so its lane has a
  // waiting stretch in the middle of its work.
  assert.deepEqual(laneShape(memberOf(crew, P.gamma)), [
    ["working", 2],
    ["waiting", 5],
    ["working", 2],
  ]);
  // The lead launched all three of ITS agents detached, so it never stopped —
  // one unbroken working stretch across the whole run.
  assert.deepEqual(laneShape(crew.lead), [["working", 16]]);
});

test("crew: an agent nothing timestamped gets no lane and no age, and still gets a row", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: "a1", label: "unstamped", depth: 1 },
      { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build" },
    ],
    1_000_000,
  );
  const member = memberOf(crew, "a1");
  assert.deepEqual(member.spans, []);
  assert.equal(member.silentForMs, undefined);
  assert.equal(member.elapsedMs, undefined);
  // A row all the same, with the name and the state it does have — an agent
  // dropped for want of a clock is an agent a reader never learns ran.
  assert.equal(member.agent.label, "unstamped");
  assert.equal(member.state, "working");
  assert.deepEqual(crew.window, { startMs: 0, endMs: 0 });
});

test("crew: a zero-value date is absent data and never an instant", () => {
  // The first line is VERBATIM what aep-api put on a live console feed: a
  // platform notice narrating the dark zone, whose `ts` was left at Go's zero
  // value and marshalled into a perfectly well-formed date. The lead's age
  // column read `1065409035m47s` — 2026 years, the interval from Go's zero
  // time to the afternoon someone was watching — beside a heartbeat line that
  // was correct, because that one reads an elapsed the producer measured.
  const now = Date.parse("2026-09-09T09:15:47Z");
  const startedAt = Date.parse("2026-09-09T09:00:00Z");
  const crew = buildCrew(
    [
      { kind: "notice", agentId: "lead", seq: -11, code: "runner_pulling_image", ts: "0001-01-01T00:00:00Z" },
      { kind: "run_started", agentId: "lead", seq: 1, ts: "2026-09-09T09:00:00Z" },
      { kind: "heartbeat", agentId: "lead", seq: 2, waitingOn: "model", elapsedMs: 13_000, ts: "2026-09-09T09:15:34Z" },
    ],
    now,
  );
  // The age is measured from the first instant the run can be shown to have
  // reached — never from a stamp that predates the platform.
  assert.equal(crew.lead.elapsedMs, now - startedAt);
  // And the axis both lanes and the timeline are drawn against stays inside the
  // run: a zero-value date at the head of a recording used to stretch it over
  // two millennia — the timeline read "1065409043m56s across 3 agents" and drew
  // the lead across the whole width, with every real agent a sliver.
  assert.deepEqual(crew.window, { startMs: startedAt, endMs: now });
  assert.equal(crew.lead.spans[0]?.startMs, startedAt);
});

test("crew: a row whose only stamp is a zero-value date shows no age at all", () => {
  // Same rule as an agent nothing timestamped: the surface falls back to no age
  // rather than inventing one. A date it cannot believe is not better evidence
  // than no date, and a number a reader trusts is worse than a blank.
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: "a1", label: "zero-stamped", depth: 1 },
      { kind: "notice", agentId: "a1", detail: "npm notice", ts: "0001-01-01T00:00:00Z" },
    ],
    Date.parse("2026-09-09T09:15:47Z"),
  );
  const member = memberOf(crew, "a1");
  assert.equal(member.elapsedMs, undefined);
  assert.equal(member.silentForMs, undefined);
  assert.deepEqual(member.spans, []);
  assert.equal(member.agent.label, "zero-stamped");
});

// --- liveness: the amber rule ------------------------------------------------

test("crew: the amber rule fires at exactly 60s of silence with a call unanswered", () => {
  // A real gap: this agent issued a Bash loop over twelve component directories
  // and said nothing for 73.65 seconds before its result arrived. Sliced at the
  // call, so the recording itself is what makes the agent silent.
  const openedAt = tsOf(RUN, 484);
  const answeredAt = tsOf(RUN, 486);
  assert.equal(answeredAt - openedAt, 73_650, "the recording's own gap");
  const upToTheCall = RUN.slice(0, RUN.findIndex((e) => e.seq === 486));

  const before = memberOf(buildCrew(upToTheCall, openedAt + STALL_MS - 1), R.webapp);
  assert.equal(before.state, "working");
  assert.equal(before.silentForMs, STALL_MS - 1);

  const at = memberOf(buildCrew(upToTheCall, openedAt + STALL_MS), R.webapp);
  assert.equal(at.state, "stalled");
  assert.equal(at.silentForMs, STALL_MS);
  assert.equal(crewTone(at.state), "warn");

  // And it goes back to blue on the very next thing it says, without anybody
  // clearing a flag: the rule is read off the clock, not latched.
  const answered = memberOf(buildCrew(RUN.slice(0, 400), answeredAt), R.webapp);
  assert.equal(answered.state, "working");
});

test("crew: silence alone is never a verdict — no call in flight, no amber", () => {
  const opened = [
    { kind: "agent_started", agentId: "lead", ts: "2026-09-04T09:00:00Z" },
    { kind: "agent_progress", agentId: "lead", phrase: "Reading the design", ts: "2026-09-04T09:00:01Z" },
  ];
  // Ten minutes of nothing. The age is the honest report and the row says it;
  // inventing a failure out of it would put a red row on a run that is thinking.
  const crew = buildCrew(opened, Date.parse("2026-09-04T09:10:01Z"));
  assert.equal(crew.lead.state, "working");
  assert.equal(crew.lead.silentForMs, 600_000);
  assert.equal(crew.lead.caption, "Reading the design");
  assert.equal(crew.silentForMs, 600_000);
});

test("crew: a fan-out call is not a call in flight — a healthy fan-out never turns amber", () => {
  const ts = (s: number) => new Date(Date.parse("2026-09-04T09:00:00Z") + s * 1000).toISOString();
  const events = [
    { kind: "tool_use", agentId: "lead", tool: "Agent", summary: "Build the API", toolUseId: "fan1", ts: ts(0) },
    { kind: "agent_started", agentId: "a1", label: "Build the API", depth: 1, background: false, ts: ts(1) },
    { kind: "tool_use", agentId: "a1", tool: "Bash", summary: "bal build", toolUseId: "t1", ts: ts(2) },
  ];
  const crew = buildCrew(events, Date.parse("2026-09-04T09:30:00Z"));
  // Half an hour later the lead's `Agent` call is still unanswered — and that is
  // not a stall, it is the fan-out working. The child's own row is the wait.
  assert.equal(crew.lead.state, "waiting");
  assert.equal(memberOf(crew, "a1").state, "stalled");
});

test("crew: a lead blocked in a foreground agent waits on it BY NAME, never idle", () => {
  const crew = buildCrew(RUN.slice(0, 400), tsOf(RUN, 486));
  assert.equal(crew.lead.state, "waiting");
  assert.equal(crew.lead.waitingOnId, R.webapp);
  // Named, so a reader can go and look at the row that explains the silence —
  // "waiting on a spawned agent" makes them hunt for which one. The elapsed is
  // derived from the clock the caller passed, so it TICKS; a heartbeat's own
  // figure froze the moment it was emitted.
  assert.equal(
    crew.lead.caption,
    "waiting on Build onboarding-webapp React SPA for 16m12s",
  );
  assert.equal(crewTone(crew.lead.state), "info", "a healthy wait is not a warning");
});

test("crew: a BACKGROUND child never blocks its parent", () => {
  // Fourteen events in, all three probe agents are alive and the lead has gone
  // quiet — but they were launched DETACHED, so the lead did not stop for them.
  const opening = PROBE.slice(0, 14);
  const crew = buildCrew(opening, endOf(opening) + 90_000);

  assert.equal(crew.lead.state, "working");
  assert.equal(crew.lead.waitingOnId, undefined);
  // gamma's child, on the other hand, is a foreground spawn: gamma waits on it.
  assert.equal(memberOf(crew, P.gamma).state, "waiting");
  assert.equal(memberOf(crew, P.gamma).caption, "waiting on probe gamma-child for 1m32s");
  assert.equal(crew.running, 5);
});

test("crew: ages come from the clock the caller passes, never from one of its own", () => {
  const opening = PROBE.slice(0, 14);
  const last = endOf(opening);
  const early = buildCrew(opening, last + 5_000);
  const later = buildCrew(opening, last + 65_000);
  // Same events, different instant: every age moved by the 60 seconds and by
  // nothing else. This is what lets a surface tick a row with no new events —
  // and what lets a test drive the model to the exact instant a rule fires.
  assert.equal(later.silentForMs! - early.silentForMs!, 60_000);
  assert.equal(memberOf(later, P.alpha).silentForMs! - memberOf(early, P.alpha).silentForMs!, 60_000);
});

// --- what the row says -------------------------------------------------------

test("crew: a heartbeat's wait is the caption, and it names the agent it names", () => {
  const events = [
    { kind: "agent_started", agentId: "a1", label: "Implement todo-api", depth: 1, ts: "2026-09-04T09:00:00Z" },
    { kind: "heartbeat", agentId: "lead", waitingOn: "agent", ref: "a1", elapsedMs: 130_000, ts: "2026-09-04T09:02:10Z" },
  ];
  const crew = buildCrew(events, Date.parse("2026-09-04T09:02:11Z"));
  // The watchdog's sentence stops being a log line and becomes the row's
  // caption. Here the lead is blocked in a foreground child, so the LIVE wait
  // wins over the heartbeat's frozen figure — same wording, ticking number.
  assert.equal(crew.lead.caption, "waiting on Implement todo-api for 2m11s");

  // With nothing in the tree to resolve, the heartbeat's own wording stands.
  const alone = buildCrew(
    [{ kind: "heartbeat", agentId: "lead", waitingOn: "model", elapsedMs: 42_000, ts: "2026-09-04T09:00:42Z" }],
    Date.parse("2026-09-04T09:00:43Z"),
  );
  assert.equal(alone.lead.caption, "waiting on the model for 42.0s");
});

test("crew: a notice explains the silence only while it is still the newest thing", () => {
  const ts = (s: number) => new Date(Date.parse("2026-09-04T09:00:00Z") + s * 1000).toISOString();
  const retry = [
    { kind: "agent_progress", agentId: "lead", phrase: "Writing src/api/shorten.ts", ts: ts(0) },
    { kind: "notice", agentId: "lead", level: "warn", code: "api_retry", detail: "overloaded_error, retrying in 4s", ts: ts(1) },
  ];
  // The retry IS why nothing is happening, so it is what the row says — the
  // wording is this package's, the specifics are the producer's.
  assert.equal(
    buildCrew(retry, Date.parse(ts(2))).lead.caption,
    "retrying after a model error · overloaded_error, retrying in 4s",
  );
  // …and the moment the agent does something else, the retry explains nothing
  // and the agent's own phrase is the truer line.
  const recovered = [...retry, { kind: "tool_use", agentId: "lead", tool: "Write", summary: "src/api/shorten.ts", ts: ts(3) }];
  assert.equal(buildCrew(recovered, Date.parse(ts(4))).lead.caption, "Writing src/api/shorten.ts");

  // A notice about something that already went wrong is news, not an
  // explanation of the present, and never displaces the phrase.
  const denied = [...retry.slice(0, 1), { kind: "notice", agentId: "lead", level: "warn", code: "workspace_guard", detail: "/etc/hosts", ts: ts(1) }];
  assert.equal(buildCrew(denied, Date.parse(ts(2))).lead.caption, "Writing src/api/shorten.ts");
});

test("crew: an amber row always says what it is amber about", () => {
  const ts = (s: number) => new Date(Date.parse("2026-09-04T09:00:00Z") + s * 1000).toISOString();
  // No heartbeat, no phrase — which is the normal case, because the recorded
  // runs carry almost no heartbeats. The unanswered call is the only witness
  // there is, and it is a true one.
  const crew = buildCrew(
    [{ kind: "tool_use", agentId: "lead", tool: "Bash", summary: "bal build", toolUseId: "t1", ts: ts(0) }],
    Date.parse(ts(0)) + 95_000,
  );
  assert.equal(crew.lead.state, "stalled");
  assert.equal(crew.lead.caption, "waiting on Bash for 1m35s");
});

test("crew: a failed agent is the only red — a stopped one was taken away, not broken", () => {
  const events = [
    { kind: "agent_started", agentId: "a1", label: "api", depth: 1, ts: "2026-09-04T09:00:00Z" },
    { kind: "agent_settled", agentId: "a1", status: "failed", durationMs: 353_000, report: "vite build never passed.", ts: "2026-09-04T09:05:53Z" },
    { kind: "agent_started", agentId: "a2", label: "webapp", depth: 1, ts: "2026-09-04T09:00:00Z" },
    { kind: "agent_settled", agentId: "a2", status: "stopped", durationMs: 12_000, ts: "2026-09-04T09:00:12Z" },
  ];
  const crew = buildCrew(events, Date.parse("2026-09-04T09:10:00Z"));
  assert.equal(memberOf(crew, "a1").state, "failed");
  assert.equal(crewTone("failed"), "error");
  assert.equal(memberOf(crew, "a1").caption, "vite build never passed.");
  // A cancellation is not a defect: the work did not go wrong, it was taken
  // away, and a run somebody stopped on purpose must not be reported as broken.
  assert.equal(memberOf(crew, "a2").state, "cancelled");
  assert.equal(crewTone("cancelled"), "warn");
});

// --- backgrounded shell commands --------------------------------------------

test("crew: a backgrounded shell command is a child of the agent that started it", () => {
  const events = [
    { kind: "agent_started", agentId: "a1", label: "webapp", depth: 1, ts: "2026-09-04T09:00:00Z" },
    { kind: "task_started", agentId: "a1", taskId: "bg1", summary: "pnpm dev:mock", ts: "2026-09-04T09:00:01Z" },
    { kind: "task_settled", agentId: "a1", taskId: "bg1", summary: "pnpm dev:mock", status: "failed", outputBytes: 20_480, ts: "2026-09-04T09:04:01Z" },
    // A settle whose start was never seen still gets a row: a backgrounded
    // command nobody can account for is exactly the one worth showing.
    { kind: "task_settled", agentId: "a1", taskId: "bg2", summary: "pnpm build", status: "completed", ts: "2026-09-04T09:05:00Z" },
  ];
  const crew = buildCrew(events, Date.parse("2026-09-04T09:06:00Z"));
  const owner = memberOf(crew, "a1");
  assert.deepEqual(
    owner.tasks.map((t) => [t.id, t.label, t.status]),
    [
      ["bg1", "pnpm dev:mock", "failed"],
      ["bg2", "pnpm build", "completed"],
    ],
  );
  assert.equal(owner.tasks[0]?.outputBytes, 20_480);
  // Filed under its owner, so an orphaned `dev:mock` holding a port after the
  // run ends has somebody's name on it.
  assert.equal(crew.lead.tasks.length, 0);
});

// --- what the inspector reads ------------------------------------------------

test("crew: a member's steps are its OWN, and none of them are the lifecycle events", () => {
  const crew = buildCrew(PROBE, endOf(PROBE));
  const alpha = memberOf(crew, P.alpha);
  // Its own six rows: three calls and their three outcomes. Its start, its
  // settle and its phrases are the HEADER — a settle repeated as a step would
  // report the whole agent as a step in itself.
  assert.deepEqual(
    alpha.steps.map((e) => e.kind),
    ["tool_use", "tool_use", "tool_use", "tool_result", "tool_result", "tool_result"],
  );
  assert.ok(alpha.steps.every((e) => e.agentId === P.alpha));
  // A depth-2 child's steps stay on the child, never on the parent that
  // spawned it — gamma forwarded nothing of its own.
  assert.equal(memberOf(crew, P.gamma).steps.length, 0);
  assert.equal(memberOf(crew, P.gammaChild).steps.length, 2);
  // Every step of every agent, and nothing double-counted.
  assert.equal(
    crew.members.reduce((n, m) => n + m.steps.length, 0),
    24,
  );
});

test("crew: the live phrase lives on the caption and nowhere else", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: "a1", label: "api", depth: 1, ts: "2026-09-04T09:00:00Z" },
      { kind: "agent_progress", agentId: "a1", phrase: "Reading the existing handler", ts: "2026-09-04T09:00:01Z" },
    ],
    Date.parse("2026-09-04T09:00:02Z"),
  );
  const member = memberOf(crew, "a1");
  assert.equal(member.caption, "Reading the existing handler");
  // One home for it. A surface that renders the header and the sub-line from
  // the same member must not be able to print the same sentence twice.
  assert.equal(member.agent.activity, undefined);
});

test("crew: the hint both views share counts agents, not rows", () => {
  const opening = PROBE.slice(0, 14);
  const live = buildCrew(opening, endOf(opening) + 1_000);
  assert.equal(live.agents, 5);
  assert.equal(live.running, 5);
  assert.equal(live.outcome, undefined);

  const finished = buildCrew(PROBE, endOf(PROBE));
  assert.equal(finished.agents, 5);
  assert.equal(finished.running, 0);
  assert.equal(finished.outcome, "success");
});

test("crew: the real run's phrases are the runtime's own words, unimproved", () => {
  // The lift renders a pre-cutover run's `phase` line as an `agent_progress`
  // whose phrase is the raw phase id. It reads poorly and it is TRUE — a phrase
  // is the runtime's own words, and this package does not rewrite them. A model
  // that prettified this would be inventing a sentence the run never said.
  const crew = buildCrew(RUN.slice(0, 1), Date.parse("2026-09-04T09:25:40Z"));
  assert.equal(crew.lead.caption, "workspace_provisioning");
});

// --- the agent's own plan ----------------------------------------------------
//
// The lead's task list reaches the feed as `work_item {source: "plan"}`, which
// is a SILENT kind: one entry moving pending → in_progress → completed is one
// row repainted, never three rows printed. Folding is the only way to show it at
// all, and it folds HERE so the console's tree and the playground's block cannot
// come to describe one list two ways.

test("crew: the plan folds by item — last status wins, the title sticks, deleted rows go", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "lead agent", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "a", title: "Scaffold", itemStatus: "pending", ts: "2026-09-04T09:00:01Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "b", title: "Walk it", itemStatus: "pending", ts: "2026-09-04T09:00:02Z" },
      // A later update carries only the status — taking the newest title
      // blindly would blank the row the moment the agent ticked it off.
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "a", itemStatus: "completed", ts: "2026-09-04T09:00:03Z" },
      // Removed from the list: not work any more, so not a row.
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "b", itemStatus: "deleted", ts: "2026-09-04T09:00:04Z" },
    ],
    Date.parse("2026-09-04T09:00:05Z"),
  );
  assert.deepEqual(crew.lead.plan, [{ id: "a", title: "Scaffold", status: "completed" }]);
});

test("crew: a criterion is not a plan entry, however much of the kind they share", () => {
  // A criterion is the PLATFORM's unit of work in a validating run, with the
  // validation method's own statuses. `completed` says a plan entry was ticked
  // off; `pass` says a criterion was asserted and held. Folding one here would
  // paint acceptance criteria onto an agent's to-do list.
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "validator", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "criterion", itemId: "AC-001-a", itemStatus: "planned", ts: "2026-09-04T09:00:01Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "criterion", itemId: "AC-001-a", itemStatus: "pass", ts: "2026-09-04T09:00:02Z" },
      // No `source` at all is a producer this build does not understand. It is
      // not assumed to be a plan: an unreadable entry on somebody's to-do list
      // is worse than no entry.
      { kind: "work_item", agentId: LEAD_AGENT_ID, itemId: "x", title: "Unsourced", itemStatus: "pending", ts: "2026-09-04T09:00:03Z" },
    ],
    Date.parse("2026-09-04T09:00:04Z"),
  );
  assert.deepEqual(crew.lead.plan, []);
});

test("crew: an entry belongs to the agent it names, and an unnamed one to the lead", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "lead agent", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "agent_started", agentId: "a1", label: "webapp", parentAgentId: LEAD_AGENT_ID, depth: 1, ts: "2026-09-04T09:00:01Z" },
      // No owner: the lead's own, because the lead is who keeps the list.
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p1", title: "Read the design", itemStatus: "completed", ts: "2026-09-04T09:00:02Z" },
      // Handed to a spawned agent. It draws under that agent, because "what was
      // this one sent to do" is the question a reader has about ITS row.
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p2", title: "Build the front end", itemStatus: "in_progress", ownerAgentId: "a1", ts: "2026-09-04T09:00:03Z" },
      // An owner no event ever mentioned still belongs to somebody: it falls to
      // the lead rather than vanishing, the same way the grouping hangs an
      // undeclared agent off the lead.
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p3", title: "Orphaned entry", itemStatus: "pending", ownerAgentId: "ghost", ts: "2026-09-04T09:00:04Z" },
    ],
    Date.parse("2026-09-04T09:00:05Z"),
  );
  assert.deepEqual(
    crew.lead.plan.map((p) => [p.id, p.title, p.status]),
    [
      ["p1", "Read the design", "completed"],
      ["p3", "Orphaned entry", "pending"],
    ],
  );
  assert.deepEqual(memberOf(crew, "a1").plan.map((p) => p.id), ["p2"]);
});

test("crew: an entry keeps its place when it is ticked off, and its owner sticks", () => {
  // A row that jumps down the list the moment somebody starts it is a list
  // nobody can read while it moves, so the order is the order the run FIRST
  // mentioned each entry. The owner is sticky for the same reason the title is:
  // a status-only update carries neither.
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "lead agent", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "agent_started", agentId: "a1", label: "webapp", parentAgentId: LEAD_AGENT_ID, depth: 1, ts: "2026-09-04T09:00:01Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p1", title: "First", itemStatus: "pending", ts: "2026-09-04T09:00:02Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p2", title: "Second", itemStatus: "pending", ownerAgentId: "a1", ts: "2026-09-04T09:00:03Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p3", title: "Third", itemStatus: "pending", ts: "2026-09-04T09:00:04Z" },
      { kind: "work_item", agentId: "a1", source: "plan", itemId: "p2", itemStatus: "completed", ts: "2026-09-04T09:00:05Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p1", itemStatus: "in_progress", ts: "2026-09-04T09:00:06Z" },
    ],
    Date.parse("2026-09-04T09:00:07Z"),
  );
  assert.deepEqual(
    crew.lead.plan.map((p) => [p.id, p.status]),
    [
      ["p1", "in_progress"],
      ["p3", "pending"],
    ],
  );
  // The update came from the spawned agent itself and named no owner. It stays
  // where it was put, rather than migrating to whoever last touched it.
  assert.deepEqual(memberOf(crew, "a1").plan, [
    { id: "p2", title: "Second", status: "completed" },
  ]);
});

test("crew: a settled agent keeps its plan, and a plan weighs by where it stands", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "lead agent", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", itemId: "p1", title: "Ship it", itemStatus: "completed", ts: "2026-09-04T09:00:01Z" },
      { kind: "agent_settled", agentId: LEAD_AGENT_ID, status: "completed", durationMs: 2_000, ts: "2026-09-04T09:00:02Z" },
      { kind: "run_settled", agentId: LEAD_AGENT_ID, outcome: "success", ts: "2026-09-04T09:00:02Z" },
    ],
    Date.parse("2026-09-04T09:05:00Z"),
  );
  // The list is what the agent SET OUT to do and whether it got there. Dropping
  // it on settle would delete the record just as it became a record — the same
  // reason a settled agent keeps its closing report.
  assert.ok(isCrewSettled(crew.lead.state));
  assert.deepEqual(crew.lead.plan.map((p) => p.title), ["Ship it"]);

  // Semantic weight, never a theme token: a TUI imports this package too.
  assert.equal(planTone("pending"), "muted");
  assert.equal(planTone("in_progress"), "info");
  assert.equal(planTone("completed"), "success");
  // A status this build has never heard of is quiet rather than loud.
  assert.equal(planTone("whatever_comes_next"), "muted");
});

test("crew: an item with no id is skipped, not folded into a row nothing repaints", () => {
  const crew = buildCrew(
    [
      { kind: "agent_started", agentId: LEAD_AGENT_ID, label: "lead agent", depth: 0, ts: "2026-09-04T09:00:00Z" },
      { kind: "work_item", agentId: LEAD_AGENT_ID, source: "plan", title: "No id", itemStatus: "pending", ts: "2026-09-04T09:00:01Z" },
    ],
    Date.parse("2026-09-04T09:00:02Z"),
  );
  assert.deepEqual(crew.lead.plan, []);
});

// --- what a consumer does with an author the producer never declared ---------

/**
 * A slice of the live coding run of 2026-09-08, while the producer was minting
 * an agent id per blocking wait. Two real agents, two phantoms, no settles.
 */
const PHANTOMS = fixture("run-2026-09-08-phantom-agents.v2.ndjson");

/** The two agents that run declared, and the two tool-call ids that got counted. */
const F = {
  api: "a69fa9b66eb3c8f95",
  webapp: "a9a6bd95fa3a4592f",
  phantomA: "toolu_015F5FWtAF7aF4WG4ni6Gnxu",
  phantomB: "toolu_01SZ1fENcA421vcG6PXyG2zd",
};

// The honest answer is COUNT THEM, and it is not a shrug.
//
// A consumer cannot tell a phantom from a legitimately undeclared agent. The
// only thing separating `toolu_015F…` from `a69fa9b66eb3c8f95` is one runtime's
// id prefix, and this package must never branch on that — the contract's rule is
// that runtime names never reach a consumer's logic. The prefix is not even a
// reliable tell: `run-2026-09-04.v2.ndjson` is a REAL run whose two real agents
// are both named `toolu_…`, because aep-api's v1 lift keys an inferred agent by
// the tool call that spawned it. A renderer that dropped `toolu_`-shaped ids
// would delete both agents of that run.
//
// So a surface reports what the producer said. The console's "5 agents · 5
// running" was a true rendering of a false feed, and the fix is the producer's
// invariant (`claude_adapter.test.ts`: every agentId is `lead` or an id the
// runtime declared). Dropping the events here would have hidden that bug for as
// long as it kept happening.
test("crew: an author nobody declared is still counted — a surface reports the feed it was given", () => {
  const crew = buildCrew(PHANTOMS, endOf(PHANTOMS));

  assert.equal(crew.agents, 5, "the lead, the two real agents, and the two the producer invented");
  assert.deepEqual(crew.members.map((m) => m.id), [LEAD_AGENT_ID, F.api, F.webapp, F.phantomA, F.phantomB]);

  // Nothing is invented for an undeclared member: no role, and its own id as its
  // name, which is what makes it recognisable as unannounced rather than as a
  // badly named agent.
  for (const id of [F.phantomA, F.phantomB]) {
    const m = memberOf(crew, id);
    assert.equal(m.declared, false);
    assert.equal(m.agent.label, id);
    assert.equal(m.agent.role, undefined);
  }
  // And the declared two are unaffected — they carry the labels their parent
  // gave them, so the count above is not five of the same thing.
  assert.equal(memberOf(crew, F.api).declared, true);
  assert.equal(memberOf(crew, F.api).agent.label, "Build expense-api Ballerina service");
  assert.equal(memberOf(crew, F.webapp).agent.label, "Build expense-webapp React SPA");
});

// Counting them is honest. BLOCKING on them is not, and that half was a bug of
// this package's own: an undeclared child carries no `background`, `undefined`
// is not `true`, so it read as a foreground spawn and held the lead in
// `waiting` — which outranks the stall rule. Live, that is how five phantoms
// that could never settle turned the stall alarm off for the rest of a run.
test("crew: an undeclared child never blocks its parent, so it cannot mute the stall rule", () => {
  const crew = buildCrew(PHANTOMS, endOf(PHANTOMS));

  assert.equal(crew.lead.waitingOnId, undefined, "waiting on an agent nobody announced is a claim, not a reading");
  assert.notEqual(crew.lead.state, "waiting");
  // The lane agrees with the state: no stretch of the lead's life is charged to
  // waiting on something that was never declared.
  assert.deepEqual(laneShape(crew.lead).map(([kind]) => kind).filter((k) => k === "waiting"), []);

  // The discriminator is DECLARED, not `background`, and the v1 lift is the
  // case that proves it: it announces its inferred agents with no `background`
  // field at all, and they must still block — the lift declared them, so the
  // relationship is the producer's claim rather than this package's guess. If
  // the rule were "absent background does not block", this run's 41-minute wait
  // would silently become 41 minutes of `working`.
  const lifted = buildCrew(RUN.slice(0, 400), tsOf(RUN, 486));
  assert.equal(memberOf(lifted, R.webapp).declared, true);
  assert.equal(memberOf(lifted, R.webapp).agent.background, undefined);
  assert.equal(lifted.lead.waitingOnId, R.webapp);
});

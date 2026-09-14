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

/**
 * The crew block: what it says, and how a terminal holds it.
 *
 * The pane is driven through its `isTTY` seam in BOTH positions, because the
 * hard rule — a piped run's transcript is byte-identical to what it was before
 * the block existed — is not something a run can be trusted to notice going
 * wrong. `replay` below is a two-escape terminal, enough to prove that a redraw
 * cannot eat the lines above it and that closing the pane leaves no residue.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { buildCrew, type RunEventView } from "@aep/progress-view";
import { renderCrewBlock } from "../src/engine/crew-block.js";
import { openCrewPane } from "../src/engine/crew-pane.js";
import { createAgentTags } from "../src/engine/agent-tags.js";
import { createTimelineRenderer } from "../src/engine/coding-run.js";

const T0 = Date.parse("2026-09-07T10:00:00.000Z");
const at = (ms: number): string => new Date(T0 + ms).toISOString();
const NOW = T0 + 45_000;

/** One fan-out run: the lead with a plan, a settled child, its background task. */
const RUN: RunEventView[] = [
  { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0, ts: at(0) },
  {
    kind: "work_item",
    agentId: "lead",
    source: "plan",
    itemId: "task_7",
    title: "Implement issue #3",
    itemStatus: "pending",
    ts: at(100),
  },
  { kind: "agent_progress", agentId: "lead", phrase: "authoring workload.yaml", ts: at(1_000) },
  {
    kind: "agent_started",
    agentId: "ag_web",
    parentAgentId: "lead",
    label: "Build onboarding-webapp React SPA",
    depth: 1,
    ts: at(2_000),
  },
  { kind: "task_started", agentId: "ag_web", taskId: "bg1", command: "npm run dev:mock", ts: at(3_000) },
  {
    kind: "agent_settled",
    agentId: "ag_web",
    status: "completed",
    durationMs: 41_000,
    toolCount: 162,
    report: "build clean",
    ts: at(41_000),
  },
];

const block = (
  events: readonly RunEventView[],
  columns = 100,
  maxRows = 30,
  now = NOW,
): string[] =>
  renderCrewBlock(buildCrew(events, now), now, {
    columns,
    maxRows,
    tag: createAgentTags(),
  }).map((r) => r.text);

const CSI = "\u001b[";

/** How many drawn lines this chunk takes back — the pane's "cursor up N, erase below". */
function linesCut(chunk: string): number {
  const tail = `A${CSI}0J`;
  if (!chunk.startsWith(CSI) || !chunk.endsWith(tail)) return 0;
  const n = Number(chunk.slice(CSI.length, chunk.length - tail.length));
  return Number.isInteger(n) && n > 0 ? n : 0;
}

/** Colour escapes, dropped — they are not what a reader sees. */
function stripColour(text: string): string {
  let out = "";
  let rest = text;
  for (;;) {
    const at = rest.indexOf(CSI);
    if (at < 0) return out + rest;
    out += rest.slice(0, at);
    const end = rest.indexOf("m", at);
    if (end < 0) return out + rest.slice(at);
    rest = rest.slice(end + 1);
  }
}

/**
 * A terminal that understands the only two escapes the pane emits: colour (which
 * it strips) and "cursor up N, erase to the end of the screen" (which drops the
 * last N committed lines). Replaying the writes gives the screen a reader would
 * actually be looking at.
 */
function replay(chunks: readonly string[]): string[] {
  const lines: string[] = [];
  let pending = "";
  for (const chunk of chunks) {
    const cut = linesCut(chunk);
    if (cut > 0) {
      lines.length = Math.max(0, lines.length - cut);
      continue;
    }
    pending += stripColour(chunk);
    const parts = pending.split("\n");
    pending = parts.pop() ?? "";
    for (const part of parts) lines.push(part);
  }
  if (pending) lines.push(pending);
  return lines;
}

function fakeTerminal(columns = 100, rows = 30) {
  const writes: string[] = [];
  return {
    writes,
    out: {
      write(chunk: string): boolean {
        writes.push(chunk);
        return true;
      },
      columns,
      rows,
    },
  };
}

test("the crew block draws the model's tree — every member, its tasks and its plan", () => {
  const rows = block(RUN);

  // The rule carries the run-level facts, so it costs no extra line.
  assert.match(rows[0] as string, /── crew · 2 agents · 1 running/);

  // The lead: its live phrase, in the runtime's own words, and its two clocks.
  assert.match(rows[1] as string, /● lead agent {2}authoring workload\.yaml/);
  assert.match(rows[1] as string, /45\.0s · ♥ 44\.0s$/);

  // Its plan entry, which reaches the feed as a SILENT kind and would otherwise
  // be invisible in local mode.
  assert.match(rows[2] as string, /^ {4}☐ Implement issue #3$/);

  // The spawned agent, tagged the way the streamed lines tag it, reporting the
  // runtime's OWN totals through the shared formatter.
  assert.match(rows[3] as string, /✓ #1 Build onboarding-webapp React SPA {2}build clean/);
  assert.match(rows[3] as string, /completed · 41\.0s · 162 tools$/);

  // Its backgrounded command sits under it — the failure this prevents is a
  // `dev:mock` still holding a port and nobody being able to say whose it was.
  assert.match(rows[4] as string, /^ {6}⟳ npm run dev:mock/);
  assert.match(rows[4] as string, /running 42\.0s$/);
  assert.equal(rows.length, 5);
});

test("a stalled member says what it is stalled ON, and a failure is not a stall", () => {
  const stalled = block(
    [
      { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0, ts: at(0) },
      { kind: "tool_use", agentId: "lead", tool: "Bash", toolUseId: "t1", summary: "npm test", ts: at(1_000) },
    ],
    100,
    30,
    T0 + 90_000,
  );
  // Past STALL_MS with the call still unanswered, and an amber row must always
  // name what it is amber about — here the call itself is the only witness.
  assert.match(stalled[1] as string, /◔ lead agent {2}stalled · waiting on Bash for 1m29s/);

  const failed = block([
    { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0, ts: at(0) },
    { kind: "agent_settled", agentId: "lead", status: "failed", report: "ran out of context", ts: at(9_000) },
  ]);
  assert.match(failed[1] as string, /✗ lead agent {2}ran out of context/);
  assert.match(failed[1] as string, /failed$/);
});

test("every drawn row fits the terminal, and a block too tall says what it hid", () => {
  for (const row of block(RUN, 46)) {
    assert.ok(row.length <= 45, `"${row}" (${String(row.length)}) must not reach the last column`);
  }
  // A row that reaches the last column wraps, and a wrapped row makes the block
  // taller than the cursor arithmetic believes — which is how a redraw eats the
  // transcript above it.
  const capped = block(RUN, 100, 3);
  assert.equal(capped.length, 3);
  assert.match(capped[2] as string, /… 3 more/);
});

test("a multi-line shell label stays one row, and keeps its place in the tree", () => {
  // Measured on a real run: a heredoc or an `&&` chain reaches the feed with its
  // newlines intact, and one BlockRow then drew as three physical lines — the
  // exact shape the redraw arithmetic cannot survive.
  const rows = block([
    { kind: "agent_started", agentId: "lead", label: "lead agent", depth: 0, ts: at(0) },
    {
      kind: "task_started",
      agentId: "lead",
      taskId: "t9",
      summary: "cd /tmp/baltest && cat > narrow.bal << 'EOF'\nimport ballerina/http;\nEOF",
      ts: at(1_000),
    },
  ]);
  for (const row of rows) {
    assert.ok(!row.includes("\n"), `"${row}" must be one physical line`);
  }
  const task = rows.find((r) => r.includes("narrow.bal"));
  assert.ok(task, "the backgrounded command must still be drawn");
  assert.match(task, /^ {4}⟳ cd \/tmp\/baltest/, "its indent under the lead is the tree");
  assert.match(task, /import ballerina\/http;/, "the flattened tail is still readable");
});

test("a plan entry handed to a spawned agent is drawn under that agent", () => {
  const rows = block([
    ...RUN,
    {
      kind: "work_item",
      agentId: "lead",
      source: "plan",
      itemId: "task_9",
      title: "Walk the checklist",
      itemStatus: "in_progress",
      ownerAgentId: "ag_web",
      ts: at(4_000),
    },
    // An owner no `agent_started` ever declared still belongs to somebody: it
    // falls to the lead rather than vanishing.
    {
      kind: "work_item",
      agentId: "lead",
      source: "plan",
      itemId: "task_10",
      title: "Orphaned entry",
      itemStatus: "pending",
      ownerAgentId: "ag_ghost",
      ts: at(4_100),
    },
  ]);
  assert.ok(
    rows.some((r) => /^ {4}☐ Orphaned entry$/.test(r)),
    "an unknown owner falls to the lead",
  );
  assert.ok(
    rows.some((r) => /^ {6}▸ Walk the checklist$/.test(r)),
    "an owned entry is indented under its owner",
  );
});

test("the feed and the block agree on which agent is #1", () => {
  // The registry is shared, so a reader crossing from a tagged step line to the
  // crew row lands on the same agent. Two registries minting in the same order
  // by coincidence is exactly what this exists to stop relying on.
  const tags = createAgentTags();
  const render = createTimelineRenderer(tags);
  const opened = render({
    agentId: "ag_web",
    kind: "agent_started",
    label: "Build onboarding-webapp React SPA",
    depth: 1,
  });
  assert.match(opened[0] as string, /\[#1\]/);

  const rows = renderCrewBlock(buildCrew(RUN, NOW), NOW, { columns: 100, maxRows: 30, tag: tags }).map(
    (r) => r.text,
  );
  assert.ok(
    rows.some((r) => r.includes("#1 Build onboarding-webapp React SPA")),
    "the same registry names it the same way on both surfaces",
  );

  // And the plan rows exist only because the flat renderer prints nothing for
  // them — a work_item is state a surface repaints, never a row.
  assert.deepEqual(
    render({ kind: "work_item", agentId: "lead", source: "plan", itemId: "task_7", itemStatus: "in_progress" }),
    [],
  );
});

test("no TTY: no block, no escapes — a piped run reads exactly as it always did", () => {
  const { writes, out } = fakeTerminal();
  const pane = openCrewPane({ out, isTTY: false, tag: createAgentTags(), now: () => NOW });
  pane.line("  $ Read /workspace/project/issues/5.md");
  pane.update(RUN);
  pane.line("  [#1] $ npx vite build");
  pane.update(RUN);
  pane.close();

  assert.equal(
    writes.join(""),
    "  $ Read /workspace/project/issues/5.md\n  [#1] $ npx vite build\n",
    "the tagged lines and nothing else",
  );
});

test("on a TTY the block is pinned under the stream and redrawn in place", () => {
  const { writes, out } = fakeTerminal();
  let clock = NOW;
  const pane = openCrewPane({ out, isTTY: true, tag: createAgentTags(), now: () => clock });

  pane.line("  $ Read /workspace/project/issues/5.md");
  pane.update(RUN);
  const painted = replay(writes);
  assert.equal(painted[0], "  $ Read /workspace/project/issues/5.md");
  assert.match(painted[1] as string, /── crew ·/);

  // A step line lands ABOVE the block, and the block is still the last thing on
  // screen afterwards.
  clock += 1_000;
  pane.line("  [#1] $ npx vite build");
  pane.update(RUN);
  const after = replay(writes);
  assert.deepEqual(after.slice(0, 2), [
    "  $ Read /workspace/project/issues/5.md",
    "  [#1] $ npx vite build",
  ]);
  assert.match(after[2] as string, /── crew ·/);
  assert.equal(after.length, 2 + block(RUN).length);

  // Closing takes the block down and leaves the transcript exactly as printed.
  pane.close();
  assert.deepEqual(replay(writes), [
    "  $ Read /workspace/project/issues/5.md",
    "  [#1] $ npx vite build",
  ]);
});

test("the block's content is rebuilt at most once a second", () => {
  const { writes, out } = fakeTerminal();
  let clock = NOW;
  const pane = openCrewPane({ out, isTTY: true, tag: createAgentTags(), now: () => clock });

  pane.update(RUN);
  const drawn = writes.length;
  // buildCrew walks the whole event array; a 40-minute run's array is long
  // enough that rebuilding per event would be quadratic.
  pane.update(RUN);
  pane.update(RUN);
  assert.equal(writes.length, drawn, "same second, no rebuild");

  clock += 1_000;
  pane.update(RUN);
  assert.ok(writes.length > drawn, "a second later it redraws");
  pane.close();
});

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

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { ValidationProgressState } from "./validation_progress.js";
import type { LadderState } from "./validation_status_line.js";
import {
  LADDER,
  LADDER_LINES,
  Ladder,
  MAX_POSTS,
  OBSERVED_COMMENT_MARKER,
  POST_TIMEOUT_MS,
  createValidationStatusLine,
  ghCommentPoster,
  ladderStateFor,
  repoSlug,
} from "./validation_status_line.js";

const write = (file: string, content: string) => ({
  toolName: "Write",
  input: { file_path: file, content },
});
const bash = (command: string) => ({ toolName: "Bash", input: { command } });

function stateFor(call: { toolName: string; input: unknown }, progress = new ValidationProgressState()) {
  return ladderStateFor(call.toolName, call.input, progress);
}

// The brand the BFF classifies by, spelled out rather than read from the
// constant — reading it back would make this tautological.
//
// One literal in two languages: this module stamps it, and the BFF's
// sourcecontrol.ObservedCommentMarker decides on it which comments are the
// platform's observation and which are somebody's own words. Change one
// spelling and every line a run posts reclassifies as the agent's, with both
// suites green. The Go side pins the same literal.
test("the observed brand matches the one the BFF classifies by", () => {
  assert.equal(OBSERVED_COMMENT_MARKER, "<!-- aep:observed -->");
});

// --- which calls announce which rung ---------------------------------------

// The two ends are matched here because no per-criterion status describes them:
// the harness is scaffolding, and the report is a verdict over every criterion
// at once.
test("ladderStateFor: the harness install and the report generator are the two ends", () => {
  assert.equal(stateFor(bash("npm install --prefix tests/e2e")), "harness");
  assert.equal(stateFor(bash("npm ci --prefix tests/e2e")), "harness");
  assert.equal(
    stateFor(bash('node "$AEP_SKILLS_DIR/aep-validation/scripts/generate-report.mjs" --issue 7')),
    "reporting",
  );
});

// p44, 01:18:13: "Generating the validation report from the results on disk."
// posted as the run's FIRST line, before the app had been opened. Step 5
// scaffolds the package by copying this very file into the repo, and a pattern
// matching the bare filename read that copy as a verdict being generated.
test("ladderStateFor: scaffolding the report generator is not generating a report", () => {
  assert.equal(
    stateFor(
      bash(
        'cp "$AEP_SKILLS_DIR/aep-validation/scripts/generate-report.mjs" tests/e2e/scripts/generate-report.mjs',
      ),
    ),
    undefined,
    "the copy announces nothing — harness already fired on the package write",
  );
  assert.equal(stateFor(bash("ls tests/e2e/scripts/generate-report.mjs")), undefined);
  assert.equal(stateFor(bash("cat scripts/generate-report.mjs | head -20")), undefined);
});

// p44 posted no harness line at all. The skill writes
// `npm install --prefix tests/e2e`, but the order of a flag and a verb is the
// agent's to choose and the pattern demanded one of them.
test("ladderStateFor: an install is an install whatever order it is written in", () => {
  for (const command of [
    "npm install --prefix tests/e2e",
    "npm --prefix tests/e2e install",
    "npm --prefix tests/e2e ci",
    "cd tests/e2e && npm install",
    "pnpm --prefix tests/e2e install",
  ]) {
    assert.equal(stateFor(bash(command)), "harness", command);
  }
});

// The surer signal, and the reason the shell form no longer has to be guessed:
// the skill NAMES these files, and re-copies the config on every run — so this
// fires on a re-validation too, where the install may legitimately not happen.
test("ladderStateFor: writing a scaffold file is the harness", () => {
  for (const file of [
    "tests/e2e/package.json",
    "tests/e2e/playwright.config.ts",
    "tests/e2e/targets.json",
    "tests/e2e/lib/targets.ts",
    "/home/aep/aep-workspace/tests/e2e/.gitignore",
  ]) {
    assert.equal(stateFor(write(file, "{}")), "harness", file);
  }
});

// The exclusion that keeps the rung honest: a spec lives under the same package
// and means the rung ABOVE. Without it every spec write would report harness and
// the ladder would ratchet backwards for the whole authoring phase.
test("ladderStateFor: a spec under tests/e2e is never the harness", () => {
  assert.equal(
    stateFor(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\n")),
    "exploring",
  );
  assert.equal(
    stateFor(
      write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\ntest('AC-001-a: x', async () => {});"),
    ),
    "authoring",
  );
});

// The middle three ARE ProgressItemStatus values, read through the same
// derivation the console's rows use. Pinned here so a change to that derivation
// cannot silently take the issue's line with it.
test("ladderStateFor: the middle rungs come from the per-criterion derivation", () => {
  assert.equal(
    stateFor(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\n")),
    "exploring",
    "a header-only spec is the stub written BEFORE exploring",
  );
  assert.equal(
    stateFor(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\ntest('AC-001-a: x', async () => {});")),
    "authoring",
  );
  assert.equal(stateFor(bash("npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts")), "running");
});

// The rungs are about the RUN. A criterion status with no rung of its own must
// not fall through to one that means something else — `planned` in particular,
// which the test plan raises for every criterion at once.
test("ladderStateFor: a call that announces no rung announces nothing", () => {
  assert.equal(stateFor(write("tests/validation/test-plan.md", "## AC-001-a — a box\n")), undefined);
  assert.equal(stateFor(bash("cat tests/e2e/specs/AC-001-a.spec.ts")), undefined);
  assert.equal(stateFor(bash("git push --force-with-lease -u origin aep/m1-validation")), undefined);
  assert.equal(stateFor(write("src/app.ts", "export const x = 1;")), undefined);
});

// `npm install` in some other package is a different run doing different work.
test("ladderStateFor: only the e2e package's install is the harness", () => {
  assert.equal(stateFor(bash("npm install --prefix apps/web")), undefined);
});

// --- the ratchet ------------------------------------------------------------

// A criterion authored twelve times is one line. Twelve identical comments would
// say nothing the first did not, and each one costs a slot in the window the
// status line is read inside.
test("Ladder: the state a run is already in is not news", () => {
  const ladder = new Ladder();
  assert.equal(ladder.admit("exploring"), true);
  assert.equal(ladder.admit("exploring"), false);
  assert.equal(ladder.admit("exploring"), false);
});

// The shape of a real middle: twelve criteria, each walking the same three
// rungs. This is the case that decides whether the ladder is usable at all —
// three lines per criterion would exhaust the cap around the fourth and leave
// the rest of a two-hour run silent, which is the defect the ladder exists to
// fix, returning at its worst possible moment.
test("Ladder: working criteria one at a time posts each rung once, not once per criterion", () => {
  const ladder = new Ladder();
  const posted: LadderState[] = [];
  const say = (s: LadderState) => {
    if (ladder.admit(s)) posted.push(s);
  };

  say("harness");
  for (let criterion = 0; criterion < 12; criterion += 1) {
    say("exploring");
    say("authoring");
    say("running");
  }
  say("reporting");

  assert.deepEqual(posted, ["harness", "exploring", "authoring", "running", "reporting"]);
});

// Step 8 heals by editing a spec and re-running it, over and over. At the run
// altitude nothing has changed — it is still running tests against the deployed
// system — and the console already says which criterion is healing, per row.
test("Ladder: healing does not walk the line backwards", () => {
  const ladder = new Ladder();
  for (const s of ["harness", "exploring", "authoring", "running"] as const) ladder.admit(s);

  for (let heal = 0; heal < 5; heal += 1) {
    assert.equal(ladder.admit("authoring"), false, "a heal is not a regression");
    assert.equal(ladder.admit("running"), false, "…and neither is re-running it");
  }
});

// Nothing behind the mark speaks. The exit-2 loop, the one thing that would
// deserve to, is the repair mode instead — which is what lets this stay a rule
// with no exception to reason about.
test("Ladder: a rung behind the mark is never news", () => {
  const ladder = new Ladder();
  for (const state of LADDER) assert.equal(ladder.admit(state), true, state);

  for (const state of ["harness", "exploring", "authoring", "running"] as const) {
    assert.equal(ladder.admit(state), false, `${state} spoke from behind the mark`);
  }
});

// The regression the exception caused before it was removed. After a report
// SUCCEEDS the mark sits on the last rung, and the run still has step 10 to do —
// a push, a pull request, whatever it touches on the way. A rule that let a fall
// speak announced "Setting up the test harness…" over a run that had finished.
test("Ladder: a finished run does not go back to setting up", () => {
  const ladder = new Ladder();
  for (const state of LADDER) ladder.admit(state);

  assert.equal(ladder.admit("harness"), false, "a late scaffold write restarted the run's story");
});

// --- the watcher ---------------------------------------------------------------

function hookInput(call: { toolName: string; input: unknown }) {
  return {
    tool_name: call.toolName,
    tool_input: call.input,
    tool_use_id: "tu_1",
  };
}

test("the watcher posts one branded line per rung", async () => {
  const posted: string[] = [];
  const { observe } = createValidationStatusLine(
    new ValidationProgressState(),
    async (body) => {
      posted.push(body);
    },
    () => assert.fail("a successful post must not warn"),
  );

  await fire(observe, hookInput(bash("npm ci --prefix tests/e2e")));
  await fire(observe, hookInput(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\n")));

  assert.deepEqual(posted, [
    `${OBSERVED_COMMENT_MARKER}\n${LADDER_LINES.harness}`,
    `${OBSERVED_COMMENT_MARKER}\n${LADDER_LINES.exploring}`,
  ]);
});

// The brand is what keeps these OUT of the platform's notes-to-the-agent class,
// which the BFF drops on read. Unbranded they would be indistinguishable from
// the agent's own words; branded as machine they would vanish entirely.
test("every line carries the observed brand, first", async () => {
  const posted: string[] = [];
  const { observe } = createValidationStatusLine(new ValidationProgressState(), async (b) => void posted.push(b), () => {});
  await fire(observe, hookInput(bash("npm ci --prefix tests/e2e")));
  assert.ok(posted[0]?.startsWith(OBSERVED_COMMENT_MARKER), posted[0]);
});

// The status line is the newest comment's FIRST non-empty line, so a rung's
// sentence is the whole claim — there is no second line a reader will see.
test("every rung's line is a single sentence on one line", () => {
  for (const state of LADDER) {
    const line = LADDER_LINES[state];
    assert.ok(!line.includes("\n"), `${state} spans lines`);
    assert.ok(line.length > 0 && line.length < 120, `${state} is not one readable line: ${line}`);
  }
});

// A validation cycle is two hours of work and the status line is commentary on
// it. Losing the commentary must never lose a criterion, so the failure is
// reported on the run's own feed and swallowed.
test("a failed post warns and never throws", async () => {
  const warnings: string[] = [];
  const { observe } = createValidationStatusLine(
    new ValidationProgressState(),
    async () => {
      throw new Error("gh: 403 rate limited");
    },
    (reason) => warnings.push(reason),
  );

  const decision = await fire(observe, hookInput(bash("npm ci --prefix tests/e2e")));

  assert.equal(decision, undefined, "the watcher watches; it never decides");
  assert.equal(warnings.length, 1);
  assert.match(warnings[0] ?? "", /harness/);
  assert.match(warnings[0] ?? "", /rate limited/);
});

// It watches the calls a run has to make. A watcher that could refuse one would
// be a far worse bargain than no status line.
test("the watcher never blocks a tool call", async () => {
  const { observe } = createValidationStatusLine(new ValidationProgressState(), async () => {}, () => {});
  for (const call of [bash("npm ci --prefix tests/e2e"), bash("ls"), write("x.ts", "y")]) {
    const decision = await fire(observe, hookInput(call));
    assert.equal(decision, undefined);
  }
});

// --- the repair mode --------------------------------------------------------

function reportCall(id = "tu_report") {
  return {
    tool_name: "Bash",
    tool_input: { command: 'node "$AEP_SKILLS_DIR/aep-validation/scripts/generate-report.mjs" --issue 7' },
    tool_use_id: id,
  };
}

async function fire(
  observe: ReturnType<typeof createValidationStatusLine>["observe"],
  input: { tool_name: string; tool_input: unknown; tool_use_id: string },
) {
  return observe(input.tool_name, input.tool_input, input.tool_use_id);
}

// The whole reason this mode exists. Step 9's exit 2 is "the ordinary loop, not
// a defect" — the generator names specs with no result, the run covers them and
// regenerates, and it may lap several times. Narrating each lap as rungs put
// three lines on the issue per lap and reached MAX_POSTS at the third.
test("a lapping run says it is repairing ONCE, however many laps it takes", async () => {
  const posted: string[] = [];
  const line = createValidationStatusLine(new ValidationProgressState(), async (b) => void posted.push(b), () => {});

  await fire(line.observe, hookInput(bash("npm ci --prefix tests/e2e")));
  await fire(line.observe, hookInput(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\n")));
  await fire(line.observe, hookInput(write("tests/e2e/specs/AC-001-a.spec.ts", "// spec: AC-001-a\ntest('AC-001-a: x', () => {});")));
  await fire(line.observe, hookInput(bash("npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts")));

  // Four laps: generate, refused, cover the gap, generate again…
  for (let lap = 0; lap < 4; lap += 1) {
    await fire(line.observe, reportCall(`tu_${lap}`));
    line.settle(`tu_${lap}`, false);
    await fire(line.observe, hookInput(bash("npm test --prefix tests/e2e -- specs/AC-001-b.spec.ts")));
    await fire(line.observe, hookInput(write("tests/e2e/specs/AC-001-b.spec.ts", "test('AC-001-b: y', () => {});")));
  }
  // …and the fifth one lands.
  await fire(line.observe, reportCall("tu_ok"));
  line.settle("tu_ok", true);

  const lines = posted.map((b) => b.split("\n")[1]);
  assert.deepEqual(lines, [
    LADDER_LINES.harness,
    LADDER_LINES.exploring,
    LADDER_LINES.authoring,
    LADDER_LINES.running,
    LADDER_LINES.reporting,
    LADDER_LINES.repairing,
  ]);
  assert.ok(posted.length < MAX_POSTS, "four laps must not approach the cap");
});

// Only the generator's own outcome enters the mode. A failing `npm test` is
// ordinary — a criterion failed, the rows say so, and step 8 heals it.
test("a failing spec run is not a repair", async () => {
  const posted: string[] = [];
  const line = createValidationStatusLine(new ValidationProgressState(), async (b) => void posted.push(b), () => {});

  await fire(line.observe, {
    tool_name: "Bash",
    tool_input: { command: "npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts" },
    tool_use_id: "tu_test",
  });
  line.settle("tu_test", false);

  assert.deepEqual(posted.map((b) => b.split("\n")[1]), [LADDER_LINES.running]);
});

// Leaving the mode is silent: step 10's push, pull request and the agent's own
// closing summary follow, and that summary says more than a rung could.
test("the report landing says nothing — the closing summary is next", async () => {
  const posted: string[] = [];
  const line = createValidationStatusLine(new ValidationProgressState(), async (b) => void posted.push(b), () => {});

  await fire(line.observe, reportCall("tu_1"));
  line.settle("tu_1", false);
  const afterRepair = posted.length;
  await fire(line.observe, reportCall("tu_2"));
  line.settle("tu_2", true);

  assert.equal(posted.length, afterRepair, "landing the report posted a line of its own");
});

// The cap going quiet looks exactly like a run that finished, which is the
// failure shape this whole mechanism exists to remove — so it says so once, on
// the run's own feed, where it is diagnosable.
test("reaching the cap warns once and then stops posting", async () => {
  const posted: string[] = [];
  const warnings: string[] = [];
  const line = createValidationStatusLine(
    new ValidationProgressState(),
    async (b) => void posted.push(b),
    (reason) => warnings.push(reason),
  );

  // Rungs are one-way, so they cannot climb past five. What still can is the
  // repair mode: a report that fails, succeeds, then fails again re-enters it,
  // and nothing bounds how many times a run may do that. This is the shape the
  // backstop is still here for.
  await fire(line.observe, hookInput(bash("npm ci --prefix tests/e2e")));
  for (let i = 0; i < MAX_POSTS * 2; i += 1) {
    await fire(line.observe, reportCall(`tu_${i}`));
    line.settle(`tu_${i}`, false);
    await fire(line.observe, reportCall(`tu_ok_${i}`));
    line.settle(`tu_ok_${i}`, true);
  }

  assert.equal(posted.length, MAX_POSTS, "the cap did not hold");
  assert.equal(warnings.length, 1, "the cap must announce itself exactly once");
  assert.match(warnings[0] ?? "", /capped at 12/);
  assert.match(warnings[0] ?? "", /last line will stand/);
});

// --- reaching gh the way the agent does -------------------------------------

// The environment is not decoration. In the mode where no token is mounted,
// `.aep/gh` refreshes $GH_CONFIG_DIR/hosts.yml before exec'ing the real binary,
// and GH_CONFIG_DIR lives ONLY in the agent's child environment — never in this
// process's own. A poster that inherited process.env posted as nobody, and said
// so only as a warning.
test("the poster runs the workspace's gh under the agent's own environment", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-poster-"));
  const seen = path.join(dir, "seen.txt");
  const fakeGh = path.join(dir, "gh");
  fs.writeFileSync(fakeGh, `#!/usr/bin/env bash\nprintf '%s\\n' "$GH_CONFIG_DIR" "$@" > ${JSON.stringify(seen)}\n`);
  fs.chmodSync(fakeGh, 0o755);

  const post = ghCommentPoster(
    { path: fakeGh, env: { ...process.env, GH_CONFIG_DIR: "/ws/.gh-config" } },
    "https://github.com/acme/widgets.git",
    7,
  );
  await post("hello");

  const lines = fs.readFileSync(seen, "utf8").trim().split("\n");
  assert.equal(lines[0], "/ws/.gh-config", "GH_CONFIG_DIR never reached gh");
  assert.deepEqual(lines.slice(1), ["issue", "comment", "7", "--repo", "acme/widgets", "--body", "hello"]);
  fs.rmSync(dir, { recursive: true, force: true });
});

// Awaited by the runtime before the call it describes, so a hung `gh` sits between the agent and
// its next tool call. Unbounded, one stalled connection would hold a two-hour
// validation there — trading the work for the commentary on it.
test("a hanging gh is abandoned rather than holding the run", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-hang-"));
  const fakeGh = path.join(dir, "gh");
  fs.writeFileSync(fakeGh, "#!/usr/bin/env bash\nsleep 30\n");
  fs.chmodSync(fakeGh, 0o755);

  const warnings: string[] = [];
  const line = createValidationStatusLine(
    new ValidationProgressState(),
    ghCommentPoster({ path: fakeGh, env: process.env }, "https://github.com/acme/widgets.git", 7),
    (reason) => warnings.push(reason),
  );

  const started = Date.now();
  const decision = await Promise.race([
    fire(line.observe, hookInput(bash("npm ci --prefix tests/e2e"))),
    new Promise((r) => setTimeout(() => r("STILL BLOCKED"), POST_TIMEOUT_MS + 5_000)),
  ]);
  assert.notEqual(decision, "STILL BLOCKED", "the watcher never came back");
  assert.ok(Date.now() - started < POST_TIMEOUT_MS + 5_000);
  assert.equal(warnings.length, 1, "an abandoned post must say so");
  fs.rmSync(dir, { recursive: true, force: true });
});

// --- addressing the issue ---------------------------------------------------

// Naming the repository rather than letting `gh` infer it from a remote: the
// clone URL is the one form every dispatch carries.
test("repoSlug: owner/repo out of the clone URLs a dispatch can carry", () => {
  for (const [url, want] of [
    ["https://github.com/acme/widgets.git", "acme/widgets"],
    ["https://github.com/acme/widgets", "acme/widgets"],
    ["git@github.com:acme/widgets.git", "acme/widgets"],
    ["https://ghe.example.com/acme/widgets.git", "acme/widgets"],
  ] as const) {
    assert.equal(repoSlug(url), want, url);
  }
});

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

// What one scenario entry has to satisfy, shared by the two things that ask it:
// `capture-scenario.mjs`, the moment a scenario finishes and while its page is
// still open, and `check-report.mjs`, again over the assembled report.
//
// ONE implementation, deliberately. The capture-time check earns its place only
// by predicting the end-of-run one: a report that would fail at the end fails
// EARLY, when re-driving the scenario is still free. Two copies of these rules
// would drift, and the early check would stop predicting anything.
//
// NO DEPENDENCIES beyond node builtins — the same constraint `check-report.mjs`
// documents. This ships inside the skill and runs from a project clone where none
// of our node_modules are installed, and NODE_PATH does not apply to ESM.

import { createHash } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const SCHEMA_VERSION = 3;

export const OUTCOMES = new Set(["passed", "failed", "blocked", "unjudgeable"]);

/**
 * Did the outcome hold when the scenario was driven a second time?
 *
 * A vocabulary rather than a boolean, because "I did not drive it again" is a
 * legitimate answer — a `When` that destroys something, a `Given` too expensive to
 * rebuild — and a boolean would make the agent pick a side it cannot stand behind.
 * `no` is the one that redirects a repair: a failure that passes on a second pass
 * is a race, not a logic defect, and nothing else in the report says so.
 */
export const REPRODUCED = new Set(["yes", "no", "unattempted"]);

export const FEATURE_DIR = "specs/validation/acceptance";
export const REPORT_PATH = "tests/acceptance/report.json";
export const SNAPSHOT_DIR = "tests/acceptance/snapshots";

/**
 * Where a run's per-scenario captures accumulate before they are assembled.
 *
 * OUTSIDE the project, and never committed. They are working state, not an
 * artifact: the one thing that has to survive the run is `report.json`, and a
 * scratch directory in the repo would be committed by a `git add -A` and read by
 * nobody. `os.tmpdir()` is writable from the pod without the workspace guard
 * having an opinion, and the pod is the sandbox.
 *
 * The override exists so the pipeline can be exercised over a real report on a
 * developer's machine without two runs sharing a directory.
 */
export function capturesDir() {
  return process.env.AEP_CAPTURE_DIR || join(tmpdir(), "aep-acceptance-captures");
}

/**
 * Commands that PRINT a value rather than answering with their exit code. For
 * these, exit 0 means "the command ran" and the agent did the judging, so the
 * value it read has to be in the report.
 */
const VALUE_COMMAND = /\bget\s+(count|value|url|text)\b/;

/** The identity all three readers agree on: feature, rule and scenario. */
export function scenarioKey(feature, rule, scenario) {
  return `${feature ?? ""} ▸ ${rule ?? ""} ▸ ${scenario ?? ""}`;
}

/**
 * The file a scenario's capture and its snapshot are written under.
 *
 * The entry's own `id` when it carries one — the mechanical scenario id, which is
 * stable across a reworded title and short enough to read. Until that exists, a
 * slug of the title plus a hash of the full key: the slug so a human opening the
 * scratch directory can see what is there, the hash so two scenarios that
 * normalise to the same slug still land in different files. Same shape as
 * `dedupeLabelFor` on the Go side, and both simplify together once ids land.
 *
 * Deterministic on purpose: a re-driven scenario overwrites its own earlier
 * capture rather than leaving two.
 */
export function scenarioFileName(s) {
  if (typeof s.id === "string" && /^[A-Za-z0-9._-]+$/.test(s.id)) return s.id;
  const key = scenarioKey(s.feature, s.rule, s.scenario);
  const slug = String(s.scenario ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 39);
  const hash = createHash("sha256").update(key).digest("hex").slice(0, 10);
  return slug ? `${slug}-${hash}` : hash;
}

/**
 * Each step's keyword with `And` / `But` / `*` resolved to the one it inherits.
 *
 * Gherkin says a continuation IS the keyword above it, and a scenario's deciding
 * assertion is routinely the continuation — `Then the row appears` / `And it shows
 * today's date`. Matching the raw keyword makes that `And` invisible to every rule
 * below: it is not counted as an assertion, it cannot back a `passed`, it cannot
 * name a `failed`, and it is never asked for `observed`. The Go reader resolves
 * these the same way (`report.go`, `effectiveKeywords`); the two must agree or a
 * report passes here and is read differently there.
 */
export function effectiveKeywords(steps) {
  let current = "";
  return steps.map((st) => {
    const k = (st.keyword ?? "").trim();
    if (k !== "And" && k !== "But" && k !== "*" && k !== "") current = k;
    return current;
  });
}

/** Why this step's exit code is not the verdict, or "" when it is. */
export function needsObserved(step) {
  if (typeof step.exit === "number" && step.exit !== 0) return "the command exited nonzero";
  if (VALUE_COMMAND.test(step.command ?? "")) return "the command prints a value rather than answering with its exit code";
  return "";
}

/**
 * Every scenario one feature file declares, as {rule, scenario, line}.
 *
 * A line scanner, not a parser — see the dependency note above. It is enough
 * because this needs identity and location, never the step AST, and because the
 * shape it reads is the one `acceptance-criteria` authors: `Feature` → `Rule` →
 * `Scenario`. Three things it must not be fooled by, and each is handled: a `#`
 * comment, a docstring whose body happens to start a line with `Scenario:`, and
 * `Example:` — which is the Gherkin grammar's synonym for `Scenario:`, not a
 * different construct.
 *
 * `Scenario Outline:` is deliberately counted as ONE scenario. The real parser did
 * the same: nothing here expands an Examples table, and a report that answers the
 * outline once is answering what the file declares once.
 */
export function scanFeature(text) {
  let feature = "";
  let rule = "";
  let ruleStories = [];
  let featureStories = [];
  let tags = [];
  let fence = ""; // the docstring delimiter we are inside, or "" outside one
  const scenarios = [];

  text.split("\n").forEach((raw, i) => {
    const line = raw.trim();

    // Docstrings open and close with the same delimiter. Everything between is
    // data the scenario quotes, not Gherkin.
    if (fence) {
      if (line.startsWith(fence)) fence = "";
      return;
    }
    if (line.startsWith('"""') || line.startsWith("```")) {
      fence = line.startsWith('"""') ? '"""' : "```";
      return;
    }
    if (line.startsWith("#")) return;

    const take = (kw) => line.slice(kw.length).trim();
    const stories = () => [...featureStories, ...ruleStories];
    if (line.startsWith("@")) {
      tags.push(...[...line.matchAll(/@story-(\S+)/g)].map((m) => m[1]));
      return;
    }
    if (line.startsWith("Feature:")) {
      feature = take("Feature:");
      featureStories = tags;
    } else if (line.startsWith("Rule:")) {
      rule = take("Rule:");
      ruleStories = tags;
    } else if (line.startsWith("Scenario Outline:")) scenarios.push({ rule, stories: stories(), name: take("Scenario Outline:"), line: i + 1 });
    else if (line.startsWith("Scenario:")) scenarios.push({ rule, stories: stories(), name: take("Scenario:"), line: i + 1 });
    else if (line.startsWith("Example:")) scenarios.push({ rule, stories: stories(), name: take("Example:"), line: i + 1 });
    if (line !== "") tags = [];
  });

  return { feature, scenarios };
}

/**
 * Every scenario the feature files declare, keyed by {@link scenarioKey}.
 *
 * `inScope` is the version's built scope — a scenario outside it is not this
 * version's to answer for, and both the checker and the assembler have to agree
 * on that or one of them refuses a report the other was happy with.
 */
export function readFeatureScenarios(featureDir, inScope = () => true) {
  const expected = new Map();
  for (const file of readdirSync(featureDir).filter((f) => f.endsWith(".feature")).sort()) {
    const { feature, scenarios } = scanFeature(readFileSync(join(featureDir, file), "utf8"));
    if (!feature) continue;
    for (const s of scenarios) {
      if (inScope(feature, s)) expected.set(scenarioKey(feature, s.rule, s.name), `${file}:${s.line}`);
    }
  }
  return expected;
}

/**
 * The rules one entry must satisfy. Returns bare messages; the caller adds the
 * context it has (a scenario index in a report, a file name in a capture).
 *
 * `stage` says which side of assembly we are on. A capture carries the page tree
 * inline as `snapshot`, because a capture has to be self-contained to be checkable
 * on its own; the assembled report carries `snapshotFile`, the path the assembler
 * wrote it to. Same requirement, two spellings, and asking for the wrong one is
 * how an early check stops predicting the late one.
 */
export function checkScenario(s, expected, { stage = "report" } = {}) {
  const errors = [];

  if (!OUTCOMES.has(s.outcome)) {
    errors.push(`outcome ${JSON.stringify(s.outcome)} is not one of ${[...OUTCOMES].join(", ")}`);
    return errors; // every rule below reads the outcome; none of them can run
  }

  const steps = Array.isArray(s.steps) ? s.steps : [];
  const keywords = effectiveKeywords(steps);
  const thens = steps.filter((_, i) => keywords[i] === "Then");
  const settled = thens.filter((st) => st.command && typeof st.exit === "number");

  // Where the scenario is written, so a reader and a repair issue can reach it.
  const where = expected?.get(scenarioKey(s.feature, s.rule, s.scenario));
  if (where) {
    const [file, line] = where.split(":");
    if (!s.featureFile) errors.push("no featureFile — a repair issue cannot point at the scenario");
    else if (!s.featureFile.endsWith(file)) errors.push(`featureFile is ${s.featureFile}, but the scenario is in ${file}`);
    if (s.line !== undefined && String(s.line) !== line)
      errors.push(`line ${s.line}, but the scenario is at line ${line}`);
  }

  // A pass has to be backed by something that could have said no.
  if (s.outcome === "passed") {
    if (thens.length === 0) errors.push("passed with no Then step recorded");
    else if (settled.length !== thens.length)
      errors.push(`passed but ${thens.length - settled.length} of ${thens.length} Then steps carry no command+exit`);
    else if (settled.some((st) => st.exit !== 0)) errors.push("passed but a Then step exited nonzero");
  }

  // A failure has to name what said no. An exit code usually does; a command that
  // prints a value cannot, so there the observed value is the evidence.
  if (s.outcome === "failed" && !settled.some((st) => st.exit !== 0) && !thens.some((st) => st.observed)) {
    errors.push("failed but no Then records a nonzero exit or an observed value — what failed?");
  }

  // A block has to say what stopped it, or an app that correctly refuses reads the
  // same as one that is broken.
  if (s.outcome === "blocked" && !steps.some((st) => st.observed)) {
    errors.push("blocked but no step records what was observed — why could it not run?");
  }

  errors.push(...checkOutcomeFields(s, stage));

  // Wherever the exit code is not the verdict, record what the agent read. EVERY
  // step, not only the assertions. A `When` settled by a value-returning command is
  // where the run records what the system did — the 401 rule reads it, and
  // `deciding()` falls back to it when no `Then` observed anything — so a `When`
  // that exits nonzero or prints a value and says nothing is the same hole as a
  // silent `Then`.
  steps.forEach((st, i) => {
    if (!st.command) return; // a step with no command is covered by the `blocked` rule
    const why = needsObserved(st);
    if (why && !st.observed) {
      errors.push(`a ${keywords[i] || st.keyword || "step"} carries no \`observed\` and ${why}`);
    }
  });

  return errors;
}

/**
 * What each outcome owes beyond its steps.
 *
 * A PASS owes nothing and may carry no evidence at all. That is a rule, not an
 * omission: nothing reads evidence on a pass — the Go reader only reaches it
 * through `FailedScenarios`, and the console does not parse it — and a real run
 * once captured eleven such blocks, every one of them the same stale page.
 *
 * Every other outcome owes `network` and `console`, because they are only
 * obtainable while the page is open and they are what separates a request that
 * left and came back from one that never fired. An EMPTY array is an answer; an
 * absent one is a hole. `notCaptured` is the escape, and it exists so the honest
 * answer is available — an agent cornered by a gate it cannot satisfy invents
 * something plausible instead.
 *
 * Only a FAILURE owes the page tree. A block's page is worth having too, but
 * nothing renders it yet, and capture nobody reads is the mistake above.
 */
function checkOutcomeFields(s, stage) {
  const errors = [];
  const ev = s.evidence;
  const hasEvidence = ev && typeof ev === "object" && !Array.isArray(ev);

  if (s.outcome === "passed") {
    if (ev !== undefined) {
      errors.push("passed but carries `evidence` — nothing reads it on a pass, so do not capture it");
    }
    return errors;
  }

  if (!REPRODUCED.has(s.reproduced)) {
    errors.push(
      `\`reproduced\` is ${JSON.stringify(s.reproduced)}; it must be one of ${[...REPRODUCED].join(", ")} — ` +
        `did this outcome hold when you drove the scenario again?`,
    );
  }
  if (typeof s.note !== "string" || s.note.trim() === "") {
    errors.push(
      "no `note` — one sentence on what you saw that the other fields do not hold, " +
        'or "nothing further" if there is none. Observation only, never a cause',
    );
  }

  const stated = typeof ev?.notCaptured === "string" && ev.notCaptured.trim() !== "";
  if (!hasEvidence) {
    errors.push(
      `${s.outcome} with no \`evidence\` — re-drive the scenario and record what the network and ` +
        `console showed. If that cannot be captured, say why: "evidence": { "notCaptured": "<reason>" }`,
    );
    return errors;
  }
  if (!stated && !(Array.isArray(ev.network) && Array.isArray(ev.console))) {
    errors.push(
      "`evidence` needs `network` and `console` arrays — an EMPTY array is an answer " +
        '("nothing left the page"), an absent one is a hole. Re-drive the scenario, or state the ' +
        "gap with a non-empty `notCaptured`.",
    );
  }
  if (s.outcome === "failed" && !stated) {
    const field = stage === "capture" ? "snapshot" : "snapshotFile";
    if (typeof ev[field] !== "string" || ev[field].trim() === "") {
      errors.push(
        `failed with no \`evidence.${field}\` — the page as the run saw it is the one thing nobody ` +
          `can recover afterwards. Capture it, or state the gap with a non-empty \`notCaptured\`.`,
      );
    }
  }
  return errors;
}

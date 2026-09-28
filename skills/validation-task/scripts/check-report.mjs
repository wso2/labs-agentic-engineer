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

// Checks an acceptance run's report against the feature files it claims to have
// run.
//
//   node "$AEP_SKILLS_DIR/validation-task/scripts/check-report.mjs" <project-dir>
//
// Exit 0 = the report is answerable for · 1 = usage/IO · 2 = a contract breach.
//
// The run's verdict comes from an agent, and the literature puts the false-
// success rate for a self-assessing agent at 45-75% with LLM judges barely
// better than chance at spotting it. So nothing here judges whether an outcome
// is CORRECT — that is unknowable from the outside. What is checkable is
// whether the agent can be held to it:
//
//   - every scenario in the feature files has an entry, so one that was hard
//     cannot be quietly dropped; it has to be reported `blocked`;
//   - a `passed` scenario carries a Then step with a command and exit 0, so a
//     pass cannot be asserted without something that could have said no;
//   - a `failed` scenario carries a Then that actually failed, AND an `evidence`
//     block saying what the page was doing at the time. That one is capture, not
//     bookkeeping: after the run the page is gone, so a failure reported without
//     it cannot be enriched later by anyone. The honest escape is stating the
//     gap (`notCaptured`), never leaving it blank;
//   - every step whose exit code does NOT settle it records what was `observed`.
//     An exit code is the verdict only for a predicate like `wait`. `get count`
//     exits 0 because the command RAN, so a pass resting on one is unauditable
//     unless the value the agent read is written down. Same for a `blocked`
//     step, which has no command at all and would otherwise carry no reason.
//
// NO DEPENDENCIES, deliberately. This file ships INSIDE the skill (the mirror
// carries a skill's `scripts/`), so it runs from the project clone in a
// validation pod where no package.json of ours is installed and a bare
// `import "@cucumber/gherkin"` would not resolve — NODE_PATH does not apply to
// ESM. The repo-root `lint-acceptance-criteria.mjs` keeps the real parser; it
// is a developer gate that runs where the workspace is installed.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join } from "node:path";

const OUTCOMES = new Set(["passed", "failed", "blocked", "unjudgeable"]);

const SCHEMA_VERSION = 2;

/**
 * Commands that PRINT a value rather than answering with their exit code. For
 * these, exit 0 means "the command ran" and the agent did the judging, so the
 * value it read has to be in the report.
 */
const VALUE_COMMAND = /\bget\s+(count|value|url|text)\b/;

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
function effectiveKeywords(steps) {
  let current = "";
  return steps.map((st) => {
    const k = (st.keyword ?? "").trim();
    if (k !== "And" && k !== "But" && k !== "*" && k !== "") current = k;
    return current;
  });
}

/** Why this step's exit code is not the verdict, or "" when it is. */
function needsObserved(step) {
  if (typeof step.exit === "number" && step.exit !== 0) return "the command exited nonzero";
  if (VALUE_COMMAND.test(step.command ?? "")) return "the command prints a value rather than answering with its exit code";
  return "";
}

/**
 * Every scenario one feature file declares, as {rule, scenario, line}.
 *
 * A line scanner, not a parser — see the dependency note above. It is enough
 * because this checker needs identity and location, never the step AST, and
 * because the shape it reads is the one `acceptance-criteria` authors:
 * `Feature` → `Rule` → `Scenario`. Three things it must not be fooled by, and
 * each is handled: a `#` comment, a docstring whose body happens to start a
 * line with `Scenario:`, and `Example:` — which is the Gherkin grammar's
 * synonym for `Scenario:`, not a different construct.
 *
 * `Scenario Outline:` is deliberately counted as ONE scenario. The real parser
 * did the same: nothing here expands an Examples table, and a report that
 * answers the outline once is answering what the file declares once.
 */
function scanFeature(text) {
  let feature = "";
  let rule = "";
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
    if (line.startsWith("Feature:")) feature = take("Feature:");
    else if (line.startsWith("Rule:")) rule = take("Rule:");
    else if (line.startsWith("Scenario Outline:")) scenarios.push({ rule, name: take("Scenario Outline:"), line: i + 1 });
    else if (line.startsWith("Scenario:")) scenarios.push({ rule, name: take("Scenario:"), line: i + 1 });
    else if (line.startsWith("Example:")) scenarios.push({ rule, name: take("Example:"), line: i + 1 });
  });

  return { feature, scenarios };
}

const projectDir = process.argv[2];
if (!projectDir) {
  console.error('usage: node check-report.mjs <project-dir>');
  process.exit(1);
}

const featureDir = join(projectDir, "specs/validation/acceptance");
const reportPath = join(projectDir, "tests/acceptance/report.json");
for (const [label, p] of [["acceptance dir", featureDir], ["report", reportPath]]) {
  if (!existsSync(p)) {
    console.error(`error: ${label} not found at ${p}`);
    process.exit(1);
  }
}

let report;
try {
  report = JSON.parse(readFileSync(reportPath, "utf8"));
} catch (e) {
  console.error(`error: report is not valid JSON — ${e.message}`);
  process.exit(2);
}

/** Every scenario the feature files define, as "feature ▸ rule ▸ scenario". */
const expected = new Map();
for (const file of readdirSync(featureDir).filter((f) => f.endsWith(".feature")).sort()) {
  const { feature, scenarios } = scanFeature(readFileSync(join(featureDir, file), "utf8"));
  if (!feature) continue;
  for (const s of scenarios) expected.set(`${feature} ▸ ${s.rule} ▸ ${s.name}`, `${file}:${s.line}`);
}

const errors = [];
const entries = Array.isArray(report.scenarios) ? report.scenarios : null;
if (!entries) errors.push("report has no `scenarios` array");
if (report.schemaVersion !== SCHEMA_VERSION)
  errors.push(`unknown schemaVersion ${JSON.stringify(report.schemaVersion)} (expected ${SCHEMA_VERSION})`);
if (!report.isolation) errors.push("report does not say how scenarios were kept independent of each other");
if (!report.commit) errors.push("report does not say which commit it judged");

const seen = new Map();
const tally = { passed: 0, failed: 0, blocked: 0, unjudgeable: 0 };

for (const [i, s] of (entries ?? []).entries()) {
  const key = `${s.feature} ▸ ${s.rule ?? ""} ▸ ${s.scenario}`;
  const at = `scenarios[${i}] "${s.scenario}"`;

  if (!expected.has(key)) errors.push(`${at}: no such scenario in the feature files (${key})`);
  if (seen.has(key)) errors.push(`${at}: reported twice`);
  seen.set(key, true);

  if (!OUTCOMES.has(s.outcome)) {
    errors.push(`${at}: outcome ${JSON.stringify(s.outcome)} is not one of ${[...OUTCOMES].join(", ")}`);
    continue;
  }
  tally[s.outcome] += 1;

  const steps = s.steps ?? [];
  const keywords = effectiveKeywords(steps);
  const thens = steps.filter((_, i) => keywords[i] === "Then");
  const settled = thens.filter((st) => st.command && typeof st.exit === "number");

  // Where the scenario is written, so a reader and a repair issue can reach it.
  const where = expected.get(key);
  if (where) {
    const [file, line] = where.split(":");
    if (!s.featureFile) errors.push(`${at}: no featureFile — a repair issue cannot point at the scenario`);
    else if (!s.featureFile.endsWith(file)) errors.push(`${at}: featureFile is ${s.featureFile}, but the scenario is in ${file}`);
    if (s.line !== undefined && String(s.line) !== line)
      errors.push(`${at}: line ${s.line}, but the scenario is at line ${line}`);
  }

  // A pass has to be backed by something that could have said no.
  if (s.outcome === "passed") {
    if (thens.length === 0) errors.push(`${at}: passed with no Then step recorded`);
    else if (settled.length !== thens.length)
      errors.push(`${at}: passed but ${thens.length - settled.length} of ${thens.length} Then steps carry no command+exit`);
    else if (settled.some((st) => st.exit !== 0))
      errors.push(`${at}: passed but a Then step exited nonzero`);
  }

  // A failure has to name what said no. An exit code usually does; a command
  // that prints a value cannot, so there the observed value is the evidence.
  if (s.outcome === "failed" && !settled.some((st) => st.exit !== 0) && !thens.some((st) => st.observed)) {
    errors.push(`${at}: failed but no Then records a nonzero exit or an observed value — what failed?`);
  }

  // A failure has to say what the SYSTEM was doing, not only which assertion
  // lost. `POST /items → 201` with the list unchanged is a rendering defect; no
  // request at all is a wiring defect — different files, and the trace alone
  // cannot tell them apart.
  //
  // Checked HARD because the evidence is only obtainable while the page is still
  // open. An `observed` can be re-read from a re-run; a network trace from a run
  // that has ended cannot be re-read by anybody. `notCaptured` is the escape, and
  // it exists so the honest answer is available — an agent cornered by a gate it
  // cannot satisfy invents something plausible instead.
  if (s.outcome === "failed") {
    const ev = s.evidence;
    const stated = typeof ev?.notCaptured === "string" && ev.notCaptured.trim() !== "";
    if (!ev || typeof ev !== "object" || Array.isArray(ev)) {
      errors.push(
        `${at}: failed with no \`evidence\` — re-drive the scenario and record what the network ` +
          `and console showed when it failed. If that cannot be captured, say why: ` +
          `"evidence": { "notCaptured": "<reason>" }`,
      );
    } else if (!stated && !(Array.isArray(ev.network) && Array.isArray(ev.console))) {
      errors.push(
        `${at}: \`evidence\` needs \`network\` and \`console\` arrays — an EMPTY array is an ` +
          `answer ("nothing left the page"), an absent one is a hole. Re-drive the scenario, or ` +
          `state the gap with a non-empty \`notCaptured\`.`,
      );
    }
  }

  // A block has to say what stopped it, or an app that correctly refuses reads
  // the same as one that is broken.
  if (s.outcome === "blocked" && !steps.some((st) => st.observed)) {
    errors.push(`${at}: blocked but no step records what was observed — why could it not run?`);
  }

  // Wherever the exit code is not the verdict, record what the agent read.
  // EVERY step, not only the assertions. A `When` settled by a value-returning
  // command is where the run records what the system did — the 401 rule reads it,
  // and `deciding()` falls back to it when no `Then` observed anything — so a
  // `When` that exits nonzero or prints a value and says nothing is the same hole
  // as a silent `Then`.
  steps.forEach((st, i) => {
    if (!st.command) return; // a step with no command is covered by the `blocked` rule
    const why = needsObserved(st);
    if (why && !st.observed) {
      errors.push(`${at}: a ${keywords[i] || st.keyword || "step"} carries no \`observed\` and ${why}`);
    }
  });
}

for (const [key, where] of expected) {
  if (!seen.has(key)) errors.push(`${where}: "${key.split(" ▸ ").pop()}" has no entry in the report`);
}

console.log(`acceptance report — ${report.scenarios?.length ?? 0} of ${expected.size} scenarios`);
console.log(`  passed ${tally.passed} · failed ${tally.failed} · blocked ${tally.blocked} · unjudgeable ${tally.unjudgeable}`);
if (report.isolation) console.log(`  isolation  ${report.isolation}`);

if (errors.length) {
  console.log(`\nfailed (${errors.length}):`);
  for (const e of errors) console.log(`  x ${e}`);
  process.exit(2);
}
console.log("\nok — the report is answerable for every scenario");

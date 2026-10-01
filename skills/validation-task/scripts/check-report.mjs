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
//   node "$AEP_SKILLS_DIR/validation-task/scripts/check-report.mjs" <project-dir> \
//     [--features F1,F2] [--held-back F2.4]
//
// A version validates its built scope (the validation issue names it): with
// --features, only the feature files whose `Feature:` line leads with one of
// those IDs are expected, and with --held-back, a rule whose every `@story-`
// tag is held back is not. An entry outside the scope is a breach like any
// other unknown entry: it reports on code this version did not build.
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

import { readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import {
  FEATURE_DIR,
  REPORT_PATH,
  SCHEMA_VERSION,
  checkScenario,
  readFeatureScenarios,
  scenarioKey,
} from "./report-rules.mjs";



/** `--name a,b` from argv, or null when absent. */
function listArg(argv, name) {
  const i = argv.indexOf(name);
  if (i < 0) return null;
  return (argv[i + 1] ?? "").split(",").map((x) => x.trim()).filter(Boolean);
}

const argv = process.argv.slice(2);
const projectDir = argv[0];
if (!projectDir || projectDir.startsWith("--")) {
  console.error("usage: node check-report.mjs <project-dir> [--features F1,F2] [--held-back F2.4]");
  process.exit(1);
}
const scopeFeatures = listArg(argv, "--features");
const heldBack = new Set(listArg(argv, "--held-back") ?? []);

/** Whether this version validates a scenario: its feature is built and its rule stands for a story that is not held back. */
function inScope(feature, scenario) {
  if (scopeFeatures && !scopeFeatures.includes(/^(F\d+)\b/.exec(feature)?.[1] ?? feature)) return false;
  return scenario.stories.length === 0 || scenario.stories.some((id) => !heldBack.has(id));
}

const featureDir = join(projectDir, FEATURE_DIR);
const reportPath = join(projectDir, REPORT_PATH);
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

const expected = readFeatureScenarios(featureDir, inScope);

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
  const key = scenarioKey(s.feature, s.rule, s.scenario);
  const at = `scenarios[${i}] "${s.scenario}"`;

  if (!expected.has(key)) errors.push(`${at}: no such scenario in the feature files (${key})`);
  if (seen.has(key)) errors.push(`${at}: reported twice`);
  seen.set(key, true);

  if (tally[s.outcome] !== undefined) tally[s.outcome] += 1;
  for (const e of checkScenario(s, expected)) errors.push(`${at}: ${e}`);

  // The report names the snapshot rather than carrying it, so the name has to
  // resolve. A path to a file that was never written reads exactly like evidence
  // until somebody follows it, which is the one moment it cannot be recovered.
  const file = s.evidence?.snapshotFile;
  if (typeof file === "string" && file.trim() !== "" && !existsSync(join(projectDir, file))) {
    errors.push(`${at}: \`evidence.snapshotFile\` points at ${file}, which does not exist`);
  }
}

for (const [key, where] of expected) {
  if (!seen.has(key)) errors.push(`${where}: "${key.split(" \u25b8 ").pop()}" has no entry in the report`);
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

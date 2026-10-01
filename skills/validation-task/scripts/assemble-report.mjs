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

// Builds `tests/acceptance/report.json` from the captures a run left behind.
//
//   node "$AEP_SKILLS_DIR/validation-task/scripts/assemble-report.mjs" <project-dir> <run.json>
//
// `run.json` carries the two things no single scenario can answer:
//
//   { "isolation": "<how scenarios were kept independent>", "baseUrl": "https://…" }
//
// Exit 0 = assembled · 1 = usage/IO · 2 = the captures do not cover the run.
//
// The report is ASSEMBLED, not authored, and that is the point. An agent writing
// one document at the end is summarising twenty scenarios from a context that has
// been compacted since it saw most of them; this reads what was written when each
// was fresh. It also turns "no scenario was quietly dropped" from a claim the
// agent makes into a count this script performs against the feature files.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  FEATURE_DIR,
  REPORT_PATH,
  SCHEMA_VERSION,
  SNAPSHOT_DIR,
  capturesDir,
  readFeatureScenarios,
  scenarioFileName,
  scenarioKey,
} from "./report-rules.mjs";

const [projectDir, runPath] = process.argv.slice(2);
if (!projectDir || !runPath) {
  console.error("usage: node assemble-report.mjs <project-dir> <run.json>");
  process.exit(1);
}

const fail = (msg) => {
  console.error(`error: ${msg}`);
  process.exit(1);
};

let run;
try {
  run = JSON.parse(readFileSync(runPath, "utf8"));
} catch (e) {
  fail(`could not read ${runPath} — ${e.message}`);
}
if (!run.isolation) fail("run.json must say how scenarios were kept independent of each other (`isolation`)");

const dir = capturesDir();
if (!existsSync(dir)) fail(`no captures at ${dir} — nothing was recorded during this run`);

const captures = new Map();
for (const file of readdirSync(dir).filter((f) => f.endsWith(".json")).sort()) {
  let entry;
  try {
    entry = JSON.parse(readFileSync(join(dir, file), "utf8"));
  } catch (e) {
    fail(`capture ${file} is not valid JSON — ${e.message}`);
  }
  const key = scenarioKey(entry.feature, entry.rule, entry.scenario);
  if (captures.has(key)) fail(`two captures claim the same scenario: ${key}`);
  captures.set(key, entry);
}

// Feature-file order, so the report reads in the order the specification does
// rather than in whatever order the run happened to drive them.
const expected = readFeatureScenarios(join(projectDir, FEATURE_DIR));
const missing = [...expected.keys()].filter((k) => !captures.has(k));
const unknown = [...captures.keys()].filter((k) => !expected.has(k));
if (missing.length || unknown.length) {
  console.error("the captures do not cover the feature files:");
  for (const k of missing) console.error(`  x no capture for "${k}" (${expected.get(k)})`);
  for (const k of unknown) console.error(`  x captured "${k}", which no feature file declares`);
  console.error("\nEvery scenario needs an entry, including the ones you could not drive — those are");
  console.error("`blocked`. Capture what is missing, then assemble again.");
  process.exit(2);
}

// The snapshot leaves the report and becomes a file beside it.
//
// It is the one unbounded item in a document a repair agent reads whole: pointing
// INTO `report.json` for a single scenario's page costs an agent the entire
// report, every other scenario's trace included. A file of its own costs what it
// weighs, and an agent that decides it does not need the page pays nothing.
//
// Written FRESH each time, so a snapshot left by a previous attempt — for a
// scenario that now passes — does not survive the merge and sit on main looking
// like evidence for a failure nobody can find.
const snapshotDir = join(projectDir, SNAPSHOT_DIR);
rmSync(snapshotDir, { recursive: true, force: true });

const scenarios = [];
for (const key of expected.keys()) {
  const entry = captures.get(key);
  const snapshot = entry.evidence?.snapshot;
  if (typeof snapshot === "string" && snapshot.trim() !== "") {
    const rel = join(SNAPSHOT_DIR, `${scenarioFileName(entry)}.txt`);
    mkdirSync(snapshotDir, { recursive: true });
    writeFileSync(join(projectDir, rel), snapshot.endsWith("\n") ? snapshot : `${snapshot}\n`);
    const { snapshot: _dropped, ...rest } = entry.evidence;
    entry.evidence = { ...rest, snapshotFile: rel };
  }
  scenarios.push(entry);
}

let commit = "";
try {
  commit = execFileSync("git", ["rev-parse", "HEAD"], { cwd: projectDir, encoding: "utf8" }).trim();
} catch (e) {
  fail(`could not read the commit under test — ${e.message}`);
}

const report = {
  schemaVersion: SCHEMA_VERSION,
  generatedAt: new Date().toISOString(),
  commit,
  ...(run.baseUrl ? { baseUrl: run.baseUrl } : {}),
  isolation: run.isolation,
  scenarios,
};

const reportPath = join(projectDir, REPORT_PATH);
mkdirSync(join(projectDir, "tests/acceptance"), { recursive: true });
writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`);

const tally = scenarios.reduce((acc, s) => ({ ...acc, [s.outcome]: (acc[s.outcome] ?? 0) + 1 }), {});
console.log(`assembled ${REPORT_PATH} — ${scenarios.length} scenarios at ${commit.slice(0, 8)}`);
console.log(
  `  passed ${tally.passed ?? 0} · failed ${tally.failed ?? 0} · blocked ${tally.blocked ?? 0} · unjudgeable ${tally.unjudgeable ?? 0}`,
);

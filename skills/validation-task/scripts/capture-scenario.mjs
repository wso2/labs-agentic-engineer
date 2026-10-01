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

// Records ONE scenario the moment it finishes, while its page is still open.
//
//   node "$AEP_SKILLS_DIR/validation-task/scripts/capture-scenario.mjs" <project-dir> [entry.json]
//
// Reads the entry from that file, or from stdin when the path is `-` or omitted.
// Exit 0 = captured · 1 = usage/IO · 2 = the entry does not satisfy its rules.
//
// Two reasons this exists rather than the agent writing `report.json` at the end.
//
// The first is that the end is too late to be held to anything. The checker has
// always run after every page is gone, so a capture it rejects can only be fixed
// by re-driving the scenario or by admitting the gap. Run here, the same rules
// fire while the browser still has the answer — a rejection costs one command,
// not a re-drive.
//
// The second is that a run's context is not a safe place to keep evidence. The
// agent's session auto-compacts, unbounded, and a `network requests` dump from
// twenty scenarios ago is exactly the bulky, stale-looking tool output a
// compaction discards. Writing it down is the only thing that survives.
//
// The agent never names the file. That is deliberate: the name has to be stable
// across a re-drive and unique across scenarios, and a naming rule an agent
// computes by hand is a rule it gets wrong under load.

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { FEATURE_DIR, capturesDir, checkScenario, readFeatureScenarios, scenarioFileName } from "./report-rules.mjs";

const [projectDir, entryPath = "-"] = process.argv.slice(2);
if (!projectDir) {
  console.error("usage: node capture-scenario.mjs <project-dir> [entry.json]");
  process.exit(1);
}

let raw;
try {
  raw = readFileSync(entryPath === "-" ? 0 : entryPath, "utf8");
} catch (e) {
  console.error(`error: could not read the entry — ${e.message}`);
  process.exit(1);
}

let entry;
try {
  entry = JSON.parse(raw);
} catch (e) {
  console.error(`error: the entry is not valid JSON — ${e.message}`);
  process.exit(2);
}
if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
  console.error("error: the entry must be one scenario object");
  process.exit(2);
}

// The feature files are the only thing that can say this scenario exists and
// where it is written. A capture naming a scenario nobody declared is a typo that
// would otherwise surface at assembly, long after the page it describes is gone.
let expected;
try {
  expected = readFeatureScenarios(join(projectDir, FEATURE_DIR));
} catch (e) {
  console.error(`error: could not read ${FEATURE_DIR} — ${e.message}`);
  process.exit(1);
}

const errors = checkScenario(entry, expected, { stage: "capture" });
if (errors.length) {
  console.error(`this scenario cannot be captured as it stands (${errors.length}):`);
  for (const e of errors) console.error(`  x ${e}`);
  console.error("\nThe page is still open. Fix the entry — re-read, re-run the command, or capture what is");
  console.error("missing — and capture again. Do not write something you did not read.");
  process.exit(2);
}

const dir = capturesDir();
const name = `${scenarioFileName(entry)}.json`;
try {
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, name), `${JSON.stringify(entry, null, 2)}\n`);
} catch (e) {
  console.error(`error: could not write the capture — ${e.message}`);
  process.exit(1);
}

console.log(`captured ${entry.outcome} · ${entry.scenario}`);
console.log(`  ${join(dir, name)}`);

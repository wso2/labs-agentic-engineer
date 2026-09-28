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

// Interim VALIDATION-phase task creator for the LOCAL harness (in production the
// platform mints the issue — see services/aep-api/internal/delivery/README.md).
//
// Reads the `.feature` files under `specs/validation/acceptance/` from a project repo
// (authored there by the spec agent's `acceptance-criteria` skill) and
// creates the GitHub validation issue the coding agent will be dispatched
// against. Stands in for the platform trigger + issue builder until that
// lands in aep-api (tech-lead-style generation may replace the rendering
// later).
//
// Usage:
//   node scripts/create-validation-issue.mjs --repo <owner/repo>            # create the issue
//   node scripts/create-validation-issue.mjs --repo <owner/repo> --dry-run  # print body only
//
// Requires an authenticated `gh` CLI.

import { execFileSync } from "node:child_process";

// ---------------------------------------------------------------------------
// Target repo — the deployed project's repo (expected structure:
// specs/validation/acceptance/*.feature committed, specs/design/** for
// component design docs). Required via --repo (or the REPO env var) so the
// issue is never created against a stale hardcoded default.
// ---------------------------------------------------------------------------

const REPO = resolveRepo();

function resolveRepo() {
  const i = process.argv.indexOf("--repo");
  const fromArg = i !== -1 ? process.argv[i + 1] : undefined;
  const repo = fromArg ?? process.env.REPO;
  if (!repo || !/^[^/\s]+\/[^/\s]+$/.test(repo)) {
    console.error(
      "create-validation-issue: pass --repo <owner/repo> (or set REPO) — " +
        "the project repo holding specs/validation/acceptance/*.feature",
    );
    process.exit(1);
  }
  return repo;
}
const CRITERIA_DIR = "specs/validation/acceptance";
const LABELS = ["aep", "validation"];

// Deployed endpoint URLs + test credentials are NOT written into the issue: the
// runner fetches them at dispatch time from the platform's validation-context
// endpoint (kept out of the public issue). The interim local harness serves
// them from token-stub.mjs.

// ---------------------------------------------------------------------------

function gh(args, input) {
  return execFileSync("gh", args, {
    encoding: "utf8",
    input,
    stdio: ["pipe", "pipe", "inherit"],
  });
}

function fetchCriteria() {
  const listing = JSON.parse(gh(["api", `repos/${REPO}/contents/${CRITERIA_DIR}`]));
  const files = [];
  for (const entry of listing) {
    if (entry.type !== "file" || !entry.name.endsWith(".feature")) continue;
    files.push({
      path: `${CRITERIA_DIR}/${entry.name}`,
      content: gh(["api", entry.url, "-H", "Accept: application/vnd.github.raw"]),
    });
  }
  return { files };
}

// Structural validation mirroring skills/acceptance-criteria/SKILL.md. The
// oracle drives everything downstream, so an unusable set fails loudly here.
//
// A line scan, not a parse: this is a local-dev harness with no Gherkin
// dependency, and the real gate on shape is the linter the skill names. What it
// catches is the case that would otherwise mint a validation task with nothing
// to drive.
function validateCriteria(doc) {
  const fail = (msg) => {
    throw new Error(`specs/validation/acceptance/ invalid: ${msg}`);
  };
  if (doc.files.length === 0) fail("no .feature files");
  for (const f of doc.files) {
    if (!/^\s*Feature:/m.test(f.content)) fail(`${f.path} has no Feature:`);
  }
  if (summarize(doc).scenarios === 0) fail("no scenarios across any feature file");
}

// `Scenario:` and `Example:` are synonyms in the Gherkin grammar, so both count.
function summarize(doc) {
  const sum = { files: doc.files.length, rules: 0, scenarios: 0 };
  for (const f of doc.files) {
    for (const line of f.content.split("\n")) {
      const t = line.trim();
      if (t.startsWith("Rule:")) sum.rules++;
      else if (t.startsWith("Scenario:") || t.startsWith("Example:")) sum.scenarios++;
    }
  }
  return sum;
}

/** "1 rule" / "2 rules" — the body is read by a person. */
function plural(n, noun) {
  return n === 1 ? `${n} ${noun}` : `${n} ${noun}s`;
}

function renderBody(doc) {
  const sum = summarize(doc);
  const lines = [];

  lines.push(
    "Drive every scenario in this project's acceptance criteria against the deployed system, and open a PR containing the run's report.",
    "",
    "The deployed endpoint URLs and any test credentials are provided to the validation runner by the platform at dispatch time — they are not in this issue.",
    "",
    "## Acceptance criteria",
    `The source of truth is \`${CRITERIA_DIR}/\` in this repo — ${plural(sum.scenarios, "scenario")} across ${plural(sum.rules, "rule")}. It is read-only input for this task — do not modify it or anything else under \`specs/\`.`,
    "",
    "There is no test code to author. The scenario text IS the test: drive each one through the deployed app and record what settled it.",
    ""
  );

  for (const f of doc.files) lines.push(`- \`${f.path}\``);
  lines.push("");

  lines.push(
    "Per-component design docs: `specs/design/components/<name>/design.md` (OpenAPI contract, when present, alongside as `openapi.yaml`); system overview: `specs/design/design.md`.",
    "",
    "## Report",
    "Commit `tests/acceptance/report.json` — one entry per scenario in the feature files, including the ones you could not drive."
  );

  return lines.join("\n");
}

function main() {
  const dryRun = process.argv.includes("--dry-run");
  const doc = fetchCriteria();
  validateCriteria(doc);

  if (dryRun) {
    console.log(renderBody(doc));
    return;
  }

  const title = `Validate ${REPO.split("/")[1]} against its validation criteria`;
  for (const label of LABELS) {
    // Idempotent: --force updates the label if it already exists.
    gh(["label", "create", label, "--repo", REPO, "--force"]);
  }
  const url = gh(
    ["issue", "create", "--repo", REPO, "--title", title, "--label", LABELS.join(","), "--body-file", "-"],
    renderBody(doc)
  ).trim();

  console.log(url);
}

main();

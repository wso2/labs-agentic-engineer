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

// Relabels a project repo's issues onto the KIND vocabulary
// (services/aep-api/internal/delivery/labels.go).
//
// WHY A SCRIPT AND NOT A COMPATIBILITY SHIM. Label matching is exact string
// equality — there is no prefix rule anywhere in the platform — so an issue
// carrying only a retired name becomes invisible to every routing predicate the
// moment the vocabulary changes. The alternative, teaching the hot path to read
// both vocabularies for a while, is the same rule in two places, which is the
// thing the kind vocabulary exists to remove. So the issues move instead, once.
//
// WHAT IT DOES, per open or closed issue in the repo:
//
//   aep, and no kind      -> + development   (the planner's output, historically
//                                             the only thing carrying bare `aep`)
//   aep:provision         -> + provision     (and NOT `aep`: a gate is a hold)
//   aep:validation        -> + validation + aep
//   aep:codingagent       -> + bug + src/user, and the retired label is removed
//                                             FROM THE ISSUE
//   none of the above     -> untouched       (a ledger issue: nobody's work)
//
// The retired label NAMES are left on the repo. Nothing in the platform deletes
// a repo label, `EnsureLabel` POSTs and so never recolours an existing one, and
// a lingering unused name costs nothing — where deleting one would rewrite the
// timeline of every issue that ever carried it.
//
// IDEMPOTENT: it adds only labels an issue does not already carry, and removes
// `aep:codingagent` only where it is present, so a second run reports nothing to
// do. Safe to re-run after a partial failure.
//
// Usage:
//   node scripts/migrate-issue-labels.mjs --repo <owner/repo> --dry-run
//   node scripts/migrate-issue-labels.mjs --repo <owner/repo>
//
// Requires an authenticated `gh` CLI with write access to the repo.

import { execFileSync } from "node:child_process";

// The vocabulary, re-spelled here because a shell script cannot import Go. It
// is short enough to read against labels.go in one glance, which is the only
// verification available; keep them side by side when either changes.
const ARM = "aep";
const KIND_DEVELOPMENT = "development";
const KIND_BUG = "bug";
const KIND_CONFLICT = "conflict";
const KIND_VALIDATION = "validation";
const KIND_PROVISION = "provision";
const SRC_USER = "src/user";

const RETIRED_GATE = "aep:provision";
const RETIRED_VALIDATION = "aep:validation";
const RETIRED_ADOPT = "aep:codingagent";

// EVERY kind in labels.go, for the "does this issue already have one" test. An
// issue that already carries a kind is left alone whatever else it carries — a
// second kind is a corruption, and this script must never be the thing that
// creates one. A kind missing from this list is exactly how it would: the armed
// issue would read as kindless and the last branch would stamp `development`
// beside the kind it already had.
const KINDS = [KIND_DEVELOPMENT, KIND_BUG, KIND_CONFLICT, KIND_VALIDATION, KIND_PROVISION];

function usage(message) {
  console.error(`migrate-issue-labels: ${message}`);
  console.error("");
  console.error("  node scripts/migrate-issue-labels.mjs --repo <owner/repo> [--dry-run]");
  process.exit(1);
}

function resolveRepo() {
  const i = process.argv.indexOf("--repo");
  const repo = (i !== -1 ? process.argv[i + 1] : undefined) ?? process.env.REPO;
  if (!repo || !/^[^/\s]+\/[^/\s]+$/.test(repo)) {
    usage("pass --repo <owner/repo> (or set REPO) — the project repo to relabel");
  }
  return repo;
}

const REPO = resolveRepo();
const DRY_RUN = process.argv.includes("--dry-run");

function gh(args) {
  return execFileSync("gh", args, { encoding: "utf8", stdio: ["pipe", "pipe", "inherit"] });
}

// GitHub matches label names case-insensitively, so the platform's HasLabel does
// too. A hand-typed `AEP` must migrate like `aep` rather than being read as a
// different population and left behind.
function has(labels, name) {
  return labels.some((l) => l.toLowerCase() === name.toLowerCase());
}

/**
 * The whole migration rule, as a pure function over an issue's current labels.
 *
 * Returns the labels to ADD and the labels to REMOVE — both already filtered
 * against what the issue holds, so an empty pair means "nothing to do" and the
 * caller can report and skip without a second thought.
 */
function plan(labels) {
  const add = [];
  const remove = [];
  const wants = (name) => {
    if (!has(labels, name) && !add.includes(name)) add.push(name);
  };

  if (has(labels, RETIRED_GATE)) {
    // A gate is NOT armed. Adding `aep` here would put every open gate into the
    // working set, and the run would dispatch an agent at work the platform
    // itself owes.
    wants(KIND_PROVISION);
  } else if (has(labels, RETIRED_VALIDATION)) {
    // The validation task gains the arming label it deliberately lacked: it is
    // real agent work, and the auto-merge policy now admits it on that label
    // rather than by naming it a second time.
    wants(KIND_VALIDATION);
    wants(ARM);
  } else if (has(labels, RETIRED_ADOPT)) {
    // A human handed this to the agent. It is a defect from a human, and the
    // retired trigger comes off the issue — the arming label is the trigger now,
    // and leaving both would say the same thing twice in two vocabularies.
    wants(KIND_BUG);
    wants(SRC_USER);
    wants(ARM);
    remove.push(RETIRED_ADOPT);
  } else if (has(labels, ARM) && !KINDS.some((k) => has(labels, k))) {
    // Armed with no kind. Historically this is the planner's output and nothing
    // else: every other minter's issues carried a second `aep:*` label. Note the
    // RUNTIME reading of this state is `bug` (delivery.workKindOf), which is
    // deliberately different — that default is about what a human's adoption
    // means today, while this is about what the corpus actually holds.
    wants(KIND_DEVELOPMENT);
  }
  return { add, remove };
}

function listIssues() {
  // State `all`: a closed issue is part of its version's ledger and the console
  // renders it, so a closed issue with a retired vocabulary would read as an
  // untagged row forever. Paging to a high limit rather than following links —
  // a project repo's issue count is in the hundreds, not the millions.
  const raw = gh([
    "issue",
    "list",
    "--repo",
    REPO,
    "--state",
    "all",
    "--limit",
    "1000",
    "--json",
    "number,title,labels,state",
  ]);
  return JSON.parse(raw).map((i) => ({
    number: i.number,
    title: i.title,
    state: i.state,
    labels: (i.labels ?? []).map((l) => l.name),
  }));
}

function main() {
  const issues = listIssues();
  let changed = 0;

  for (const issue of issues) {
    const { add, remove } = plan(issue.labels);
    if (add.length === 0 && remove.length === 0) continue;
    changed++;

    const what = [add.length ? `+${add.join(" +")}` : "", remove.length ? `-${remove.join(" -")}` : ""]
      .filter(Boolean)
      .join("  ");
    console.log(`#${issue.number} [${issue.state}] ${what}  ${issue.title}`);
    if (DRY_RUN) continue;

    // `gh issue edit` creates a label it does not find, so no separate ensure
    // pass is needed. Add before remove: an issue is never, even briefly, left
    // carrying neither vocabulary.
    const args = ["issue", "edit", String(issue.number), "--repo", REPO];
    for (const l of add) args.push("--add-label", l);
    for (const l of remove) args.push("--remove-label", l);
    gh(args);
  }

  const verb = DRY_RUN ? "would relabel" : "relabelled";
  console.log(`\n${verb} ${changed} of ${issues.length} issues in ${REPO}`);
  if (DRY_RUN && changed > 0) console.log("re-run without --dry-run to apply");
}

main();

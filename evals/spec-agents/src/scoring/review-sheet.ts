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
 * The human review queue v1 (#355): any run banded review-or-worse emits one
 * markdown sheet into `eval-reviews/` — judge evidence + pointers + a blank
 * human verdict section. The sheets ARE the queue and the durable record; no
 * UI. Committing a filled-in sheet is the act of recording human judgment.
 */

import { mkdirSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";
import { REPO_ROOT, REVIEWS_DIR } from "../config.js";
import type { SectionVerdict } from "./bands.js";
import type { JudgeReport } from "./judge.js";
import type { StructuralReport } from "./structural.js";

export interface ReviewSection {
  verdict: SectionVerdict;
  structural: StructuralReport;
  judge: JudgeReport | null;
  skipped?: boolean;
}

export function needsReview(sections: ReviewSection[]): boolean {
  return sections.some((s) => !s.skipped && s.verdict.band !== "pass");
}

/**
 * A run artifact's path as the repository sees it.
 *
 * The sheet is COMMITTED — it is the human verdict record for a run — so an
 * absolute path bakes the machine and account that happened to run the eval
 * into the repo, and reads as a dead link on anyone else's checkout.
 */
function repoRelative(path: string): string {
  const rel = relative(REPO_ROOT, path);
  // Outside the repo entirely (a caller pointing at a scratch dir): the
  // absolute path is still the only useful thing to say.
  return rel && !rel.startsWith("..") ? rel : path;
}

export function renderReviewSheet(input: {
  scenario: string;
  evalName: string;
  when: string;
  sections: ReviewSection[];
  transcriptPath: string;
  tracePath: string;
}): string {
  const lines: string[] = [
    `# Eval review — ${input.evalName} / ${input.scenario}`,
    "",
    `- Run: ${input.when}`,
    `- Transcript: ${repoRelative(input.transcriptPath)}`,
    `- Raw trace: ${repoRelative(input.tracePath)}`,
    "",
  ];
  for (const s of input.sections) {
    const v = s.verdict;
    lines.push(`## ${v.section} — ${s.skipped ? "SKIPPED (upstream fail)" : `${v.band.toUpperCase()} (${v.score})`}${v.forcedReview ? " · demoted by mustNot" : ""}`, "");
    if (s.skipped) continue;
    lines.push(`Structural ${(s.structural.score * 100).toFixed(0)}%:`);
    for (const c of s.structural.checks) lines.push(`- [${c.ok ? "x" : " "}] ${c.name}${c.detail ? ` — ${c.detail}` : ""}`);
    lines.push("");
    if (s.judge) {
      lines.push(`Rubric judge ${(s.judge.weightedScore * 100).toFixed(0)}%:`);
      for (const i of s.judge.items) lines.push(`- [${i.covered ? "x" : " "}] (w${i.weight}) ${i.item}\n  - ${i.evidence}`);
      for (const m of s.judge.mustNotViolations) lines.push(`- ⛔ mustNot violated: ${m.item}\n  - ${m.evidence}`);
      if (s.judge.inventions.length) {
        lines.push("", "Inventions flagged (unscored — human call):");
        for (const inv of s.judge.inventions) lines.push(`- ${inv}`);
      }
      lines.push("");
    }
  }
  lines.push(
    "## Human verdict",
    "",
    "- [ ] Agree with the bands above",
    "- [ ] Override: <section> should be <band> because …",
    "",
    "Notes:",
    "",
    "",
  );
  return lines.join("\n");
}

/** Write the sheet; returns its path (or null when everything passed). */
export function emitReviewSheet(input: {
  scenario: string;
  evalName: string;
  sections: ReviewSection[];
  transcriptPath: string;
  tracePath: string;
}): string | null {
  if (!needsReview(input.sections)) return null;
  const when = new Date().toISOString();
  mkdirSync(REVIEWS_DIR, { recursive: true });
  const file = join(REVIEWS_DIR, `${input.scenario}-${when.replace(/[:.]/g, "-")}.md`);
  writeFileSync(file, renderReviewSheet({ ...input, when }));
  return file;
}

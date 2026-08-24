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

import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { SectionVerdict } from "../src/scoring/bands.js";
import { needsReview, renderReviewSheet, type ReviewSection } from "../src/scoring/review-sheet.js";
import { REPO_ROOT } from "../src/config.js";

const verdict = (band: SectionVerdict["band"], score: number): SectionVerdict => ({
  section: "requirements",
  structural: 1,
  judge: 0.5,
  score,
  band,
  forcedReview: false,
});

const section = (band: SectionVerdict["band"], score: number, skipped?: boolean): ReviewSection => ({
  verdict: verdict(band, score),
  structural: { score: 1, checks: [{ name: "exists", ok: true }] },
  judge: null,
  ...(skipped ? { skipped: true } : {}),
});

test("needsReview: only review-or-worse runs queue a sheet (#355)", () => {
  assert.equal(needsReview([section("pass", 90)]), false);
  assert.equal(needsReview([section("review", 60)]), true);
  assert.equal(needsReview([section("fail", 20)]), true);
  // A skipped section alone doesn't queue — its upstream fail already did.
  assert.equal(needsReview([section("pass", 90), section("fail", 0, true)]), false);
});

test("renderReviewSheet carries verdicts, checks, and the blank human section", () => {
  const sheet = renderReviewSheet({
    scenario: "lunch-coordinator",
    evalName: "requirements-section",
    when: "2026-08-02T00:00:00Z",
    sections: [section("review", 60)],
    transcriptPath: "/tmp/t.md",
    tracePath: "/tmp/t.json",
  });
  assert.match(sheet, /REVIEW \(60\)/);
  assert.match(sheet, /\[x\] exists/);
  assert.match(sheet, /## Human verdict/);
  assert.match(sheet, /\/tmp\/t\.md/);
});

// The sheet is committed as the human verdict record for a run, so an absolute
// artifact path would bake the machine and account that ran the eval into the
// repository and read as a dead link on anyone else's checkout.
test("renderReviewSheet: artifact paths are repo-relative, never absolute", () => {
  const sheet = renderReviewSheet({
    scenario: "lunch-coordinator",
    evalName: "requirements-section",
    when: "2026-08-21T00:00:00.000Z",
    sections: [section("review", 60)],
    transcriptPath: `${REPO_ROOT}/playground/.projects/spec-agent-evals/req.transcript.md`,
    tracePath: `${REPO_ROOT}/playground/.projects/spec-agent-evals/req.trace.json`,
  });

  assert.match(sheet, /- Transcript: playground\/\.projects\/spec-agent-evals\/req\.transcript\.md/);
  assert.match(sheet, /- Raw trace: playground\/\.projects\/spec-agent-evals\/req\.trace\.json/);
  assert.equal(sheet.includes(REPO_ROOT), false);
});

test("renderReviewSheet: a path outside the repo is left absolute", () => {
  // Nothing better to say — a relative path would climb out of the tree.
  const sheet = renderReviewSheet({
    scenario: "lunch-coordinator",
    evalName: "requirements-section",
    when: "2026-08-21T00:00:00.000Z",
    sections: [section("review", 60)],
    transcriptPath: "/tmp/scratch/req.transcript.md",
    tracePath: "/tmp/scratch/req.trace.json",
  });

  assert.match(sheet, /- Transcript: \/tmp\/scratch\/req\.transcript\.md/);
});

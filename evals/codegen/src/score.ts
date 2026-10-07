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
 * The score, computed in code from the judge's verdicts (ADR-0004).
 * Weighted pass ratio × 100; blocked is a fail; a violated mustNot caps the
 * band at review.
 */

import type { Item } from "./case.js";
import { BANDS } from "./config.js";

export type Band = "pass" | "review" | "fail";

/** The walker's own verdict on an item — the evidence, not the score. */
export type WalkVerdict = "pass" | "fail" | "blocked";

export interface JudgedItem {
  id: string;
  verdict: "pass" | "fail";
  /** What a user would see, one sentence; empty on a pass. */
  symptom: string;
}

export interface JudgedMustNot {
  id: string;
  violated: boolean;
  evidence: string;
}

export interface Judgement {
  items: JudgedItem[];
  mustNot: JudgedMustNot[];
  summary: string;
}

export interface Score {
  /** 0..100, rounded. */
  score: number;
  band: Band;
  /** True when a mustNot pulled a would-be pass down to review. */
  capped: boolean;
  violated: string[];
  /** Failing items, heaviest first — the report's "top failing items". */
  failing: { id: string; weight: number; symptom: string }[];
}

export function bandFor(score: number, mustNotViolated: boolean): { band: Band; capped: boolean } {
  const raw: Band = score >= BANDS.pass ? "pass" : score >= BANDS.review ? "review" : "fail";
  if (mustNotViolated && raw === "pass") return { band: "review", capped: true };
  return { band: raw, capped: false };
}

export function scoreAttempt(
  items: Item[],
  judgement: Judgement,
  walk: Map<string, WalkVerdict>,
): Score {
  const judged = new Map(judgement.items.map((entry) => [entry.id, entry]));
  let total = 0;
  let passed = 0;
  const failing: Score["failing"] = [];
  for (const item of items) {
    total += item.weight;
    const verdict = judged.get(item.id);
    const blocked = walk.get(item.id) === "blocked";
    if (verdict?.verdict === "pass" && !blocked) {
      passed += item.weight;
      continue;
    }
    failing.push({
      id: item.id,
      weight: item.weight,
      symptom: verdict?.symptom || (blocked ? "blocked — the walker could not attempt it" : "not judged"),
    });
  }
  const score = total > 0 ? Math.round((passed / total) * 100) : 0;
  const violated = judgement.mustNot.filter((entry) => entry.violated).map((entry) => entry.id);
  const { band, capped } = bandFor(score, violated.length > 0);
  failing.sort((a, b) => b.weight - a.weight);
  return { score, band, capped, violated, failing };
}

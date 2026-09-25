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
 * Marks AE's OWN planned work in a related-issue search result.
 *
 * Planned work is an issue carrying both the `aep` arming switch (aep-api's
 * `delivery.LabelAgentWork`) and the `development` kind (`delivery.KindDevelopment`):
 * what the planner minted from the spec to describe what to BUILD. It is a
 * record of the spec — and that is exactly what makes it dangerous to hand an
 * agent unannotated, because it reads as evidence that the behaviour it
 * describes is intended and therefore fine.
 *
 * `aep` alone is NOT that signal. It only says a loop may work the issue: build
 * and deploy failures, validation repairs, merge conflicts and adopted incidents
 * all carry it too, and an adopted incident is a defect report — the opposite
 * of a spec. The kind is what says which one an issue is.
 *
 * That misreading is not hypothetical. An RCA whose remediation asked to "remove
 * the artificial delay in service2" was declined wholesale, because the search
 * surfaced AE's own issue titled "Implement service2 slow backend" and the
 * deliberate delay it described was taken as grounds for filing nothing. The
 * incident was dropped: nothing retries a handoff.
 *
 * The annotation lives HERE, on the search result, rather than in the prompt or
 * skill that reads it. Prose asking a model to keep the distinction in mind did
 * not survive contact with that issue title; a field on the record it is
 * actually reading is at the point of use.
 */

/**
 * The label AE stamps on work it armed for its own coding agent. Kept in step
 * with `delivery.LabelAgentWork` in aep-api — one string, two runtimes.
 */
export const PLATFORM_WORK_LABEL = "aep";

/**
 * The kind of planned work. Kept in step with `delivery.KindDevelopment`, and
 * the kinds that outrank it with aep-api's `kindPrecedence`: a hand-stamped
 * second kind wins over `development` there, so it must here too, or a defect
 * someone also tagged `development` would be read as a spec.
 */
export const PLATFORM_PLAN_KIND = "development";
const KINDS_OUTRANKING_PLAN = ["provision", "validation", "conflict", "bug"];

/** What a `PlatformRecord` issue is, and what it is not, said in the record. */
export const PLATFORM_ISSUE_NOTE =
  "PLATFORM IMPLEMENTATION RECORD — this issue records AE's original plan for what " +
  "to BUILD. It is not a defect report and it is never grounds for ruling out a code " +
  "change: behaviour can be deliberate and still be worth hardening.";

function isPlatformPlan(labels: unknown): boolean {
  if (!Array.isArray(labels)) return false;
  const names = new Set(
    labels.filter((label): label is string => typeof label === "string").map((label) => label.trim().toLowerCase()),
  );
  return (
    names.has(PLATFORM_WORK_LABEL) &&
    names.has(PLATFORM_PLAN_KIND) &&
    !KINDS_OUTRANKING_PLAN.some((kind) => names.has(kind))
  );
}

/**
 * Returns the issues with AE's own planned work marked. Pure, and it copies
 * rather than mutates so a caller's records are never rewritten underneath it.
 *
 * Anything that is not an object, or carries no recognisable label array, passes
 * through untouched: the annotation is a help, and mangling an unexpected record
 * would cost the caller its search result to gain a hint. `Labels` is aep-api's
 * casing; `labels` is accepted too so a shape change upstream degrades to no
 * annotation rather than to silence.
 */
export function annotatePlatformIssues<T>(issues: readonly T[]): T[] {
  return issues.map((issue) => {
    if (issue === null || typeof issue !== "object") return issue;
    const record = issue as Record<string, unknown>;
    if (!isPlatformPlan(record["Labels"] ?? record["labels"])) return issue;
    return { ...record, PlatformRecord: true, ReadAs: PLATFORM_ISSUE_NOTE } as T;
  });
}

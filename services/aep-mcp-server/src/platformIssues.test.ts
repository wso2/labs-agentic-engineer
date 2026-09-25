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
 * The incident behind every test here: a search surfaced AE's own issue
 * "Implement service2 slow backend", the handoff read the deliberate delay it
 * described as grounds for filing nothing, and the RCA was dropped. What has to
 * hold is that the marking is on the RECORD, that it never eats a search result
 * to add a hint, and that it never touches the caller's own objects.
 */

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  annotatePlatformIssues,
  PLATFORM_ISSUE_NOTE,
  PLATFORM_PLAN_KIND,
  PLATFORM_WORK_LABEL,
} from "./platformIssues.js";

const platformIssue = { Number: 4, Title: "Implement service2 slow backend", Labels: ["aep", "development"] };
const bugIssue = { Number: 9, Title: "service1 times out", Labels: ["bug", "incident"] };

test("AE's own planned work is marked as the spec record it is", () => {
  const [annotated] = annotatePlatformIssues([platformIssue]) as Record<string, unknown>[];

  assert.equal(annotated?.["PlatformRecord"], true);
  assert.equal(annotated?.["ReadAs"], PLATFORM_ISSUE_NOTE);
  // The note has to say the thing the model got wrong, not merely that the
  // issue is AE's — "it is deliberate" was the whole basis of the bad decline.
  assert.match(PLATFORM_ISSUE_NOTE, /never grounds for ruling out a code change/);
  // A record of the original plan, not an instruction: telling the agent to
  // keep the planned behaviour would contradict the sentence above whenever
  // the incident calls for changing it.
  assert.match(PLATFORM_ISSUE_NOTE, /original plan/);
  assert.doesNotMatch(PLATFORM_ISSUE_NOTE, /preserv/i);
  assert.equal(annotated?.["Title"], platformIssue.Title, "the record must survive intact");
});

test("an ordinary issue is left exactly as it came", () => {
  const [annotated] = annotatePlatformIssues([bugIssue]) as Record<string, unknown>[];

  assert.equal(annotated?.["PlatformRecord"], undefined);
  assert.deepEqual(annotated, bugIssue);
});

test("the caller's records are never rewritten underneath it", () => {
  const original = { ...platformIssue };
  annotatePlatformIssues([platformIssue]);

  assert.deepEqual(platformIssue, original, "annotation must copy, not mutate");
});

test("label matching ignores case and padding", () => {
  const issues = annotatePlatformIssues([
    { Number: 1, Labels: [" AEP ", "development"] },
    { Number: 2, Labels: ["Aep", " Development "] },
  ]) as Record<string, unknown>[];

  assert.ok(issues.every((i) => i["PlatformRecord"] === true));
});

test("a label that merely contains the word is not the arming label", () => {
  const [annotated] = annotatePlatformIssues([
    { Number: 1, Labels: ["aep-docs", "needs-aep", "development"] },
  ]) as Record<string, unknown>[];

  assert.equal(annotated?.["PlatformRecord"], undefined);
});

test("armed work that is not planned work is not marked as a plan", () => {
  // `aep` is only the arming switch: an adopted incident, a red build and a
  // merge conflict all carry it, and each is a defect or an obstruction, never
  // a spec. Marking one as a plan would tell the model the defect is intended.
  const issues = annotatePlatformIssues([
    { Number: 1, Title: "service1 times out", Labels: ["aep", "bug", "incident"] },
    { Number: 2, Title: "adopted by a human", Labels: ["aep"] },
    { Number: 3, Labels: ["aep", "bug", "src/build"] },
    { Number: 4, Labels: ["aep", "conflict"] },
    { Number: 5, Labels: ["aep", "validation"] },
  ]) as Record<string, unknown>[];

  assert.ok(issues.every((i) => i["PlatformRecord"] === undefined));
});

test("planned work nobody armed is not marked", () => {
  const [annotated] = annotatePlatformIssues([
    { Number: 1, Labels: [PLATFORM_PLAN_KIND] },
  ]) as Record<string, unknown>[];

  assert.equal(annotated?.["PlatformRecord"], undefined);
});

test("a second kind outranks development, as it does in aep-api", () => {
  // aep-api resolves a hand-stamped multi-kind issue worst-consequence-first,
  // with development last: a defect someone also tagged `development` is a bug.
  const [annotated] = annotatePlatformIssues([
    { Number: 1, Labels: ["aep", "development", "bug"] },
  ]) as Record<string, unknown>[];

  assert.equal(annotated?.["PlatformRecord"], undefined);
});

test("aep-api's casing and a lowercase shape are both understood", () => {
  const issues = annotatePlatformIssues([
    { Number: 1, Labels: [PLATFORM_WORK_LABEL, PLATFORM_PLAN_KIND] },
    { Number: 2, labels: [PLATFORM_WORK_LABEL, PLATFORM_PLAN_KIND] },
  ]) as Record<string, unknown>[];

  assert.ok(issues.every((i) => i["PlatformRecord"] === true));
});

test("a shape the annotation cannot read costs nothing", () => {
  // Losing a search result to gain a hint is the worse trade: the model can
  // still judge an unannotated issue, but it cannot judge one it never saw.
  const odd = [null, "not an issue", 7, { Number: 3 }, { Number: 4, Labels: "aep" }];

  assert.deepEqual(annotatePlatformIssues(odd), odd);
});

test("an empty result stays empty", () => {
  assert.deepEqual(annotatePlatformIssues([]), []);
});

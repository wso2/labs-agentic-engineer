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

import { describe, expect, it } from "vitest";
import { prdUnsettled } from "./prdUnsettled";

describe("prdUnsettled — what the rail reports about the requirements", () => {
  it("counts the agent's own judgments", () => {
    const md = [
      "## User Stories",
      "",
      "1. As a manager, I approve claims *assumed* single approver",
      "2. As finance, I export claims *assumed* monthly",
    ].join("\n");

    expect(prdUnsettled(md).assumptions).toBe(2);
  });

  it("counts the holes only the user can fill", () => {
    const md = ["## Open Questions", "", "- Which payroll vendor?", "- What is the SLA?"].join("\n");

    expect(prdUnsettled(md).openQuestions).toBe(2);
  });

  // They are different things and the user does different work on each, so the
  // rail reports them apart.
  it("keeps the two apart", () => {
    const md = [
      "## User Stories",
      "",
      "1. As a manager, I approve claims *assumed* single approver",
      "",
      "## Open Questions",
      "",
      "- Which payroll vendor?",
    ].join("\n");

    expect(prdUnsettled(md)).toEqual({ assumptions: 1, openQuestions: 1 });
  });

  // `deferred` is the user's own "stop asking me about this". Surfacing it as
  // something to resolve would undo the answer they already gave.
  it("does not count what the user has deferred", () => {
    const md = ["## Open Questions", "", "- Which payroll vendor? *deferred*"].join("\n");

    expect(prdUnsettled(md).assumptions).toBe(0);
    // The assumption count stays zero either way, so without this the case had
    // no purchase on the thing it exists to check.
    expect(prdUnsettled(md).openQuestions).toBe(0);
  });

  it("a settled document reports nothing", () => {
    const md = ["## User Stories", "", "1. As a manager, I approve claims"].join("\n");

    expect(prdUnsettled(md)).toEqual({ assumptions: 0, openQuestions: 0 });
  });

  // The rail is a read-out. Failing to render the workspace because one
  // document will not parse costs the user far more than an uncounted flag.
  it("survives nothing to read", () => {
    expect(prdUnsettled(undefined)).toEqual({ assumptions: 0, openQuestions: 0 });
    expect(prdUnsettled("")).toEqual({ assumptions: 0, openQuestions: 0 });
    expect(prdUnsettled("   ")).toEqual({ assumptions: 0, openQuestions: 0 });
  });
});

// Regression: the live agent writes `*(assumed)*`, and an exact `assumed`
// match dropped it — the rail stopped counting the assumption and, worse, the
// line lost its Settle control, so a judgment the agent had made was neither
// visible nor challengeable.
describe("the punctuation agents wrap the tag in", () => {
  const decision = (tag: string) =>
    ["## Product Decisions", "", `- Claims use a single, organization-wide currency ${tag}`].join(
      "\n",
    );

  it.each(["*assumed*", "*(assumed)*", "*[assumed]*", "*Assumed*"])("counts %s", (tag) => {
    expect(prdUnsettled(decision(tag)).assumptions).toBe(1);
  });

  // The whole run, not a substring: this is the agent emphasising its
  // reasoning, not tagging a judgment.
  it("leaves emphasised prose that merely contains the word alone", () => {
    expect(prdUnsettled(decision("*assumed single approver*")).assumptions).toBe(0);
  });
});

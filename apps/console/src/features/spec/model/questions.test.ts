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
import { markdownToNode } from "@aep/collab-doc";
import { docLines } from "../collab/docLines";
import { blockingEntries, blockingQuestions } from "./questions";

// Read through the same pipeline the room uses (markdown → document → lines),
// so the nesting an option's depth comes from is the real one.

const linesOf = (markdown: string) => docLines(markdownToNode(markdown));

const XERO = "Does finance post to one Xero organisation, or one per country?";

describe("blockingQuestions", () => {
  it("reads an Open Questions entry tagged blocking, with its nested bullets as the options", () => {
    const md = `# Payroll export

## Open Questions

1. ${XERO} *blocking*
   - One organisation for every claim.
   - One per country; a claim goes to the employee's country.
2. Which currency do refunds use?
`;
    expect(blockingQuestions(linesOf(md))).toEqual([
      {
        question: XERO,
        options: ["One organisation for every claim.", "One per country; a claim goes to the employee's country."],
      },
    ]);
  });

  it("reads a tag without options as a question answered in the user's own words", () => {
    expect(blockingQuestions(linesOf(`## Open Questions\n\n1. ${XERO} *blocking*\n2. Refund currency? *blocking*\n`))).toEqual([
      { question: XERO, options: [] },
      { question: "Refund currency?", options: [] },
    ]);
  });

  it("reads options without a tag as an ordinary open question, which blocks nothing", () => {
    expect(blockingQuestions(linesOf(`## Open Questions\n\n1. ${XERO}\n   - One organisation.\n   - One per country.\n`))).toEqual([]);
  });

  it("does not take a sibling after the options, or a deeper bullet, as an option", () => {
    const md = `## Open Questions

1. ${XERO} *blocking*
   - One organisation.
     - Named in the finance handbook.
   - One per country.
2. Refund currency?
`;
    expect(blockingQuestions(linesOf(md))).toEqual([{ question: XERO, options: ["One organisation.", "One per country."] }]);
  });

  it("reads the tag only in Open Questions, closing the entry", () => {
    const md = `## Decisions

- Claims go to Xero. *blocking*

## Open Questions

1. Is *blocking* here only a word?
`;
    expect(blockingQuestions(linesOf(md))).toEqual([]);
  });


  it("keeps the entry's line, whose range covers its options", () => {
    const [entry] = blockingEntries(linesOf(`## Open Questions\n\n1. ${XERO} *blocking*\n   - One organisation.\n`));
    expect(entry!.line.text).toBe(`${XERO} blocking`);
    expect(entry!.line.lineTo - entry!.line.lineFrom).toBeGreaterThan(entry!.line.to - entry!.line.from);
  });
});

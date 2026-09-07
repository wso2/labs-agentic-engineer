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
import type { components } from "../../../generated/aep-api";
import { latestComment, statusLine } from "./statusLine";

type IssueComment = components["schemas"]["IssueComment"];

const comment = (body: string, id = "c1"): IssueComment => ({
  id,
  author: "aep-bot",
  body,
  createdAt: "2026-09-04T10:00:00Z",
  url: "https://github.com/acme-dev/demo-shop/issues/121#issuecomment-1",
});

describe("latestComment", () => {
  it("is the last element, and undefined when the field is absent", () => {
    // The contract never sends an empty array — absence covers every empty case.
    expect(latestComment({ comments: [comment("a", "c1"), comment("b", "c2")] })?.id).toBe("c2");
    expect(latestComment({})).toBeUndefined();
  });
});

describe("statusLine", () => {
  it("is the agent's own words", () => {
    expect(statusLine({ comments: [comment("Authoring the last three specs.")] })).toBe(
      "Authoring the last three specs.",
    );
  });

  it("takes the NEWEST comment — the thread arrives oldest first", () => {
    // This is the whole reason the line is durable: a reader arriving late gets
    // the current answer, not the opening one.
    expect(
      statusLine({
        comments: [
          comment("Starting validation: 12 criteria, 9 to author.", "c1"),
          comment("Healing AC-004-b.", "c2"),
        ],
      }),
    ).toBe("Healing AC-004-b.");
  });

  it("flattens a markdown body to its first real line", () => {
    // A comment body is markdown over an unbounded textarea; every consumer
    // renders one line. Leading blank lines must not render as an empty note.
    expect(
      statusLine({ comments: [comment("\n\n  Rebased onto main\n\nthen re-ran the suite")] }),
    ).toBe("Rebased onto main");
  });

  it("is null when the newest body is entirely whitespace", () => {
    // Null, not "" — a caller falls back to something it knows rather than
    // rendering a blank where a sentence belongs.
    expect(statusLine({ comments: [comment("   \n  ")] })).toBeNull();
  });

  it("is null when there are no comments at all", () => {
    expect(statusLine({})).toBeNull();
  });
});

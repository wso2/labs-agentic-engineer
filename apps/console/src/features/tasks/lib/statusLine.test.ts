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
import { latestComment, statusLine, statusLineText } from "./statusLine";

type IssueComment = components["schemas"]["IssueComment"];

const comment = (body: string, id = "c1"): IssueComment => ({
  id,
  author: "aep-bot",
  body,
  createdAt: "2026-09-04T10:00:00Z",
  url: "https://github.com/acme-dev/demo-shop/issues/121#issuecomment-1",
});

const observed = (body: string, id = "o1"): IssueComment => ({ ...comment(body, id), observed: true });

describe("latestComment", () => {
  it("is the last element, and undefined when the field is absent", () => {
    // The contract never sends an empty array — absence covers every empty case.
    expect(latestComment({ comments: [comment("a", "c1"), comment("b", "c2")] })?.id).toBe("c2");
    expect(latestComment({})).toBeUndefined();
  });
});

describe("statusLine", () => {
  it("is the newest comment's claim, attributed to whoever made it", () => {
    expect(statusLine({ comments: [comment("Authoring the last three specs.")] })).toEqual({
      text: "Authoring the last three specs.",
      writer: "agent",
    });
  });

  it("reports the platform as the writer when it observed the line", () => {
    // Most lines on a validation issue are this: the platform posting what it
    // watched the run's own tool calls do, because a run asked to narrate its
    // progress did not.
    expect(
      statusLine({
        comments: [observed("Running automated tests against the deployed system…")],
      }),
    ).toEqual({ text: "Running automated tests against the deployed system…", writer: "platform" });
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
      })?.text,
    ).toBe("Healing AC-004-b.");
  });

  it("lets the platform's line supersede the agent's, and the agent's supersede that", () => {
    // Newest wins, whoever wrote it. An agent-first rule would be worse than it
    // sounds: the agent's FIRST act is an opening comment, so it would pin
    // "Starting validation…" over the whole run and hide every line after it.
    const opening = comment("Starting validation: 12 criteria, 9 to author.", "c1");
    const ladder = observed("Running automated tests against the deployed system…", "c2");
    const blocker = comment("AC-001-b blocked: the roles gate published no second login.", "c3");

    expect(statusLine({ comments: [opening, ladder] })).toEqual({
      text: "Running automated tests against the deployed system…",
      writer: "platform",
    });
    expect(statusLine({ comments: [opening, ladder, blocker] })).toEqual({
      text: "AC-001-b blocked: the roles gate published no second login.",
      writer: "agent",
    });
  });

  it("flattens a markdown body to its first real line", () => {
    // A comment body is markdown over an unbounded textarea; every consumer
    // renders one line. Leading blank lines must not render as an empty note.
    expect(
      statusLine({ comments: [comment("\n\n  Rebased onto main\n\nthen re-ran the suite")] })
        ?.text,
    ).toBe("Rebased onto main");
  });

  it("statusLineText is the claim without the attribution", () => {
    // What the Builds rows take: eleven tasks with one line each have no room
    // for a second signal per row.
    expect(statusLineText({ comments: [observed("Setting up the test harness…")] })).toBe(
      "Setting up the test harness…",
    );
    expect(statusLineText({ comments: [] })).toBeNull();
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

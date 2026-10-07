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
import {
  alertItems,
  codingAgentTookOn,
  issueOrigin,
  issueSections,
  issueStateLabel,
  issueText,
  needsPersonCount,
  type IssueInfo,
} from "./issues";

type RcaAgentReport = components["schemas"]["RcaAgentReport"];

const issue = (n: number, over: Partial<IssueInfo> = {}): IssueInfo => ({
  Number: n,
  Title: `Issue ${n}`,
  Body: "",
  URL: `https://github.com/acme/app/issues/${n}`,
  State: "open",
  Labels: [],
  ...over,
});

const report = (over: Partial<RcaAgentReport> = {}): RcaAgentReport => ({
  id: "r1",
  project: "acme",
  title: "Latency spike",
  summary: "The expense API's pool ran dry.",
  diagnosis: "",
  classification: "config-level",
  dispatched: false,
  deployed: false,
  createdAt: "2026-10-01T10:00:00Z",
  ...over,
});

describe("issueOrigin", () => {
  it("names the SRE agent for an incident, by either label", () => {
    expect(issueOrigin(["incident", "aep"])).toEqual({ kind: "incident", by: "the SRE agent, from an incident" });
    expect(issueOrigin(["sre-agent"]).kind).toBe("incident");
  });

  it("names what the platform filed it for, by its source, then its kind", () => {
    expect(issueOrigin(["aep", "bug", "src/build"])).toEqual({ kind: "platform", by: "the platform, from a failed build" });
    expect(issueOrigin(["aep", "development"])).toEqual({ kind: "platform", by: "the planner, from the spec" });
  });

  it("reads a bug with no source, and an issue with none of the platform's labels, as a person's", () => {
    expect(issueOrigin(["bug"])).toEqual({ kind: "person", by: "a person" });
    expect(issueOrigin(["src/user", "bug"])).toEqual({ kind: "person", by: "a person" });
    expect(issueOrigin(null)).toEqual({ kind: "person", by: "someone on GitHub" });
  });
});

describe("codingAgentTookOn", () => {
  it("reads the arming label", () => {
    expect(codingAgentTookOn(issue(1, { Labels: ["incident", "aep"] }))).toBe(true);
    expect(codingAgentTookOn(issue(1, { Labels: ["aep:halted"] }))).toBe(false);
  });
});

describe("issueStateLabel", () => {
  it("says how a closed issue closed, and that an open one was reopened", () => {
    expect(issueStateLabel({ State: "closed", StateReason: "not_planned" })).toBe("Closed as not planned");
    expect(issueStateLabel({ State: "closed", StateReason: "completed" })).toBe("Closed");
    expect(issueStateLabel({ State: "open", StateReason: "reopened" })).toBe("Open, reopened");
    expect(issueStateLabel({ State: "open" })).toBe("Open");
  });
});

describe("issueText", () => {
  it("drops the platform's comment markers and the gaps they leave", () => {
    expect(issueText("Pool ran dry.\n\n## Recurrence 1\n\n<!-- aep:recurrence-closure:ab12 -->\n\nAgain.")).toBe(
      "Pool ran dry.\n\n## Recurrence 1\n\nAgain.",
    );
  });
});

describe("issueSections", () => {
  it("puts what needs attention first, a person's before a verdict, then open, then closed", () => {
    const verdict = issue(5, { State: "closed", attentionReason: "no_change_verdict" });
    const review = issue(4, { attentionReason: "unverified_fix" });
    const escalated = issue(3, { attentionReason: "escalated" });
    const open = issue(2);
    const closed = issue(1, { State: "closed" });
    expect(issueSections([verdict, review, escalated, open, closed])).toEqual({
      attention: [escalated, review, verdict],
      open: [open],
      closed: [closed],
    });
  });
});

describe("alertItems", () => {
  it("lists what needs a person first, across projects, then what only asks to be read", () => {
    const issues = new Map([
      ["billing", [issue(7, { State: "closed", attentionReason: "no_change_verdict" }), issue(9, { attentionReason: "unverified_fix" })]],
      ["acme", [issue(2, { attentionReason: "unverified_fix" }), issue(3, { attentionReason: "escalated" }), issue(4)]],
    ]);
    expect(alertItems(issues, []).map((a) => [a.key, a.needsPerson])).toEqual([
      ["acme#3", true],
      ["acme#2", true],
      ["billing#9", true],
      ["billing#7", false],
    ]);
  });

  it("adds a report whose issue is not listed, and leaves one whose issue is to its issue", () => {
    const issues = new Map([["acme", [issue(3, { attentionReason: "escalated" })]]]);
    const items = alertItems(issues, [report({ id: "r1", issueNumber: 3 }), report({ id: "r2", issueNumber: 8, issueTitle: "Pool exhausted" })]);
    expect(items.map((a) => a.key)).toEqual(["acme#3", "acme#8"]);
    expect(items[1]).toMatchObject({ issueNumber: 8, title: "Pool exhausted", reason: null, needsPerson: true });
  });

  it("asks a person about a report only while nothing was dispatched at a change it calls for", () => {
    const items = alertItems(new Map(), [
      report({ id: "dispatched", dispatched: true, createdAt: "2026-10-02T10:00:00Z" }),
      report({ id: "nothing", classification: "none", createdAt: "2026-10-03T10:00:00Z" }),
      report({ id: "waiting" }),
    ]);
    expect(items.map((a) => [a.key, a.needsPerson])).toEqual([
      ["report:waiting", true],
      ["report:nothing", false],
      ["report:dispatched", false],
    ]);
    expect(needsPersonCount(items)).toBe(1);
  });
});

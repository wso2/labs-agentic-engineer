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

// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import type React from "react";
import { describe, expect, it, vi } from "vitest";

import type { components } from "../../../generated/aep-api";

type TaskView = components["schemas"]["TaskView"];

// The row links to a task route; the router is not what these assert.
vi.mock("@tanstack/react-router", () => ({
  createLink:
    (Component: React.ElementType) =>
    ({
      children,
      ...rest
    }: {
      to?: string;
      params?: Record<string, string>;
      children?: React.ReactNode;
    }) => <Component {...rest}>{children}</Component>,
}));

const { IssueChips } = await import("./IssueChips");

function issue(issueNumber: number, kind: string | undefined, title = "a task"): TaskView {
  return {
    issueNumber,
    title,
    kind,
    derivedStatus: "pending",
    executorClass: "coding",
  } as TaskView;
}

describe("IssueChips", () => {
  // The KIND is what tells a reader the version picked up work it did not plan.
  // The field rode the API for a release with nothing rendering it, so the data
  // was present and the row said nothing — which is the gap this closes.
  it("tags a defect, a conflict and a gate", () => {
    render(
      <IssueChips
        projectName="widgets"
        issues={[issue(1, "bug"), issue(2, "conflict"), issue(3, "provision")]}
      />,
    );
    expect(screen.getByText("Defect")).toBeTruthy();
    expect(screen.getByText("Conflict")).toBeTruthy();
    expect(screen.getByText("Provisioning")).toBeTruthy();
  });

  // Planned work is the majority of any version's list, so tagging it would be
  // noise: the UNTAGGED row is what says "this is what the version set out to do".
  it("leaves planned work untagged", () => {
    render(<IssueChips projectName="widgets" issues={[issue(4, "development")]} />);
    expect(screen.queryByText("Defect")).toBeNull();
    expect(screen.queryByText("Development")).toBeNull();
  });

  // A kind this console has not learned renders untagged rather than guessing a
  // label — that is what lets the platform add one without a console release.
  it("renders an unknown kind, and a missing one, untagged", () => {
    render(
      <IssueChips projectName="widgets" issues={[issue(5, "somethingNew"), issue(6, undefined)]} />,
    );
    expect(screen.queryByText("somethingNew")).toBeNull();
    // Both rows still render their own state chip.
    expect(screen.getAllByText("Open").length).toBe(2);
  });
});

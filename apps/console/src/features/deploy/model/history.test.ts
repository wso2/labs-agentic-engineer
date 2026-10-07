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
import { deployHistory } from "./history";
import type { EnvironmentColumn } from "./pipeline";

type BuildSummary = components["schemas"]["BuildSummary"];

const build = (tag: string, status: BuildSummary["status"], completedAt?: string): BuildSummary => ({
  tag,
  status,
  milestoneNumber: 1,
  startedAt: "2026-10-01T09:00:00Z",
  ...(completedAt ? { completedAt } : {}),
});

const column = (over: Partial<EnvironmentColumn>): EnvironmentColumn => ({
  name: "development",
  label: "Development",
  entry: true,
  version: "v2",
  running: true,
  state: { label: "Running", tone: "success" },
  components: [],
  dependencies: [],
  next: null,
  ...over,
});

describe("deployHistory", () => {
  it("lists every completed build as deployed to the first environment, newest first, marking what runs now", () => {
    const rows = deployHistory(
      [column({})],
      [build("v1", "completed", "2026-10-02T16:05:00Z"), build("v2", "completed", "2026-10-03T10:42:00Z"), build("v3", "failed")],
    );
    expect(rows.map((r) => [r.version, r.environment, r.current, r.at?.of])).toEqual([
      ["v2", "Development", true, "build"],
      ["v1", "Development", false, "build"],
    ]);
  });

  it("adds what a later environment runs now, dated by its deployment", () => {
    const staging = column({
      name: "staging",
      label: "Staging",
      entry: false,
      version: null,
      components: [
        {
          name: "expense-api",
          displayName: "expense-api",
          type: "service",
          kind: "ready",
          url: null,
          deployment: { createdAt: "2026-10-04T08:00:00Z" },
        },
      ],
    });
    const rows = deployHistory([column({}), staging], [build("v2", "completed", "2026-10-03T10:42:00Z")]);
    expect(rows[0]).toMatchObject({ environment: "Staging", version: null, current: true, at: { iso: "2026-10-04T08:00:00Z", of: "deploy" } });
  });
});

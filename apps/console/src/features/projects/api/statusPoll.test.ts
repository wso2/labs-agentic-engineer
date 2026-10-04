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
import { statusIsMoving } from "./queries";

type ProjectStatus = components["schemas"]["ProjectStatus"];

function status(spec: Partial<ProjectStatus["spec"]>): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    spec: { exists: true, version: "v1", dirty: false, design: false, agent: "", availability: "available", ...spec },
    build: { version: "", status: "idle" },
    deploy: { version: "", status: "none", components: { total: 0, ready: 0 }, validation: "none" },
  };
}

describe("statusIsMoving", () => {
  // A running turn is read from the AE Studio pod (useActiveTurn), not from
  // the status aggregate: `spec.agent` keeps only never-started | "" | failed.
  it("no longer reads spec.agent === 'working'", () => {
    expect(statusIsMoving(status({ agent: "working" }))).toBe(false);
  });

  it("stays fast mid-interview: a turn has run and nothing is written yet", () => {
    expect(statusIsMoving(status({ agent: "", exists: false }))).toBe(true);
    expect(statusIsMoving(status({ agent: "never-started", exists: false }))).toBe(false);
  });
});

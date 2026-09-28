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
import { formatEvent } from "@aep/progress-view";
import type { components } from "../../generated/aep-api";
import { runCycleEvents } from "./run-progress";

type RunCycleView = components["schemas"]["RunCycleView"];

const merged: RunCycleView = {
  id: "cycle-oc",
  kind: "coding",
  attempts: 1,
  branch: "aep/m1-c1",
  prNumber: 3,
  mergeSha: "dcb1edc5fe0417b2",
  createdAt: "2026-09-22T09:14:00Z",
  endedAt: "2026-09-22T09:41:00Z",
};

/**
 * The OpenCode-flavoured feed exists so the Builds rail is walked on the second
 * runtime's spellings. These pin that it actually carries them, and that the
 * renderer reads them as the same rows a Claude Code run gets.
 */
describe("runCycleEvents on OpenCode", () => {
  const events = runCycleEvents(merged, 0, "opencode");

  it("announces the runtime and settles once", () => {
    expect(events[0]).toMatchObject({ kind: "run_started", runtime: "opencode" });
    expect(events.filter((e) => e.kind === "run_settled")).toHaveLength(1);
  });

  it("fans out in the foreground, as role general", () => {
    const spawned = events.filter((e) => e.kind === "agent_started" && e.agentId !== "lead");
    expect(spawned.length).toBeGreaterThan(1);
    for (const agent of spawned) {
      expect(agent.background).toBe(false);
      expect(agent.role).toBe("general");
    }
    // No background task pair: OpenCode's wave is parallel foreground calls.
    expect(events.some((e) => e.kind === "task_started")).toBe(false);
  });

  it("renders OpenCode's lower-case bash as a shell row", () => {
    const shell = events.filter((e) => e.kind === "tool_use" && e.tool === "bash");
    expect(shell.length).toBeGreaterThan(0);
    for (const e of shell) {
      expect(formatEvent(e).text).toBe(`$ ${e.summary ?? ""}`);
    }
  });

  it("is only the coding feed: a validation cycle keeps its own story", () => {
    const validation = runCycleEvents({ ...merged, kind: "validation" }, 0, "opencode");
    expect(validation[0]).toMatchObject({ kind: "run_started", taskKind: "validation" });
  });

  it("defaults to the Claude Code feed", () => {
    expect(runCycleEvents(merged, 0)[0]).toMatchObject({ runtime: "claude-code" });
  });
});

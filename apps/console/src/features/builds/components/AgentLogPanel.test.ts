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
import { agentLogEmptyNote } from "./AgentLogLines";

describe("agentLogEmptyNote", () => {
  it("names archive attach distinctly from a live agent that has not spoken", () => {
    expect(agentLogEmptyNote("connecting")).toMatch(/Loading agent output/);
    expect(agentLogEmptyNote("live", { agentRunning: true })).toMatch(/Waiting for the agent's first line/);
    expect(agentLogEmptyNote("live", { agentRunning: false })).toMatch(/Loading agent output/);
  });

  it("settles empty finished runs without looking like a hang", () => {
    expect(agentLogEmptyNote("ended")).toMatch(/No output was recorded/);
    expect(agentLogEmptyNote("reconnecting")).toMatch(/Reconnecting/);
  });
});

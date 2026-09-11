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

/**
 * This run's short name for each agent — `#1`, `#2` — minted on first sight.
 *
 * Its own module because TWO surfaces print it: the streaming step lines tag
 * every non-lead row `[#2]`, and the crew block names the same agent `#2` on its
 * own row. A reader crosses between them constantly ("that failed line — which
 * crew member was that?"), so the two must agree, and the only way to guarantee
 * that is ONE registry handed to both rather than two that mint in the same
 * order by coincidence.
 *
 * A NUMBER rather than the agent's label: labels run to a full sentence
 * ("Implement todo-api Ballerina service (issue #3)") and would push every
 * streamed line off the right edge. The label is announced once, by the agent's
 * own `agent_started` row, and stands on its own row in the block.
 */

import { LEAD_AGENT_ID } from "@aep/progress-view";

/** An agent's short name, or "" for the lead — which is never tagged. */
export type AgentTags = (agentId: string) => string;

/**
 * A fresh registry for ONE run.
 *
 * The lead is deliberately untagged: it is the overwhelming majority of a run's
 * rows, and an unstamped row reading as "the lead" is what keeps the feed quiet.
 */
export function createAgentTags(): AgentTags {
  const minted = new Map<string, string>();
  return (agentId: string): string => {
    if (agentId === LEAD_AGENT_ID) return "";
    const held = minted.get(agentId);
    if (held) return held;
    const fresh = `#${String(minted.size + 1)}`;
    minted.set(agentId, fresh);
    return fresh;
  };
}

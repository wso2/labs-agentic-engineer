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

// The shared row vocabulary — what BOTH envelope versions render into. Only what
// a SURFACE needs: the package's own building blocks (byte and token wording, a
// status's tone, an opening report) stay behind their modules, so the entry
// point stays the list of things a renderer actually calls.
export {
  formatDuration,
  formatOutcome,
  SLOW_CALL_MS,
  type CallOutcome,
  type FormattedLine,
  type LineTone,
  type OutcomeView,
} from "./line.js";

// One agent's report, the wording of a section header.
export {
  formatAgentReport,
  formatAgentStatus,
  LEAD_AGENT_ID,
  type AgentReport,
} from "./agent.js";

// v2 (RunEvent) — the run feed.
export {
  formatEvent,
  formatHeartbeat,
  isSilentKind,
  type RunEventView,
} from "./event.js";

// The crew — who is doing what, and whether the run is stuck. The shared
// meaning behind BOTH the console's crew view and its timeline: one model, so
// the tree and the lanes cannot disagree about which agent is alive.
export {
  buildCrew,
  crewStateLabel,
  crewTone,
  isCrewSettled,
  planTone,
  STALL_MS,
  type Crew,
  type CrewMember,
  type CrewPlanItem,
  type CrewState,
  type CrewTask,
} from "./crew.js";

// One agent's stretch of the axis. The span ALGEBRA stays behind the module —
// the crew is the only thing that computes lanes, and a surface only draws them.
export { type LaneSpan } from "./lane.js";

// v1 (RunProgressLine / TimelineEvent) — the task log, for the compatibility
// window while v1 executions are still being read back.
export { formatLine, type ProgressLineView } from "./format.js";

export {
  groupByAgent,
  mergeOutcomes,
  type AgentSection,
  type EventRow,
  type MergedRow,
  type OutcomePairable,
} from "./group.js";

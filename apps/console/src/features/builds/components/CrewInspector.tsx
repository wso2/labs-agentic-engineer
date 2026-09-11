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

import { Box, Typography } from "@wso2/oxygen-ui";
import {
  crewStateLabel,
  crewTone,
  formatAgentStatus,
  isCrewSettled,
  type CrewMember,
} from "@aep/progress-view";
import { toneColor } from "../../../components/logTone";
import { AgentPlan } from "./AgentPlan";
import { AgentReportNote, AgentSteps } from "./AgentSteps";
import type { StampedRunEvent } from "../hooks/useRunProgress";

// WHAT ONE AGENT DID. The tree answers who is alive; this answers what the one
// you picked has been doing, and nothing else — no other agent's steps are
// interleaved into it, which is the whole reason the tree exists beside it.
//
// Everything here is on the feed already. There is no narration and no file
// list: an invented summary of an agent's work is exactly the thing a reader
// cannot check, and the agent's own closing report is right there.

/**
 * What this agent DISPATCHED, and what became of each one.
 *
 * The gap this closes: a spawn is filed under the agent it created, so it became
 * that agent's section header and left no trace in the feed of the agent that
 * ordered it. Reading the lead's own log, a run's whole fan-out was invisible —
 * three agents appeared from nowhere in the tree and the lead's steps jumped
 * from "read the design" to "open the pull request" with nine minutes and all
 * the actual work in between unaccounted for.
 *
 * Derived from `member.children`, not from a fabricated event: every field here
 * is one the runtime declared about the child. Deliberately NOT interleaved into
 * the steps below — the honest position for a spawn is where it happened, and
 * `steps` holds only this agent's own events. Putting `agent_started` in the
 * parent's rows instead would draw it twice on the flat feed, which renders each
 * child section with a header of its own.
 *
 * `running` on a row here is the answer to "why is the lead quiet": it is
 * waiting on this. The header above already says so in the runtime's words; this
 * says WHICH, with a way to go and look.
 */
function DispatchedCrew({
  children,
  onSelect,
}: {
  children: readonly CrewMember<StampedRunEvent>[];
  onSelect?: ((id: string) => void) | undefined;
}) {
  if (children.length === 0) return null;
  return (
    <Box sx={{ mb: 1 }}>
      <Typography component="div" sx={{ font: "inherit", color: "grey.500", mb: 0.25 }}>
        dispatched {children.length === 1 ? "1 agent" : `${String(children.length)} agents`}
      </Typography>
      {children.map((child) => {
        const tone = toneColor(crewTone(child.state));
        const live = !isCrewSettled(child.state);
        return (
          <Box
            key={child.id}
            component={onSelect ? "button" : "div"}
            type={onSelect ? "button" : undefined}
            onClick={onSelect ? () => { onSelect(child.id); } : undefined}
            // Deliberately does NOT open with the agent's name. The tree's rows
            // are named `<agent>: <state>…`, and a second control whose name
            // also began with the agent's name left a screen-reader user two
            // near-identical buttons per agent with nothing to tell them apart.
            // This one is an action, so it reads as one.
            aria-label={
              onSelect ? `Show ${child.agent.label}, ${crewStateLabel(child.state)}` : undefined
            }
            sx={{
              display: "flex",
              alignItems: "baseline",
              gap: 1,
              width: "100%",
              font: "inherit",
              textAlign: "left",
              background: "none",
              border: 0,
              p: 0,
              pl: 1,
              color: "inherit",
              cursor: onSelect ? "pointer" : "default",
              "&:hover": onSelect ? { color: "grey.100" } : undefined,
            }}
          >
            <Typography component="span" sx={{ font: "inherit", flexShrink: 0 }}>
              ⑂
            </Typography>
            <Typography
              component="span"
              title={child.agent.label}
              sx={{
                font: "inherit",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                minWidth: 0,
              }}
            >
              {child.agent.label}
            </Typography>
            {child.background && (
              <Typography component="span" sx={{ font: "inherit", color: "grey.500", flexShrink: 0 }}>
                background
              </Typography>
            )}
            <Typography
              component="span"
              sx={{ font: "inherit", color: tone, ml: "auto", flexShrink: 0 }}
            >
              {live ? crewStateLabel(child.state) : formatAgentStatus(child.agent)}
            </Typography>
          </Box>
        );
      })}
    </Box>
  );
}

export function CrewInspector({
  member,
  onSelect,
}: {
  member: CrewMember<StampedRunEvent>;
  onSelect?: ((id: string) => void) | undefined;
}) {
  const settled = isCrewSettled(member.state);
  return (
    <Box sx={{ minWidth: 0 }}>
      {/* Stacked, not one row. The totals run to "completed · 3m29s · 19 tools ·
          +553/−4 lines · 214.0k tokens" and the label to a whole sentence, and
          side by side in a narrow column the label was the half that lost —
          leaving a panel whose figures nobody could attribute. */}
      <Box sx={{ pb: 0.5, mb: 1, borderBottom: 1, borderColor: "grey.800" }}>
        <Typography
          component="div"
          title={member.agent.label}
          sx={{
            font: "inherit",
            color: "grey.200",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {member.agent.label}
        </Typography>
        {/* The runtime's OWN totals — duration, tool count, lines written. It
            measured the agent's whole life, including the parts that never
            reached this feed, so nothing here is re-derived from the rows below. */}
        <Typography
          component="div"
          sx={{
            font: "inherit",
            color: toneColor(crewTone(member.state)),
            wordBreak: "break-word",
          }}
        >
          {formatAgentStatus(member.agent)}
        </Typography>
      </Box>

      {/* What it is waiting on, in the runtime's words. One line, and only while
          it is still live: on a settled agent the same slot carries its report,
          which is a different kind of sentence and belongs below. */}
      {!settled && member.caption && (
        <Typography component="div" sx={{ font: "inherit", color: "grey.400", mb: 1 }}>
          {member.caption}
        </Typography>
      )}

      {/* The one thing no flat feed could show: the agent's own closing summary.
          Its transcript dies with the pod, so this is the only copy. */}
      {member.agent.report && (
        <Box sx={{ mb: 1 }}>
          <AgentReportNote report={member.agent.report} />
        </Box>
      )}

      {/* What it SET OUT to do, above what it did. The tree carries the same
          rows under this agent's row, and that repetition is deliberate — the
          same way a settled agent's report is both its tree sub-line and the
          note below. The tree answers "what is this run trying to do" at a
          glance across every agent; this answers "did the one I picked get
          there", and it is the ONLY place the plan appears on a cycle with a
          single agent, where there is no tree at all. */}
      <AgentPlan plan={member.plan} />

      <DispatchedCrew children={member.children} onSelect={onSelect} />

      <AgentSteps steps={member.steps} />
    </Box>
  );
}

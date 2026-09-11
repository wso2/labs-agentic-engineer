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

import { Box, List, ListItemButton, Typography } from "@wso2/oxygen-ui";
import {
  crewStateLabel,
  crewTone,
  formatDuration,
  isCrewSettled,
  type CrewMember,
  type CrewState,
} from "@aep/progress-view";
import { toneColor } from "../../../components/logTone";
import { PlanRow } from "./AgentPlan";
import type { StampedRunEvent } from "../hooks/useRunProgress";

// The tree holds AGENTS and nothing else: a name, a state, what the runtime says
// it is doing, and how long it has been going. It deliberately does NOT list an
// agent's commands.
//
// It used to. Every backgrounded shell command got a row of its own, and a live
// run produced 47 of them — so the column that answers "who is working" was
// mostly raw command lines, and the answer was buried in its own evidence. The
// commands did not go anywhere: the inspector beside this holds each agent's
// whole feed, which is where a reader goes once they have picked the agent this
// column is for. `CrewMember.tasks` is still computed and still true; nothing
// here renders it.

// WHO IS DOING WHAT RIGHT NOW. One row per agent, nested as the runtime declared
// the tree, each row carrying its own liveness rather than deferring it to a
// line somebody has to find in a log.
//
// Every row says the same four things in the same places: a state dot, the name,
// one live sub-line in the runtime's own words, and how long it has been going.
// A reader scanning down the column is scanning ONE fact at a time, which is what
// makes "is anything stuck" answerable at a glance instead of by reading.

/**
 * The state dot.
 *
 * Never the only signal — the state's word sits beside it on the sub-line, and
 * the row would read identically to somebody who cannot tell amber from blue.
 * The dot is for scanning; the word is for knowing.
 */
function StateDot({ state }: { state: CrewState }) {
  return (
    <Box
      component="span"
      aria-hidden
      sx={{
        width: (t) => t.spacing(1),
        height: (t) => t.spacing(1),
        borderRadius: "50%",
        flexShrink: 0,
        bgcolor: toneColor(crewTone(state)),
        // A settled agent's dot is a ring: the run is over there, and a solid
        // dot beside a live one reads as two things happening at once.
        ...(isCrewSettled(state)
          ? { bgcolor: "transparent", border: 2, borderColor: toneColor(crewTone(state)) }
          : {}),
      }}
    />
  );
}

function CrewRow({
  member,
  selected,
  onSelect,
}: {
  member: CrewMember<StampedRunEvent>;
  selected: boolean;
  onSelect: (id: string) => void;
}) {
  const tone = toneColor(crewTone(member.state));
  const live = !isCrewSettled(member.state);
  // "waiting · waiting on the model" says it twice. When the runtime's own
  // sentence already opens with the state, the sentence IS the state word — it
  // just carries the state's colour instead of being prefixed by it.
  const state = crewStateLabel(member.state);
  const captionLeads = member.caption.startsWith(state);
  return (
    <>
      <ListItemButton
        dense
        disableGutters
        selected={selected}
        onClick={() => {
          onSelect(member.id);
        }}
        // The accessible name is the whole row's meaning, in the order it is
        // drawn: a reader who cannot see the tree still hears which agent, how
        // it stands, and what it says it is doing.
        aria-label={`${member.agent.label}: ${crewStateLabel(member.state)}${member.caption ? `. ${member.caption}` : ""}`}
        sx={{
          display: "block",
          // Indented by the depth the runtime DECLARED, so a depth-2 agent sits
          // under the agent that spawned it however late it joined the feed.
          pl: 1 + member.depth * 2,
          pr: 1,
          py: 0.5,
          borderRadius: 1,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <StateDot state={member.state} />
          <Typography
            component="span"
            title={member.agent.label}
            sx={{
              font: "inherit",
              color: "grey.200",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              minWidth: 0,
            }}
          >
            {member.agent.label}
          </Typography>
          {/* Printed only when the runtime said TRUE, which is the ordinary case
              for a builder: fan-out is backgrounded by default and the skill is
              what decides its shape (ADR-0011). `false` means the parent is
              blocked inside this agent's call, and the row says so in words
              instead — so only the true case needs a chip. */}
          {member.background && (
            <Typography component="span" sx={{ font: "inherit", color: "grey.500", flexShrink: 0 }}>
              background
            </Typography>
          )}
          <Typography
            component="span"
            sx={{
              font: "inherit",
              color: "grey.500",
              ml: "auto",
              flexShrink: 0,
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {member.elapsedMs === undefined ? "" : formatDuration(member.elapsedMs)}
          </Typography>
        </Box>
        <Box sx={{ display: "flex", alignItems: "baseline", gap: 1, pl: 2 }}>
          {!captionLeads && (
            <Typography component="span" sx={{ font: "inherit", color: tone, flexShrink: 0 }}>
              {state}
            </Typography>
          )}
          {member.caption && (
            <Typography
              component="span"
              title={member.caption}
              sx={{
                font: "inherit",
                color: captionLeads ? tone : "grey.400",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                minWidth: 0,
              }}
            >
              {captionLeads ? member.caption : `· ${member.caption}`}
            </Typography>
          )}
          {/* The age, and the reason this surface owns a clock: it has to tick
              while NOTHING arrives. A frozen "3s ago" on a wedged run is the
              precise lie this view exists to stop telling. Shown only while the
              agent is live — on a settled one it would count how long ago the
              build was, which the page header already says. */}
          {live && member.silentForMs !== undefined && (
            <Typography
              component="span"
              sx={{
                font: "inherit",
                color: "grey.500",
                ml: "auto",
                flexShrink: 0,
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {formatDuration(member.silentForMs)} ago
            </Typography>
          )}
        </Box>
      </ListItemButton>
      {/* The agent's own plan, under the agent whose plan it is — including the
          entries a LEAD wrote and handed to this one, since "what was this one
          sent to do" is the question a reader has about its row. Shown on a
          settled agent too: the list is what it set out to do and whether it got
          there, which only becomes a record once the run is over. */}
      {member.plan.map((item) => (
        <PlanRow key={item.id} item={item} depth={member.depth} />
      ))}
    </>
  );
}

/**
 * The crew tree.
 *
 * Rendered from the model's already-flattened `members` — which is depth-first
 * from the lead — rather than by recursing the tree here. The nesting is drawn
 * by each row's own declared `depth`, so the selection is decided in ONE place:
 * a recursive render would have to thread it down every branch, and a branch
 * that forgot it is a tree with two rows highlighted.
 */
export function CrewTree({
  members,
  selectedId,
  onSelect,
}: {
  members: readonly CrewMember<StampedRunEvent>[];
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  return (
    <List dense disablePadding sx={{ font: "inherit" }}>
      {members.map((member) => (
        <CrewRow
          key={member.id}
          member={member}
          selected={member.id === selectedId}
          onSelect={onSelect}
        />
      ))}
    </List>
  );
}

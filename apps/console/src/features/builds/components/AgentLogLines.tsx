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

import { useState } from "react";
import { Box, Chip, Collapse, Typography } from "@wso2/oxygen-ui";
import { ChevronDown, ChevronRight } from "@wso2/oxygen-ui-icons-react";
import {
  formatAgentStatus,
  formatEvent,
  formatOutcome,
  groupByAgent,
  mergeOutcomes,
  type AgentReport,
  type AgentSection,
  type MergedRow,
} from "@aep/progress-view";
import { LogNote, LogSurface } from "../../../components/LogSection";
import { toneColor } from "../../../components/logTone";
import { AgentReportNote, LogRow, agentLogEmptyNote } from "./AgentSteps";
import {
  runEventKey,
  type RunProgressPhase,
  type StampedRunEvent,
} from "../hooks/useRunProgress";

// One cycle's agent output, in the v2 flat form: a row per action, one nested
// section per agent, and each settled agent's closing REPORT on its header.
//
// v1 could show none of that. It stamped a line `main` or `subagent` and
// inferred the rest, which carried exactly one level of fan-out and no report at
// all — an agent's own summary of what it did died with its pod. Here the tree
// is declared (`agent_started` names the agent, its parent and its depth) and
// the report is a field.

// The log surface moved to components/LogSection (ADR-0021): three surfaces now
// render logs and one definition keeps them identical. The ROW, the report note
// and the empty-state copy moved to AgentSteps, which the crew inspector renders
// from — one definition each, so the flat form and the crew cannot drift into
// drawing the same event two ways. Re-exported here so the feature's existing
// importers are untouched.
export { LogNote, LogSurface, agentLogEmptyNote };

/**
 * The name an agent's section carries. A run fans out to several agents at once
 * and their events arrive interleaved, so the chip is what says whose work this
 * stretch is — the label its parent gave it ("Implement todo-api service (issue
 * #3)"), or the runtime's id when the parent gave none.
 */
function AgentChip({ agent }: { agent: AgentReport }) {
  return (
    <Chip
      label={agent.label}
      size="small"
      variant="outlined"
      title={agent.label}
      sx={{
        height: 16,
        fontSize: "0.6875rem",
        color: "grey.400",
        borderColor: "grey.700",
        mr: 1,
        flexShrink: 0,
        maxWidth: 220,
      }}
    />
  );
}

/** Log surface with subtle loading / empty copy when the stream has no events. */
export function AgentLogPanel({
  events,
  phase,
  agentRunning = false,
  maxHeight = 420,
}: {
  events: StampedRunEvent[];
  phase: RunProgressPhase;
  agentRunning?: boolean;
  maxHeight?: number;
}) {
  return (
    <LogSurface maxHeight={maxHeight}>
      {events.length === 0 ? (
        <LogNote>{agentLogEmptyNote(phase, { agentRunning })}</LogNote>
      ) : (
        <AgentLogLines events={events} />
      )}
    </LogSurface>
  );
}

/** The rows of one agent's section, with the agents IT spawned nested in place. */
function SectionRows({ section }: { section: AgentSection<StampedRunEvent> }) {
  // The agent's own events are merged as ONE stream, because an action and its
  // outcome are routinely separated by a nested section that spoke in between.
  // Merging is then looked up per event rather than re-derived inside the walk,
  // so a nested section still renders at the point its agent first spoke — the
  // ordering is what makes a fan-out readable.
  const merged = new Map<StampedRunEvent, MergedRow<StampedRunEvent>>();
  for (const row of mergeOutcomes(
    section.rows.flatMap((r) => (r.kind === "event" ? [r.event] : [])),
  )) {
    merged.set(row.line, row);
  }

  return (
    <>
      {section.rows.map((row) => {
        if (row.kind === "section") {
          return <AgentSectionView key={`agent:${row.section.id}`} section={row.section} />;
        }
        // Absent from the map = folded into an earlier action's row.
        const own = merged.get(row.event);
        if (!own) return null;
        // No `report` arm here: an agent_settled never reaches this walk. The
        // grouping folds it into its section's header, which is where a closing
        // report belongs — it is about the whole section, not a step in it.
        const { text, tone } = formatEvent(row.event);
        // Deliberately silent events carry no row — see formatEvent.
        if (!text) return null;
        const { detail, duration, tone: outcomeTone } = formatOutcome(own.outcome);
        // See AgentSteps: `background` rides the outcome column so one row
        // carries both what the command was and what became of it.
        const outcome = [own.backgrounded ? "background" : "", detail, duration]
          .filter(Boolean)
          .join(" · ");
        return (
          <LogRow
            key={runEventKey(row.event)}
            text={text}
            tone={toneColor(tone)}
            outcome={{ text: outcome, tone: toneColor(outcomeTone) }}
          />
        );
      })}
    </>
  );
}

/** One agent's collapsible section: its header, its report, and its rows. */
function AgentSectionView({ section }: { section: AgentSection<StampedRunEvent> }) {
  // Open by default: this is a progress feed, and a run whose work is hidden
  // behind a click reads as a run that is not doing anything. Collapsing is for
  // taming a finished fan-out, not for hiding a live one.
  const [open, setOpen] = useState(true);
  const { agent } = section;
  const failed = agent.status === "failed";

  return (
    <Box sx={{ my: 0.5 }}>
      <Box
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={() => {
          setOpen((v) => !v);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
        sx={{ display: "flex", alignItems: "center", cursor: "pointer", userSelect: "none" }}
      >
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <AgentChip agent={agent} />
        {/* Collapsed, this line is ALL the reader gets about this agent, so it
            carries the verdict and the figures rather than a row count: choosing
            not to expand a section should still tell you whether it worked and
            how much code it produced. */}
        <Typography
          component="span"
          sx={{ font: "inherit", color: failed ? "error.light" : "grey.500", minWidth: 0 }}
        >
          {formatAgentStatus(agent)}
        </Typography>
      </Box>
      {/* Outside the collapse: a section folded shut is exactly when its report
          is the only thing left saying what the agent did. */}
      {agent.report ? <AgentReportNote report={agent.report} /> : null}
      <Collapse in={open} unmountOnExit>
        {/* The rule is what says "this is one agent's work", so the rows
            themselves drop the chip — repeating it on every row is noise, and
            it is also what makes a depth-2 agent read as nested. */}
        <Box sx={{ borderLeft: 1, borderColor: "grey.800", ml: 0.75, pl: 1.5 }}>
          <SectionRows section={section} />
        </Box>
      </Collapse>
    </Box>
  );
}

export function AgentLogLines({ events }: { events: StampedRunEvent[] }) {
  if (events.length === 0) {
    return <LogNote>No output from this cycle yet.</LogNote>;
  }
  // The lead is the top level rather than a section of its own: everything in
  // this cycle is its work, so wrapping it would indent the whole feed for
  // nothing. Its own report still shows, under its last row.
  const lead = groupByAgent(events);
  return (
    <>
      <SectionRows section={lead} />
      {lead.agent.report ? <AgentReportNote report={lead.agent.report} /> : null}
    </>
  );
}

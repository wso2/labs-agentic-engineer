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
  formatSubagentStatus,
  groupBySubagent,
  mergeOutcomes,
  type MergedRow,
  type SubagentGroup,
} from "@aep/progress-view";
import type { components } from "../../../generated/aep-api";
import { formatLine, formatOutcome } from "../../tasks/lib/timeline";
import { runLineKey, type RunProgressPhase } from "../hooks/useRunProgress";

type RunProgressLine = components["schemas"]["RunProgressLine"];

// One cycle's agent output. Extracted so the run's own cycle sections and the
// deployment surface's validation feed render a line identically — two
// renderings of the same stream that drifted apart would read as two different
// agents.

/**
 * Attribution for one line. Exported so the run feed and the task log stamp a
 * line identically — two renderings of the same attribution that drifted apart
 * would read as two different agents.
 *
 * A cycle can fan out to several subagents at once and their lines interleave,
 * so the chip carries the label the main agent gave that subagent ("Implement
 * todo-api service (issue #3)") when the runner recorded one. Falling back to
 * the bare "subagent" keeps older feeds — and a fan-out whose call carried no
 * description — readable.
 */
export function EmitterChip({ emitter, label }: { emitter: string; label?: string | undefined }) {
  // The main agent is the overwhelming majority of lines, so only a subagent
  // line is stamped — an unstamped line reads as "the main agent", which is
  // exactly the contract's own rule and keeps the feed quiet.
  if (emitter !== "subagent") return null;
  return (
    <Chip
      label={label || "subagent"}
      size="small"
      variant="outlined"
      title={label}
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

// The log surface moved to components/LogSection (ADR-0021): three surfaces now
// render logs and one definition keeps them identical. Re-exported here so the
// feature's existing importers are untouched.
// Imported as well as re-exported: a bare `export … from` does not bind the
// names in this module's own scope, and the renderers below use both.
import { LogNote, LogSurface } from "../../../components/LogSection";

export { LogNote, LogSurface };

/**
 * Empty-state copy for the agent log panel. Distinguishes attaching to a
 * finished run's archive from a live agent that has not spoken yet, and from a
 * settled run that truly had nothing to say.
 */
export function agentLogEmptyNote(
  phase: RunProgressPhase,
  opts: { agentRunning?: boolean } = {},
): string {
  switch (phase) {
    case "connecting":
      return "Loading agent output…";
    case "reconnecting":
      return "Reconnecting…";
    case "live":
      return opts.agentRunning
        ? "Waiting for the agent's first line…"
        : "Loading agent output…";
    case "ended":
      return "No output was recorded.";
    default:
      return "No output from this cycle yet.";
  }
}

/** Log surface with subtle loading / empty copy when the stream has no lines. */
export function AgentLogPanel({
  lines,
  phase,
  agentRunning = false,
  maxHeight = 420,
}: {
  lines: RunProgressLine[];
  phase: RunProgressPhase;
  agentRunning?: boolean;
  maxHeight?: number;
}) {
  return (
    <LogSurface maxHeight={maxHeight}>
      {lines.length === 0 ? (
        <LogNote>{agentLogEmptyNote(phase, { agentRunning })}</LogNote>
      ) : (
        <AgentLogLines lines={lines} />
      )}
    </LogSurface>
  );
}

/**
 * One rendered line, with its outcome trailing on the same row.
 *
 * The wire carries an action and its outcome as two events on purpose — the
 * action is emitted before the command runs, which is what makes the feed live.
 * This surface holds every line in state, so it can put the outcome back where
 * it belongs instead of printing it as a second row.
 */
function LogLine({
  text,
  tone,
  chip,
  outcome,
}: {
  text: string;
  tone: string;
  chip?: React.ReactNode;
  outcome?: { text: string; tone: string } | undefined;
}) {
  return (
    <Box sx={{ display: "flex", alignItems: "baseline", gap: 1.5 }}>
      {chip}
      <Typography
        component="div"
        sx={{ font: "inherit", color: tone, whiteSpace: "pre-wrap", wordBreak: "break-word", minWidth: 0 }}
      >
        {text}
      </Typography>
      {outcome?.text ? (
        <Typography
          component="div"
          title={outcome.text}
          sx={{
            font: "inherit",
            color: outcome.tone,
            // Pushed to the right so the durations and exit codes line up in
            // their own column and an abnormal one is findable by scanning.
            ml: "auto",
            textAlign: "right",
            maxWidth: "45%",
            wordBreak: "break-word",
            flexShrink: 0,
          }}
        >
          {outcome.text}
        </Typography>
      ) : null}
    </Box>
  );
}

/** The action rows of one line list, each carrying whatever its outcome added. */
function renderRows(rows: MergedRow<RunProgressLine>[]) {
  return rows
    .map((row) => ({ ...row, ...formatLine(row.line), outcome: formatOutcome(row.outcome) }))
    // Deliberately silent lines carry no row — see formatLine.
    .filter((r) => r.text);
}

function SubagentSection({ group }: { group: SubagentGroup<RunProgressLine> }) {
  // Open by default: this is a progress feed, and a run whose work is hidden
  // behind a click reads as a run that is not doing anything. Collapsing is for
  // taming a finished fan-out, not for hiding a live one.
  const [open, setOpen] = useState(true);
  const rendered = renderRows(mergeOutcomes(group.lines));
  const failed = group.report.status !== "running" && group.report.status !== "completed";

  return (
    <Box sx={{ my: 0.5 }}>
      <Box
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
        sx={{ display: "flex", alignItems: "center", cursor: "pointer", userSelect: "none" }}
      >
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <EmitterChip emitter="subagent" label={group.label} />
        {/* Collapsed, this line is ALL the reader gets about this subagent, so
            it carries the verdict and the figures rather than a line count:
            choosing not to expand a section should still tell you whether it
            worked and how much code it produced. */}
        <Typography
          component="span"
          sx={{ font: "inherit", color: failed ? "error.light" : "grey.500", minWidth: 0 }}
        >
          {formatSubagentStatus(group.report)}
        </Typography>
      </Box>
      <Collapse in={open} unmountOnExit>
        {/* The rule is what says "this is one agent's work", so the lines
            themselves drop the chip — repeating it on every row is noise. */}
        <Box sx={{ borderLeft: 1, borderColor: "grey.800", ml: 0.75, pl: 1.5 }}>
          {rendered.map((r) => (
            <LogLine key={runLineKey(r.line)} text={r.text} tone={r.tone} outcome={r.outcome} />
          ))}
        </Box>
      </Collapse>
    </Box>
  );
}

export function AgentLogLines({ lines }: { lines: RunProgressLine[] }) {
  if (lines.length === 0) {
    return <LogNote>No output from this cycle yet.</LogNote>;
  }
  const rows = groupBySubagent(lines);

  // The main agent's own lines are merged as ONE stream, because its action and
  // its outcome are routinely separated by a subagent section that spoke in
  // between. Merging is then looked up per line rather than re-derived inside
  // the walk, so a section still renders at the point its subagent first spoke —
  // the ordering is what makes a fan-out readable.
  const merged = new Map<RunProgressLine, MergedRow<RunProgressLine>>();
  for (const row of mergeOutcomes(rows.flatMap((r) => (r.kind === "line" ? [r.line] : [])))) {
    merged.set(row.line, row);
  }

  return (
    <>
      {rows.map((row) => {
        if (row.kind === "group") {
          return <SubagentSection key={`sub:${row.group.id}`} group={row.group} />;
        }
        // Absent from the map = folded into an earlier action's row.
        const own = merged.get(row.line);
        if (!own) return null;
        const { text, tone } = formatLine(row.line);
        // Deliberately silent lines carry no row — see formatLine.
        if (!text) return null;
        return (
          <LogLine
            key={runLineKey(row.line)}
            text={text}
            tone={tone}
            outcome={formatOutcome(own.outcome)}
            // Ungrouped, so the line carries its own attribution: null for the
            // main agent, and the bare "subagent" chip for a runner too old to
            // stamp an id — which would otherwise silently read as the main
            // agent's work.
            chip={<EmitterChip emitter={row.line.emitter} label={row.line.emitterLabel} />}
          />
        );
      })}
    </>
  );
}

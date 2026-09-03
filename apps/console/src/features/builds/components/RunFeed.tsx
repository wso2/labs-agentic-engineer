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
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { GitHubRefChip } from "../../../components/GitHubRefChip";
import { AgentLogLines, LogSurface } from "./AgentLogLines";
import { connectionTail } from "../lib/feedTail";
import { useRunProgress, type RunProgressCycle } from "../hooks/useRunProgress";

// The run feed: ONE SSE stream for the whole run, rendered as one accordion
// section per cycle. Grouping by cycle is the point — a fix or conflict cycle
// re-enters an earlier phase of the loop, so a flat log would read as the agent
// going backwards. Within a cycle, each subagent the main agent fanned out to
// gets its own collapsible section (see AgentLogLines, shared with the task
// log) — several run at once and their lines arrive interleaved, so read flat
// they would look like one agent contradicting itself.
//
// Sections read NEWEST FIRST. The cycle a reader came to watch is the newest one,
// so it leads rather than sitting below however much history the run accumulated.
// The LINES inside a section stay oldest-first — a log read upwards is unreadable,
// and that is a different tier of ordering from the boxes holding them.

/**
 * One cycle's accordion box. Exported because the VERSION feed renders the same
 * box under a run heading (BuildFeed) — one cycle must read identically whether
 * it is reached through its own run.
 */
function CycleSection({
  section,
  ordinal,
  runNumber,
  expanded,
  onToggle,
}: {
  section: RunProgressCycle;
  /** The cycle's CHRONOLOGICAL position, counted from the oldest — never its
   *  position on screen, which is reversed. */
  ordinal: number;
  /** Which run this cycle belongs to, for a surface that stacks several runs'
   *  feeds. Omitted leaves the heading as the cycle alone. Explicitly `| undefined`
   *  because `exactOptionalPropertyTypes` is on and the feed forwards its own
   *  optional prop straight through. */
  runNumber?: number | undefined;
  expanded: boolean;
  onToggle: (open: boolean) => void;
}) {
  const { cycle, lines } = section;
  // ONE string for the heading and for the pull request's accessible name. The link
  // has to state which box it belongs to — two runs each hold a "Cycle 1" — and
  // composing the prefix twice is how the two drift apart. Keeping them identical is
  // also what WCAG 2.5.3 asks for: the accessible name contains the visible label.
  const label =
    runNumber === undefined
      ? `Cycle ${ordinal}`
      : `Run ${runNumber} · Cycle ${ordinal}`;
  return (
    <Accordion
      disableGutters
      elevation={0}
      expanded={expanded}
      onChange={(_, open) => {
        onToggle(open);
      }}
      sx={{ "&:before": { display: "none" } }}
    >
      <AccordionSummary expandIcon={<ChevronDown size={16} />}>
        {/* Full width so the pull request can sit at the far end: the facts about
            the cycle read left to right, and the one link the row carries is where
            the eye lands last. */}
        <Stack
          direction="row"
          spacing={1}
          sx={{ alignItems: "center", width: "100%", pr: 1 }}
        >
          <Typography variant="subtitle2">{label}</Typography>
          <Chip label={cycle.kind} size="small" variant="outlined" />
          {cycle.attempts > 1 && (
            <Typography variant="caption" color="text.secondary">
              {cycle.attempts} attempts
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary">
            {lines.length} line{lines.length === 1 ? "" : "s"}
          </Typography>
          {/* A spacer rather than `ml: auto` on the link: Stack lays its spacing
              down as `margin-left` through a descendant selector, which outranks a
              margin set on the child's own sx and would pin the link beside the
              counts instead of at the row's end. */}
          <Box sx={{ flexGrow: 1 }} />
          {/* The pull request THIS cycle produced — per cycle rather than per run,
              because a run holds several (a repeat validation, a fix, a conflict
              resolution) and each opens its own. Absent until the agent opens one;
              the stream upserts the cycle frame, so it appears the moment the pull
              request lands rather than on the next page load.
              Named by section so it stays distinct from the page header's chip,
              which points at the newest cycle's pull request — the same one. */}
          {cycle.prUrl && cycle.prNumber ? (
            <GitHubRefChip
              kind="pull"
              number={cycle.prNumber}
              url={cycle.prUrl}
              name={`${label} pull request`}
              tooltip="Open this cycle's pull request"
              // The summary's whole surface toggles the section — without this,
              // opening the pull request also collapses the log being read.
              onClick={(e) => e.stopPropagation()}
            />
          ) : null}
        </Stack>
      </AccordionSummary>
      <AccordionDetails sx={{ pt: 0 }}>
        <LogSurface>
          <AgentLogLines lines={lines} />
        </LogSurface>
      </AccordionDetails>
    </Accordion>
  );
}

/**
 * The run's per-cycle progress feed. Mounted only where it should stream —
 * the hook opens the SSE connection on mount and closes it on unmount, so
 * keeping this behind a toggle is what keeps a settled page connection-free.
 */
export function RunFeed({
  projectName,
  runId,
  cycleKinds,
  expandNewest = true,
  runNumber,
}: {
  projectName: string;
  runId: string;
  /** Show only these cycle kinds. The stream is always the whole run — the
   *  filter is presentational, for a surface that owns one phase of the loop
   *  (the deployment surface owns validation). Omitted = every cycle. */
  cycleKinds?: readonly string[];
  /** Which run this feed is, for a surface that stacks one feed PER RUN: every
   *  feed numbers its own cycles from 1, so without this two runs each show a
   *  "Cycle 1" in the same stack. Omitted = no run prefix, the single-feed case. */
  runNumber?: number;
  /** Whether this feed may open its newest section. A page showing several feeds
   *  passes `false` for the historical ones, so exactly ONE box is open across the
   *  whole page rather than one per feed. */
  expandNewest?: boolean;
}) {
  const all = useRunProgress(projectName, runId);
  const feed = cycleKinds
    ? {
        ...all,
        cycles: all.cycles.filter((c) => cycleKinds.includes(c.cycle.kind)),
      }
    : all;
  // Reversed for RENDER only. `feed.cycles` stays oldest-first, which is the order
  // the wire promises (the contract documents cycles as "Oldest first" and the SSE
  // walks them that way), and the ordinals below are still counted from it.
  const shown = [...feed.cycles].reverse();

  // Which section is open, CONTROLLED. `defaultExpanded` cannot express this: it is
  // read once at mount, so a cycle arriving mid-stream opened alongside the one
  // already open — two logs expanded, plus MUI's warning about an uncontrolled
  // Accordion changing its default. Three meanings, one state:
  //   undefined — follow the newest cycle, which the stream keeps moving
  //   null      — the reader closed it and wants nothing open
  //   string    — the reader picked that section
  // The reader's choice outranks the stream, the same way the task log releases its
  // bottom-pin once the reader scrolls up.
  const [chosen, setChosen] = useState<string | null | undefined>(undefined);
  const followed = expandNewest ? (shown[0]?.cycle.id ?? null) : null;
  const openId = chosen === undefined ? followed : chosen;

  // Connection health only. How the run ENDED labels the section header instead
  // — beside the title, where every other section on this page carries its
  // status — so repeating it under the log would be the same news twice.
  const tail = connectionTail(feed.phase);

  return (
    <Box>
      {feed.cycles.length === 0 ? (
        // The same `EmptyState compact` the Build logs section below uses. Two
        // placeholders that sit one card apart on this page read as one surface
        // only if they are drawn the same way — a left-aligned sentence beside a
        // centred one looks like a mistake, not a distinction.
        <EmptyState
          compact
          description="No cycle output yet — the run's first agent has not written a line."
        />
      ) : (
        shown.map((section, i) => (
          <CycleSection
            key={section.cycle.id}
            section={section}
            // Counted from the OLDEST, so the stack can be reversed without
            // renumbering the boxes — cycle 1 is the run's first, wherever it is
            // drawn. Numbered within what is shown, too: a filtered feed owns one
            // phase and its section is "Cycle 1" of that phase, not of the whole run.
            ordinal={feed.cycles.length - i}
            // Every feed numbers its own cycles from 1, so the run is what tells two
            // "Cycle 1"s apart when a version was validated more than once.
            runNumber={runNumber}
            // The newest cycle is what the user came to watch, and it now LEADS the
            // stack instead of trailing it; older ones stay collapsed so a long run
            // does not open as a wall of log.
            expanded={openId === section.cycle.id}
            onToggle={(open) => {
              setChosen(open ? section.cycle.id : null);
            }}
          />
        ))
      )}
      {tail && (
        <Typography
          variant="caption"
          color="text.secondary"
          // Right-aligned: it is the feed's own connection status, not a line
          // OF the feed, and the left edge is where the log's content starts.
          sx={{ display: "block", mt: 1, textAlign: "right" }}
        >
          {tail}
        </Typography>
      )}
    </Box>
  );
}

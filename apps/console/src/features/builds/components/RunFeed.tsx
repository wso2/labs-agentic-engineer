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
import { RunCrew } from "./RunCrew";
import { connectionTail } from "../lib/feedTail";
import { useRunProgress, type RunProgressCycle } from "../hooks/useRunProgress";

// The run feed: ONE SSE stream for the whole run, rendered as one accordion
// section per cycle. Grouping by cycle is the point — a fix or conflict cycle
// re-enters an earlier phase of the loop, so a flat log would read as the agent
// going backwards.
//
// Inside a cycle the events are not a log any more: they are a CREW (see
// RunCrew). A flat feed could show what happened but never whether anything was
// still happening, because a stall on a log surface looks exactly like a log
// that has scrolled — and on a run that fans out to several agents at once, the
// interleaved rows read as one agent contradicting itself.
//
// Sections read NEWEST FIRST. The cycle a reader came to watch is the newest one,
// so it leads rather than sitting below however much history the run accumulated.

/**
 * One cycle's accordion box. Exported because the VERSION feed renders the same
 * box under a run heading (BuildFeed) — one cycle must read identically whether
 * it is reached through its own run.
 */
function CycleSection({
  section,
  label,
  showKind,
  expanded,
  onToggle,
}: {
  section: RunProgressCycle;
  /** The heading — ONE string for it and for the pull request's accessible name.
   *  The link has to state which box it belongs to, and composing the label twice
   *  is how the two drift apart. Keeping them identical is also what WCAG 2.5.3
   *  asks for: the accessible name contains the visible label. */
  label: string;
  /** Whether the kind chip says anything: it tells cycles apart, so a feed that
   *  shows one kind has nothing for it to tell. */
  showKind: boolean;
  expanded: boolean;
  onToggle: (open: boolean) => void;
}) {
  const { cycle, events } = section;
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
          {showKind && <Chip label={cycle.kind} size="small" variant="outlined" />}
          {/* Dispatches of THIS cycle: an agent that died was started again for
              the same work. "Started", not "attempts" — an attempt is what the
              validation page calls one judging of a version, and the two would
              otherwise sit on one row meaning different things. */}
          {cycle.attempts > 1 && (
            <Typography variant="caption" color="text.secondary">
              started {cycle.attempts} times
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary">
            {events.length} event{events.length === 1 ? "" : "s"}
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
              tooltip="Open its pull request"
              // The summary's whole surface toggles the section — without this,
              // opening the pull request also collapses the log being read.
              onClick={(e) => e.stopPropagation()}
            />
          ) : null}
        </Stack>
      </AccordionSummary>
      <AccordionDetails sx={{ pt: 0 }}>
        <RunCrew events={events} />
      </AccordionDetails>
    </Accordion>
  );
}

/**
 * A section's heading. The surface's own label wins; the default is the cycle's
 * position, prefixed by the run where a page stacks several feeds — every feed
 * numbers its own cycles from 1, so the run is what tells two "Cycle 1"s apart.
 */
function heading(
  ordinal: number,
  runNumber: number | undefined,
  label: ((ordinal: number) => string) | undefined,
): string {
  if (label) return label(ordinal);
  return runNumber === undefined ? `Cycle ${ordinal}` : `Run ${runNumber} · Cycle ${ordinal}`;
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
  label,
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
  /** How a section is headed, given the cycle's chronological position within
   *  this feed (1 = oldest). Omitted = the feed's own `Cycle N`, prefixed by the
   *  run when `runNumber` is set. A surface whose unit is not the cycle supplies
   *  its own — the validation page, where a box is one attempt on the version
   *  and "cycle" is the loop's word, not the reader's. */
  label?: (ordinal: number) => string;
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
          description="No output yet — the run's first agent has not written a line."
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
            label={heading(feed.cycles.length - i, runNumber, label)}
            // A feed filtered to one kind would stamp the same chip on every row.
            showKind={!cycleKinds || cycleKinds.length > 1}
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

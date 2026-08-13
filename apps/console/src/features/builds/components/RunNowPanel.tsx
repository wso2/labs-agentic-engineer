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
import { Box, ButtonBase, Collapse, Link, Stack, Typography } from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import type { RunProgressCycle, RunProgressPhase } from "../hooks/useRunProgress";
import { formatLine } from "../../tasks/lib/timeline";
import { aheadSentence, glanceHeadline, type RunGlance } from "../lib/runGlance";
import { AgentLogPanel, agentLogEmptyNote } from "./AgentLogLines";
import { IssueChips } from "./IssueChips";

type TaskView = components["schemas"]["TaskView"];

/**
 * The one stage worth words, and nothing else.
 *
 * The rail narrates every stage because a reader of a finished run wants the
 * whole story. A reader of a LIVE run wants one thing: what is happening now,
 * and what it is working on. Everything still ahead collapses to a single quiet
 * line — five future stages each explaining what they wait for is five lines of
 * "not yet".
 */
export function RunNowPanel({
  projectName,
  glance,
  issues,
  issuesCaption,
  lines,
  logPhase,
  showLog,
  onOpenLog,
}: {
  projectName: string;
  glance: RunGlance;
  /** The issues the current build session is working. */
  issues: TaskView[];
  issuesCaption?: string;
  /** The current session's log lines, newest last. */
  lines: RunProgressCycle["lines"];
  logPhase: RunProgressPhase;
  /** Whether the run feed is attached at all — a settled run opens none. */
  showLog: boolean;
  onOpenLog: () => void;
}) {
  const now = glance.nowIndex === null ? undefined : glance.stages[glance.nowIndex];

  if (!now) {
    return (
      <Typography variant="body2" color="text.secondary">
        Every stage of this build session is done.
      </Typography>
    );
  }

  const { stage, step } = now;

  return (
    <Stack spacing={1.5}>
      {/* NOW sits in a left gutter rather than stacked above: the label is a
          margin note, and giving it its own line pushed the sentence a reader
          actually came for further down the card. */}
      <Stack direction="row" spacing={2} sx={{ alignItems: "flex-start" }}>
        <Typography
          variant="caption"
          sx={{
            fontWeight: 700,
            letterSpacing: "0.08em",
            color: "info.main",
            flexShrink: 0,
            width: 34,
            mt: 0.25,
          }}
        >
          NOW
        </Typography>
        <Stack spacing={1.25} sx={{ minWidth: 0, flexGrow: 1 }}>
        <Typography variant="body1" sx={{ fontWeight: 600 }}>
          {glanceHeadline(stage)}{" "}
          <Typography component="span" variant="caption" color="text.disabled">
            · {stage.actor} · step {step} of {glance.stages.length}
            {stage.fact ? ` · ${stage.fact}` : ""}
          </Typography>
        </Typography>
        {stage.note && (
          <Typography variant="body2" color="text.secondary">
            {stage.note}
          </Typography>
        )}
        {stage.factHref && stage.fact && (
          <Link
            href={stage.factHref}
            target="_blank"
            rel="noreferrer"
            variant="body2"
          >
            {stage.fact} →
          </Link>
        )}

        <IssueChips
          projectName={projectName}
          issues={issues}
          {...(issuesCaption ? { caption: issuesCaption } : {})}
        />

        <AgentLogDrawer
          lines={lines}
          phase={logPhase}
          showLog={showLog}
          onOpenLog={onOpenLog}
        />
        </Stack>
      </Stack>

      {glance.ahead.length > 0 && (
        <Typography variant="caption" color="text.disabled">
          Then: {aheadSentence(glance.ahead)}.
        </Typography>
      )}
    </Stack>
  );
}

const PHASE_LABEL: Record<RunProgressPhase, string> = {
  idle: "",
  connecting: "connecting",
  live: "live",
  reconnecting: "reconnecting",
  ended: "ended",
};

/**
 * The agent's output, collapsed to its newest line.
 *
 * The rail keeps this open at 420px because it is the record of how the code
 * got written. Here it is one line — the thing the agent just did — because the
 * panel's job is the current moment, and the whole log is one click below it.
 */
function AgentLogDrawer({
  lines,
  phase,
  showLog,
  onOpenLog,
}: {
  lines: RunProgressCycle["lines"];
  phase: RunProgressPhase;
  showLog: boolean;
  onOpenLog: () => void;
}) {
  const [open, setOpen] = useState(false);

  const newest = lines.at(-1);
  const preview = !showLog
    ? "Not attached — open to replay this run's log."
    : newest
      ? formatLine(newest).text
      : agentLogEmptyNote(phase);

  return (
    <Box sx={{ border: "1px solid", borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
      <ButtonBase
        onClick={() => {
          if (!showLog) onOpenLog();
          setOpen((v) => !v);
        }}
        aria-expanded={open}
        aria-label={open ? "Hide the agent log" : "Show the agent log"}
        sx={{ width: "100%", justifyContent: "flex-start", px: 1.5, py: 1, gap: 1.25, textAlign: "left" }}
      >
        <ChevronRight
          size={14}
          style={{
            flexShrink: 0,
            transition: "transform 0.15s",
            transform: open ? "rotate(90deg)" : "none",
          }}
        />
        <Typography variant="caption" sx={{ fontWeight: 600, flexShrink: 0 }}>
          Agent log
        </Typography>
        <Typography
          variant="caption"
          sx={{
            flexGrow: 1,
            minWidth: 0,
            fontFamily: "monospace",
            color: "info.main",
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
        >
          {preview}
        </Typography>
        <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>
          {PHASE_LABEL[phase]}
        </Typography>
      </ButtonBase>

      <Collapse in={open} unmountOnExit>
        <Box sx={{ px: 1.5, pb: 1.5 }}>
          <AgentLogPanel lines={lines} phase={phase} maxHeight={320} />
        </Box>
      </Collapse>
    </Box>
  );
}

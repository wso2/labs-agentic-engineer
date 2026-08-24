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
  Box,
  ButtonBase,
  Card,
  CardContent,
  Collapse,
  Divider,
  LinearProgress,
  Link,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { GitHubRefChip } from "../../../components/GitHubRefChip";
import { gateSubject } from "../../tasks/lib/issueRows";
import { bucketMilestone } from "../lib/milestoneBuckets";
import { gateRows } from "../lib/provisioning";

type TaskView = components["schemas"]["TaskView"];

/**
 * The milestone beside the run: how much of this version is left.
 *
 * The run card answers "what is happening"; this answers "how much is done".
 * Open work is listed in full because that is what a reader plans against;
 * closed work collapses to a count, because it only needs to be findable.
 *
 * Both populations are WHOLE, closed members included — a version's record is
 * what it did, not only what it has left.
 */
export function MilestonePanel({
  tag,
  title,
  work,
  gates,
  ledger,
  claimed,
  presumeOpenWork = false,
  claimedBy,
  issuesUrl,
}: {
  tag: string;
  /** The milestone's own name, when it carries one richer than the tag. */
  title?: string;
  /** The milestone's agent work, open and closed. */
  work: TaskView[];
  /** Every connection gate, resolved ones included. */
  gates: TaskView[];
  /** Bare human issues that joined the milestone — never worked, never
   *  stalling settle (ADR-0013 §7). Their own section, so they are not read
   *  as agent work. */
  ledger: TaskView[];
  /** Issue numbers the run's OPEN cycle has claimed (openCycleClaims). */
  claimed: ReadonlySet<number>;
  /** A live session with no claims yet — presume it works the open issues. */
  presumeOpenWork?: boolean;
  /** Which cycle claimed an issue, for the ones in flight. */
  claimedBy?: (issue: TaskView) => string | undefined;
  /** The repo's issue list — omitted when the project has no repo URL yet. */
  issuesUrl?: string;
}) {
  // The two-value vocabulary plus the run's recorded claims — see
  // lib/milestoneBuckets for why nothing here reads liveness off a row.
  const { inProgress, open, closed } = bucketMilestone(work, claimed, presumeOpenWork);

  const delivered = work.length > 0 && closed.length === work.length;
  const percent = work.length === 0 ? 0 : (closed.length / work.length) * 100;

  const scope = [
    `${work.length} issue${work.length === 1 ? "" : "s"}`,
    ...(gates.length > 0
      ? [`${gates.length} connection${gates.length === 1 ? "" : "s"}`]
      : []),
  ].join(" + ");

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography
          variant="caption"
          sx={{
            fontWeight: 700,
            letterSpacing: "0.08em",
            color: delivered ? "success.main" : "text.secondary",
          }}
        >
          {delivered ? "MILESTONE · DELIVERED" : "MILESTONE"}
        </Typography>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, mt: 0.5 }}>
          {title && title !== tag ? `${tag} — ${title}` : tag}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {scope}
        </Typography>

        <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", mt: 1.5 }}>
          <LinearProgress
            variant="determinate"
            value={percent}
            aria-label={`${closed.length} of ${work.length} issues closed`}
            sx={{
              flexGrow: 1,
              height: 6,
              borderRadius: 3,
              // MUI tints the track with the bar's colour, which reads as "all
              // done" at 0% — keep the track neutral, colour only the fill.
              bgcolor: "action.selected",
              "& .MuiLinearProgress-bar": { bgcolor: "success.main", borderRadius: 3 },
            }}
          />
          <Typography
            variant="caption"
            sx={{
              color: delivered ? "success.main" : "text.secondary",
              fontVariantNumeric: "tabular-nums",
              whiteSpace: "nowrap",
            }}
          >
            {closed.length} / {work.length} closed
          </Typography>
        </Stack>

        <Stack direction="row" spacing={1} sx={{ mt: 1.5, flexWrap: "wrap", rowGap: 1 }}>
          {/* Soft, not outlined: these are a readout of the milestone, and an
              outline reads as something you can act on. Same treatment the run
              card's state chips use, so the two columns agree. */}
          {inProgress.length > 0 && (
            <StatusChip
              label={`${inProgress.length} in progress`}
              tone="info"
              appearance="soft"
              dot
            />
          )}
          {open.length > 0 && (
            <StatusChip label={`${open.length} open`} tone="neutral" appearance="soft" />
          )}
          {closed.length > 0 && (
            <StatusChip
              label={`${closed.length} closed`}
              tone="success"
              appearance="soft"
            />
          )}
        </Stack>

        {/* The one bucket with an agent on it right now gets a surface of its
            own — it is the panel's answer to "what is being worked", and a
            plain row buried it among the six open ones. */}
        <IssueGroup
          title="In progress"
          issues={inProgress}
          tone="info.main"
          highlight
          {...(claimedBy ? { note: claimedBy } : {})}
        />
        <IssueGroup
          title="Open"
          issues={open}
          tone="text.disabled"
          counted
        />
        <ClosedGroup title="Closed" issues={closed} />

        {gates.length > 0 && (
          <Box sx={{ mt: 2 }}>
            <Divider sx={{ mb: 1.5 }} />
            <GroupLabel title="Connections" />
            <Stack spacing={1} sx={{ mt: 1 }}>
              {/* gateRows is the ONE mapping from a gate to who is acting —
                  reinventing it here once collapsed "idle" (a human must
                  supply something) into "provisioning" (nothing needed). */}
              {gateRows(gates).map(({ gate, state, label }) => (
                <Stack key={gate.issueNumber} direction="row" spacing={1} sx={{ alignItems: "baseline" }}>
                  <Dot color={GATE_STATE_COLOR[state] ?? "text.disabled"} />
                  <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace" }}>
                    #{gate.issueNumber}
                  </Typography>
                  <Typography variant="body2" sx={{ color: "text.secondary" }}>
                    {gateSubject(gate.title)}
                  </Typography>
                  <Typography
                    variant="caption"
                    sx={{ color: GATE_STATE_COLOR[state] ?? "text.secondary" }}
                  >
                    {label}
                  </Typography>
                </Stack>
              ))}
            </Stack>
          </Box>
        )}

        {ledger.length > 0 && (
          <Box sx={{ mt: 2 }}>
            <Divider sx={{ mb: 1.5 }} />
            <GroupLabel title="Ledger" />
            <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 0.5 }}>
              Filed against this version by a human — never worked by an agent,
              and never holding the run.
            </Typography>
            <Stack spacing={1} sx={{ mt: 1 }}>
              {ledger.map((issue) => (
                <IssueRow
                  key={issue.issueNumber}
                  issue={issue}
                  tone="text.disabled"
                />
              ))}
            </Stack>
          </Box>
        )}

        {issuesUrl && (
          <Link
            href={issuesUrl}
            target="_blank"
            rel="noreferrer"
            variant="body2"
            sx={{ display: "inline-block", mt: 2 }}
          >
            View all issues on GitHub →
          </Link>
        )}
      </CardContent>
    </Card>
  );
}

// StageState → the panel's colour vocabulary, matching StatusChip tones.
const GATE_STATE_COLOR: Record<string, string> = {
  done: "success.main",
  active: "info.main",
  attention: "warning.main",
  failed: "error.main",
  waiting: "text.disabled",
};

function Dot({ color }: { color: string }) {
  return (
    <Box
      aria-hidden
      sx={{ width: 6, height: 6, mt: 0.75, borderRadius: "50%", flexShrink: 0, bgcolor: color }}
    />
  );
}

/**
 * A section heading. The count is suffixed only where it TELLS the reader
 * something they cannot see: how many are hidden below a fold (closed), or how
 * long a list runs (open). "In progress" and "Connections" are short and fully
 * on screen, so a count there is noise.
 */
function GroupLabel({ title, count }: { title: string; count?: number }) {
  return (
    <Typography
      variant="caption"
      sx={{ fontWeight: 700, letterSpacing: "0.08em", color: "text.secondary" }}
    >
      {title.toUpperCase()}
      {count === undefined ? "" : ` · ${count}`}
    </Typography>
  );
}

function IssueRow({
  issue,
  tone,
  note,
}: {
  issue: TaskView;
  tone: string;
  note?: string;
}) {
  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: "baseline", minWidth: 0 }}>
      <Dot color={tone} />
      <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace", flexShrink: 0 }}>
        #{issue.issueNumber}
      </Typography>
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        {/* Durable facts only, and NOT clickable (ADR-0013 §5): the run's
            story is the card beside this, and the issue itself lives on
            GitHub — which is the one link the row carries. */}
        <Typography variant="body2" sx={{ color: "text.primary" }}>
          {issue.title}
        </Typography>
        {note && (
          <Typography variant="caption" sx={{ display: "block", color: "text.secondary" }}>
            {note}
          </Typography>
        )}
      </Box>
      <GitHubRefChip kind="issue" number={issue.issueNumber} url={issue.issueUrl} />
    </Stack>
  );
}

function IssueGroup({
  title,
  issues,
  tone,
  note,
  highlight = false,
  counted = false,
}: {
  title: string;
  issues: TaskView[];
  tone: string;
  note?: (issue: TaskView) => string | undefined;
  /** Give each row its own tinted surface — for the bucket that is moving. */
  highlight?: boolean;
  /** Suffix the heading with the count. */
  counted?: boolean;
}) {
  if (issues.length === 0) return null;
  return (
    <Box sx={{ mt: 2 }}>
      <GroupLabel title={title} {...(counted ? { count: issues.length } : {})} />
      <Stack spacing={1} sx={{ mt: 1 }}>
        {issues.map((issue) => {
          const detail = note?.(issue);
          const row = (
            <IssueRow
              issue={issue}
              tone={tone}
              {...(detail ? { note: detail } : {})}
            />
          );
          if (!highlight) {
            return <Box key={issue.issueNumber}>{row}</Box>;
          }
          return (
            <Box
              key={issue.issueNumber}
              sx={{
                px: 1.25,
                py: 1,
                borderRadius: 1,
                border: "1px solid",
                borderColor: (t) => alpha(t.palette.info.main, 0.25),
                bgcolor: (t) => alpha(t.palette.info.main, 0.07),
              }}
            >
              {row}
            </Box>
          );
        })}
      </Stack>
    </Box>
  );
}

/** Closed work: a count by default, the list on demand. */
function ClosedGroup({
  title,
  issues,
}: {
  title: string;
  issues: TaskView[];
}) {
  const [open, setOpen] = useState(false);
  if (issues.length === 0) return null;

  return (
    <Box sx={{ mt: 2 }}>
      <Divider sx={{ mb: 1 }} />
      <ButtonBase
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-label={`${open ? "Hide" : "Show"} the ${issues.length} closed issues`}
        sx={{ width: "100%", justifyContent: "flex-start", gap: 1, py: 0.5 }}
      >
        <ChevronRight
          size={13}
          style={{
            flexShrink: 0,
            transition: "transform 0.15s",
            transform: open ? "rotate(90deg)" : "none",
          }}
        />
        <GroupLabel title={title} count={issues.length} />
        <Box sx={{ flexGrow: 1 }} />
        {/* A closed agent issue was closed BY its merge — that is the only way
            one closes — so the count needs no per-row explanation. */}
        <Typography variant="caption" sx={{ color: "success.main" }}>
          all merged
        </Typography>
      </ButtonBase>
      <Collapse in={open} unmountOnExit>
        <Stack spacing={1} sx={{ mt: 1 }}>
          {issues.map((issue) => (
            <IssueRow
              key={issue.issueNumber}
              issue={issue}
              tone="success.main"
            />
          ))}
        </Stack>
      </Collapse>
    </Box>
  );
}

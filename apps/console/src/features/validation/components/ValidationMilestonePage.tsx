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

import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Divider,
  IconButton,
  Menu,
  MenuItem,
  Stack,
  Tooltip,
  type Theme,
} from "@wso2/oxygen-ui";
import { Copy, Ellipsis, GitHub, Play, RotateCw, X } from "@wso2/oxygen-ui-icons-react";
import { Link } from "@tanstack/react-router";
import {
  isReportParseError,
  parseAcceptanceReport,
  tallyOutcomes,
  tallySentence,
} from "@aep/ui-acceptance-view";
import { EmptyState } from "../../../components/EmptyState";
import { LogSection } from "../../../components/LogSection";
import { PageHeader } from "../../../components/PageHeader";
import { SectionCaption } from "../../../components/SectionCaption";
import type { components } from "../../../generated/aep-api";
import { useCancelRun } from "../../builds/api/queries";
import { RunFeed } from "../../builds/components/RunFeed";
import { useTicker } from "../../builds/hooks/useTicker";
import { useTask } from "../../tasks/api/queries";
import { statusLine } from "../../tasks/lib/statusLine";
import { useStartValidation, useValidation, useValidationSnapshot } from "../api/queries";
import { validationChip } from "../lib/chip";
import { validationIsLive } from "../lib/lifecycle";
import { countsFromScenarios } from "../lib/verdict";
import { ReportCard, type Attempt } from "./ReportCard";
import { ValidationSummaryCard } from "./ValidationSummaryCard";

type ValidationDetail = components["schemas"]["ValidationDetail"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];

/** Only validation cycles reach this page; the server filtered the rest. */
const VALIDATION_CYCLE = ["validation"] as const;

/**
 * Makes several runs' cycle boxes read as ONE block.
 *
 * MUI rounds an Accordion's corners by :first-of-type and :last-of-type among
 * its SIBLINGS, so consecutive accordions in one parent already fit together —
 * which is exactly how the report card's older attempts get their silhouette,
 * with no help. The log card cannot lean on that: each run is a RunFeed, and a
 * RunFeed wraps its cycles in a Box of its own, so two runs' cycles are never
 * siblings and every run rounds itself. This wrapper takes the decision back:
 * square every cycle inside it, then round only the first cycle of the first
 * run and the last cycle of the last run.
 *
 * It reaches into RunFeed's shape (a Box holding Accordions) rather than
 * changing RunFeed, which is shared with the builds page and is its own PR to
 * touch. The direct-child `div` selectors skip the caption, which is a span.
 */
const groupedCycleCorners = (theme: Theme) => ({
  "& .MuiAccordion-root": { borderRadius: 0 },
  "& > div:first-of-type .MuiAccordion-root:first-of-type": {
    borderTopLeftRadius: theme.shape.borderRadius,
    borderTopRightRadius: theme.shape.borderRadius,
  },
  "& > div:last-of-type .MuiAccordion-root:last-of-type": {
    borderBottomLeftRadius: theme.shape.borderRadius,
    borderBottomRightRadius: theme.shape.borderRadius,
  },
});

/**
 * One version's validation: what it concluded, every attempt's report, and the
 * agent's feed.
 *
 * Shaped after the build detail page, because the two answer the same question
 * about the same version and a reader moves between them. What differs is the
 * ordering argument: here the REPORT sits above the log, where builds puts its
 * Tasks. The report is the durable record — committed to git, kept forever —
 * while the feed behind the log is a recording pruned at 30 days (ADR-0027), so
 * on an older version the log has nothing to say and the report still does.
 */
export function ValidationMilestonePage({
  projectName,
  tag,
}: {
  projectName: string;
  tag: string;
}) {
  const detail = useValidation(projectName, tag);
  const data = detail.data;

  // Attempts, newest first, flattened out of the runs and numbered from the
  // OLDEST across the whole version, so the numbers descend down the page
  // (ADR-0017) — the same rule the log below follows, which is what lets the
  // two lists be read as one history.
  const attempts = useMemo(() => flattenAttempts(data?.runs ?? []), [data?.runs]);
  const newest = attempts[0];

  // The newest attempt's evidence is the page's, not the section's: the verdict
  // card needs its counts whether or not anything is expanded.
  const newestSnapshot = useValidationSnapshot(
    projectName,
    tag,
    newest?.cycle.id ?? "",
    Boolean(newest),
    Boolean(newest?.cycle.endedAt),
  );

  const counts = useMemo(() => {
    const raw = newestSnapshot.data?.report;
    if (!raw) return undefined;
    const parsed = parseAcceptanceReport(raw);
    return isReportParseError(parsed) ? undefined : countsFromScenarios(parsed.scenarios);
  }, [newestSnapshot.data?.report]);

  const countsLine = useMemo(() => {
    const raw = newestSnapshot.data?.report;
    if (!raw) return "";
    const parsed = parseAcceptanceReport(raw);
    return isReportParseError(parsed) ? "" : tallySentence(tallyOutcomes(parsed.scenarios));
  }, [newestSnapshot.data?.report]);

  const state = data?.state ?? "";
  // VALIDATION itself is running, not merely the loop: under `awaiting-fix` the
  // cycle in flight is coding, so the issue's newest comment would be a
  // finished attempt's last words.
  const validating = state === "running";
  const issueNumber = newest?.cycle.validationIssue ?? 0;
  const issue = useTask(projectName, issueNumber, { live: validating });
  // A comment outlives its run, so this is gated: ungated, a closing summary
  // sat under a settled verdict forever.
  const note = validating && issue.data ? statusLine(issue.data) : null;

  // One clock for the page, so every counting surface moves together.
  useTicker(Boolean(newest?.cycle.createdAt) && !newest?.cycle.endedAt);

  const [actionError, setActionError] = useState<string | null>(null);

  const backTo = {
    link: <Link to="/projects/$projectName/validations" params={{ projectName }} />,
    label: "Back to Validations",
  };
  const chip = validationChip(state);

  const header = (actions?: React.ReactNode) => (
    <>
      <PageHeader
        title={`Validation ${tag}`}
        backTo={backTo}
        {...(chip ? { status: { ...chip, variant: "filled" as const } } : {})}
        {...(actions ? { actions } : {})}
      />
      {actionError && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setActionError(null)}>
          {actionError}
        </Alert>
      )}
    </>
  );

  if (detail.isPending) {
    return (
      <>
        {header()}
        <Stack sx={{ alignItems: "center", p: 6 }}>
          <CircularProgress size={24} aria-label="Loading validation" />
        </Stack>
      </>
    );
  }

  if (detail.isError || !data) {
    return (
      <>
        {header()}
        <Alert
          severity="error"
          action={<Button onClick={() => void detail.refetch()}>Retry</Button>}
        >
          Failed to load this version's validation
          {detail.error instanceof Error && detail.error.message
            ? `: ${detail.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  const actions = (
    <ValidationActions
      projectName={projectName}
      tag={tag}
      detail={data}
      hasVerdict={attempts.some((a) => Boolean(a.cycle.validationVerdict))}
      issueUrl={issue.data?.issueUrl}
      runId={newest?.runId}
      onError={setActionError}
    />
  );

  // No attempt has ever been made against this version. Three sentences rather
  // than one, because the reader's next move differs: wait, act, or neither.
  if (attempts.length === 0) {
    return (
      <>
        {header(actions)}
        <EmptyState compact description={emptyReason(state, data.live)} />
      </>
    );
  }

  const live = validationIsLive(state);
  // Newest first, so the log's runs match the report's ordering above it.
  const feedRuns = [...data.runs];
  const [newestRun, ...olderRuns] = feedRuns;
  // Each feed heads its boxes "Attempt N" with N counted across the version, so
  // a feed needs to know how many attempts the runs OLDER than its own hold.
  // Read off the detail rather than the stream: the older runs are settled, so
  // their count cannot move under a live feed.
  const attemptLabel = (runIndex: number) => {
    const before = attemptsBefore(feedRuns, runIndex);
    return (ordinal: number) => `Attempt ${String(before + ordinal)}`;
  };

  return (
    <>
      {header(actions)}
      <Stack spacing={2}>
        <ValidationSummaryCard
          state={state}
          verdict={newest?.cycle.validationVerdict ?? ""}
          counts={counts}
          countsLine={countsLine}
          startedAt={newest?.cycle.createdAt}
          endedAt={newest?.cycle.endedAt}
          live={live}
          repairing={state === "awaiting-fix"}
          note={note}
        />

        <ReportCard
          projectName={projectName}
          tag={tag}
          attempts={attempts}
          state={state}
          newestSnapshot={newestSnapshot}
        />

        {/* Open while something is running, collapsed once the version has
            settled. LogSection unmounts its children when closed, so a settled
            version opens no SSE connection until the reader asks for one —
            which matters most on an old version, whose recording is likely
            gone anyway. */}
        <LogSection title="Validation logs" defaultOpen={live}>
          <Stack spacing={2}>
            {/* The same two blocks as the report card above: the newest run alone
                so its cycles keep all four corners, then every older run together
                under the caption so they read as one fitted block. */}
            <Box>
              {newestRun && (
                <RunFeed
                  projectName={projectName}
                  runId={newestRun.id}
                  cycleKinds={VALIDATION_CYCLE}
                  label={attemptLabel(0)}
                  expandNewest
                />
              )}
            </Box>
            {olderRuns.length > 0 && (
              <Box sx={groupedCycleCorners}>
                <SectionCaption>EARLIER ATTEMPTS OF {tag.toUpperCase()}</SectionCaption>
                {olderRuns.map((run, j) => (
                  <RunFeed
                    key={run.id}
                    projectName={projectName}
                    runId={run.id}
                    cycleKinds={VALIDATION_CYCLE}
                    label={attemptLabel(j + 1)}
                    expandNewest={false}
                  />
                ))}
              </Box>
            )}
          </Stack>
        </LogSection>
      </Stack>
    </>
  );
}

/**
 * Why there is nothing here, and whether the reader should do something.
 *
 * The split on `live` is the same boolean that enables the trigger, so the
 * sentence and the control cannot contradict each other: a version mid-build
 * would otherwise read as one where the reader must act, beside a disabled
 * menu item.
 */
function emptyReason(state: string, live: boolean): string {
  if (state === "skipped") {
    return "This version has no acceptance criteria, so there is nothing to validate against.";
  }
  if (live) {
    return "Nothing validated yet. After a deployment, the deployed system is validated against the acceptance criteria in your spec. Results appear here.";
  }
  return "Nothing validated yet. Run validation to check this version against its acceptance criteria.";
}

/**
 * One attempt per validation cycle, newest first, numbered from the oldest
 * across the whole version. Which run an attempt sat in is not part of its
 * name: since validation became its own run, an attempt IS a run, and a run
 * holding two attempts is the platform dispatching again after an agent merged
 * without a report — a remedy, not a distinction a reader needs in a heading.
 */
function flattenAttempts(runs: readonly MilestoneRunView[]): Attempt[] {
  // Runs arrive newest first and each run's cycles in dispatch order, so the
  // runs are kept as they come and only each run's cycles are walked backwards.
  // Reversing the whole flattened list instead would put the OLDER run's
  // attempts on top — the run order is already right, the cycle order is not.
  return runs.flatMap((run, runIndex) => {
    const before = attemptsBefore(runs, runIndex);
    return (run.cycles ?? [])
      .map((cycle, i): Attempt => ({ cycle, number: before + i + 1, runId: run.id }))
      .reverse();
  });
}

/** How many attempts the runs OLDER than the one at `index` hold. Runs arrive
 *  newest first, so those are the ones after it. */
function attemptsBefore(runs: readonly MilestoneRunView[], index: number): number {
  return runs.slice(index + 1).reduce((n, run) => n + (run.cycles ?? []).length, 0);
}

function ValidationActions({
  projectName,
  tag,
  detail,
  hasVerdict,
  issueUrl,
  runId,
  onError,
}: {
  projectName: string;
  tag: string;
  detail: ValidationDetail;
  /** Has any attempt on this version ever produced a verdict? */
  hasVerdict: boolean;
  issueUrl: string | undefined;
  runId: string | undefined;
  onError: (message: string) => void;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const start = useStartValidation(projectName, tag);
  const cancel = useCancelRun(projectName, tag);
  const close = () => setAnchor(null);

  // TWO conditions, and both are facts the server hands over rather than rules
  // the console invents. A revalidation drives whatever is SERVING — the runner
  // resolves its endpoints from the cluster at request time — so only the
  // deployed version can be judged; asking an older one would test code that
  // version never shipped and file the verdict, and any repair work, on its
  // milestone. The endpoint refuses it too; this only stops a reader finding
  // out by clicking.
  //
  // The other refusals (open work, no criteria) stay the server's. Gating on
  // the VERDICT would be a rule the API does not have: re-asking a passed
  // version is exactly what this endpoint is for.
  const notDeployed = !detail.deployed;
  const blocked = detail.live || notDeployed || start.isPending;
  // "Revalidate" only once something has actually answered — the platform's
  // own word for the trigger, and wrong on a version nothing has judged yet,
  // which is a state this page reaches routinely. The icon says the same
  // thing the word does: a start, or a retry — and a retry turns forward,
  // clockwise; the counter-clockwise arrow is undo.
  const startLabel = hasVerdict ? "Revalidate" : "Run validation";
  const StartIcon = hasVerdict ? RotateCw : Play;
  const startFace = (
    <>
      <StartIcon size={15} style={{ marginRight: 10 }} />
      {startLabel}
    </>
  );
  // ADR-0016 decision 7: cancel follows the LIFECYCLE, not run liveness.
  const cancellable = validationIsLive(detail.state);

  return (
    <>
      <IconButton
        aria-label="Validation actions"
        onClick={(e) => setAnchor(e.currentTarget)}
        sx={{ border: 1, borderColor: "divider" }}
      >
        <Ellipsis size={16} />
      </IconButton>
      <Menu anchorEl={anchor} open={Boolean(anchor)} onClose={close}>
        {/* Cancel, then (re)start, then the links — the builds menu's order,
            so a reader who learned one menu finds the same item in the same
            place on the other. */}
        <MenuItem
          disabled={!cancellable || !runId || cancel.isPending}
          onClick={() => {
            if (!cancellable || cancel.isPending) return;
            if (runId) {
              cancel.mutate(runId, {
                onError: (e) => onError(e instanceof Error ? e.message : String(e)),
              });
            }
            close();
          }}
        >
          <X size={15} style={{ marginRight: 10 }} />
          Cancel run
        </MenuItem>

        {/* Wrapped ONLY while refused. MenuList walks its own children to move
            focus, so a permanent tooltip span between it and the item takes the
            page's main action off the keyboard entirely; a refused item is not
            focusable anyway, and the span is what lets it still explain
            itself — a disabled MenuItem swallows the hover the tooltip needs. */}
        {blocked ? (
          <Tooltip title={triggerRefusal(detail)}>
            <span>
              <MenuItem disabled>{startFace}</MenuItem>
            </span>
          </Tooltip>
        ) : (
          <MenuItem
            onClick={() => {
              start.mutate(undefined, {
                onError: (e) => onError(e instanceof Error ? e.message : String(e)),
              });
              close();
            }}
          >
            {startFace}
          </MenuItem>
        )}

        <Divider />

        <MenuItem
          disabled={!issueUrl}
          onClick={() => {
            if (issueUrl) window.open(issueUrl, "_blank", "noopener,noreferrer");
            close();
          }}
        >
          <GitHub size={15} style={{ marginRight: 10 }} />
          View validation issue on GitHub
        </MenuItem>

        <MenuItem
          disabled={!runId}
          onClick={() => {
            if (runId) void navigator.clipboard?.writeText(runId);
            close();
          }}
        >
          <Copy size={15} style={{ marginRight: 10 }} />
          Copy run ID
        </MenuItem>
      </Menu>
    </>
  );
}

/**
 * Why the trigger is disabled, or "" when it is not.
 *
 * A disabled item with no reason is a dead control, and these two are the only
 * refusals the console can state before the call — the rest come back as the
 * server's own sentences in the page's error slot.
 */
function triggerRefusal(detail: ValidationDetail): string {
  if (detail.live) return "A run is already working this version.";
  if (!detail.deployed) {
    return "Only the deployed version can be validated — this one is not what is running.";
  }
  return "";
}

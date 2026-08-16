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

import {
  Alert,
  alpha,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Stack,
  Tooltip,
} from "@wso2/oxygen-ui";
import { FileText, GitPullRequest, ScrollText, X } from "@wso2/oxygen-ui-icons-react";
import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import {
  parseValidationCriteria,
  parseValidationReport,
  tallyCriterionStates,
  ValidationView,
  type CriterionTally,
} from "@aep/ui-validation-view";
import { PageHeader, type PageHeaderStatus } from "../../../components/PageHeader";
import type { StatusTone } from "../../../components/StatusChip";
import { EmptyState } from "../../../components/EmptyState";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuildRuns, useCancelRun } from "../../builds/api/queries";
import { RunFeed } from "../../builds/components/RunFeed";
import { isTerminalRun } from "../../builds/lib/runView";
import {
  validationState,
  validationView,
  type StageTone,
} from "../../projects/lib/pipeline";
import { useValidationCriteria, useValidationReport } from "../api/queries";
import { VerdictTile } from "./VerdictTile";

// Validation lives on the DEPLOYMENT surface because the deployment is what is
// being validated. Its verdict is a RUN property — read from the version's run
// story (list-build-runs), which is the only place the platform keeps it; there
// is no validation endpoint. The page joins that verdict with the authored
// oracle (specs/validation/validation-criteria.json) and the runner's committed
// report, both read at HEAD through the Files API.

// The validation cycle is the phase of the run this page owns; the rest of the
// loop is the Builds page's story.
const VALIDATION_CYCLE = ["validation"] as const;

// The run origins that ask a version's acceptance criteria — the console's mirror
// of delivery.RunValidates. A spec build validates the version it delivered, and a
// revalidation exists to ask again; an incident adoption is absent on purpose,
// because it fixes one thing in an already-judged version.
//
// It matters that this is an ORIGIN test and not "does the run have a validation
// cycle": a spec build with no criteria authored settles `skipped` and opens no
// cycle, and that is still the version's answer — the one the "not validated"
// empty state below is written for.
const VALIDATING_ORIGINS: readonly string[] = ["spec-build", "revalidate"];

// StageTone → StatusTone. The two unions differ only in `ghost`, which the shared
// validation mapper never returns; it is mapped for exhaustiveness only.
const TONE_TO_STATUS: Record<StageTone, StatusTone> = {
  ghost: "neutral",
  neutral: "neutral",
  info: "info",
  warning: "warning",
  success: "success",
  error: "error",
};

// Header chip for the version's validation state. DERIVED from the shared mapper
// rather than restating its cases, so this page's chip cannot drift from the
// deployments board's — the drift that left `partial`, `inconclusive` and
// `unreported` chipless here while the board named them correctly, and later left
// this page reading "Validation failed" while the board correctly read "awaiting
// fix" for the same run.
function headerChip(view: ReturnType<typeof validationView>): PageHeaderStatus | undefined {
  if (!view) return undefined;
  const lead = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
  return {
    // The shared labels are lowercase for mid-sentence use; the chip leads.
    label: lead(view.label),
    // This chip stands ALONE at the top of the page — nothing beside it spells out
    // what "Validated*" hedges — so the two mark-bearing labels need their spoken
    // form here. Run through the same capitalization rather than pre-cased at the
    // mapper, so one casing rule covers both names. A no-op for the seven states
    // that carry no spoken form.
    ...(view.spoken ? { spokenLabel: lead(view.spoken) } : {}),
    tone: TONE_TO_STATUS[view.tone],
  };
}

// The oracle joined with the run's report, as counts. Parsed here rather than
// reaching into ValidationView's internals: the tile's copy names run concepts
// (`validation-unreported`, the milestone staying open) that the shared view
// package knows nothing about, and a second JSON.parse of a few-KB file inside a
// useMemo is a cheaper price than teaching that package about runs.
function useTally(
  criteria: string | undefined,
  report: string | undefined,
): CriterionTally | undefined {
  return useMemo(() => {
    if (!criteria) return undefined;
    const oracle = parseValidationCriteria(criteria);
    if ("kind" in oracle) return undefined;
    const parsed = report ? parseValidationReport(report) : undefined;
    const statuses = parsed && !("kind" in parsed) ? parsed : undefined;
    return tallyCriterionStates(oracle, statuses);
  }, [criteria, report]);
}

/**
 * The Validation page: a read-only report of the deployed system against its
 * acceptance criteria, plus the validation cycle's live log. No writes.
 */
export function ValidationPage({
  projectName,
  view,
  onViewChange,
}: {
  projectName: string;
  view: "logs" | undefined;
  onViewChange: (view: "logs" | undefined) => void;
}) {
  const status = useProjectStatus(projectName);
  const deploy = status.data?.deploy;

  // The version this page is about is the NEWEST run's — the same run
  // `deploy.validation` (the chip) describes.
  //
  // NOT deploy.version, which names the newest SUCCEEDED run. Validation is the
  // last cycle before a run settles, so while it is in flight the run is still
  // `running` and deploy.version names either nothing (the first version) or the
  // PREVIOUS one. Keyed on that, this page said "No validation has run yet" about
  // a validation that was running, and on any later build would have shown the
  // previous version's report under a chip reading "Validating".
  const version = status.data?.build.version ?? "";

  // The version's runs, newest first. A milestone sees SEQUENTIAL runs across its
  // life and only some of them validate, so "the newest run" is not this page's
  // subject. TWO different questions are asked of that list, and conflating them is
  // the bug this page had:
  //
  //   who OWNED the question  → by origin. Its verdict is the version's answer,
  //                             including when the answer is `skipped` because no
  //                             criteria were ever authored.
  //   who ANSWERED it         → by cycles. Those carry the report and the logs.
  //
  // Keying both on the newest run meant one adopted issue erased a version's whole
  // validation record: an incident adoption never validates, and settle stamps
  // `skipped` on it, which sent a genuinely PASSED version to the "not validated"
  // empty state and stopped the report being fetched at all. A revalidation is the
  // same shape from the other side — it validates and nothing else, so the run that
  // DELIVERED the version stops being the newest.
  const runs = useBuildRuns(projectName, version || undefined);
  const runList = runs.data?.runs ?? [];
  // Origins that ask the question at all — delivery/RunValidates, in the console's
  // terms. An incident run is deliberately absent: it fixes one thing in a version
  // already judged, and re-validating the system for it would price every incident
  // like a release.
  const run = runList.find((r) => VALIDATING_ORIGINS.includes(r.origin));
  // The verdict VALUE drives every decision below. Deriving them from the chip's
  // rendered label instead (as this page used to) breaks silently the moment the
  // copy changes — and swapping in the shared mapper changes its casing.
  const rawVerdict = run?.validation?.verdict ?? "";
  const reportPath = run?.validation?.reportPath ?? "";
  // What to SAY, which is not the same as what the run last concluded. A fatal
  // verdict on a live run is mid-loop: the platform files the failures as work and
  // validates again, so the column alone would announce a terminal failure over a
  // version the platform is repairing. The lifecycle half of that lives only on
  // deploy.validation, and joining the two is the shared mapper's job so this page
  // and the deployments board cannot disagree about the same run.
  const state = validationState(deploy?.validation ?? "", rawVerdict);
  const verdict = validationView(state);
  // Every run that actually produced an attempt, OLDEST first — the version's
  // chronology. Separate from `run` above because a run can own the question
  // without having answered it yet (a revalidation mid-flight), and because the
  // attempts that matter may span several runs.
  const attemptRuns = runList
    .filter((r) => (r.cycles ?? []).some((c) => c.kind === "validation"))
    .reverse();
  // Every attempt across the whole version, oldest first. The LAST is what the page
  // is about — not the first, and not the newest run's. A version can be judged more
  // than once (a failed attempt is repaired and re-validated; a revalidation asks
  // again later), so pairing an older attempt's merge commit with the current verdict
  // would show the wrong report.
  const validationCycles = attemptRuns.flatMap((r) =>
    (r.cycles ?? []).filter((c) => c.kind === "validation"),
  );
  const validationCycle = validationCycles.at(-1);
  // The report is pinned to the last attempt that MERGED, which is not always the
  // last attempt. A repeat attempt in flight has no report yet by definition, and its
  // cycle record carries no mergeSha — pinning to it passes an empty ref, which
  // degrades to a branch-tip read, the one thing this pin exists to prevent. The tip
  // happens to hold the previous attempt's report until the new one merges, so the
  // bug returns the right bytes by accident and would stop the moment anything else
  // wrote the path.
  const reportCycle = validationCycles.filter((c) => c.mergeSha).at(-1);
  // The cycle carries the pull request's page as the webhook reported it. This
  // page used to build one from the project's repoUrl and the number, which is a
  // CLONE url — a `.git` suffix produced a link that 404s.
  //
  // Taken from the LATEST attempt rather than the merged one: mid-repeat the open
  // pull request is the one a reader wants, and it is the one this link is for.
  const prUrl = validationCycle?.prUrl;

  // The run reached an ANSWER — which is not the same as "everything passed", and
  // not the same as "there is a report". Hooks stay unconditional; `enabled` gates
  // them.
  const settled = rawVerdict !== "" && rawVerdict !== "skipped";
  // `unreported` MEANS no report was committed at that commit, and the server
  // omits reportPath for it. Requesting the file anyway would 404 to rediscover
  // what the verdict already told us, and land the reader on a vague "wasn't
  // found" note instead of the tile that explains the breach.
  const missingReport = rawVerdict === "unreported";
  const criteria = useValidationCriteria(projectName, version, settled);
  // Pinned to the merge commit of the attempt that produced it. Reading the branch
  // tip would show whichever run last overwrote the path — so an older run in the
  // story would display the newest run's results, and a run that committed no report
  // would silently inherit its predecessor's.
  const report = useValidationReport(
    projectName,
    version,
    settled && !missingReport,
    reportPath,
    reportCycle?.mergeSha,
  );
  const tally = useTally(criteria.data?.content, report.data?.content);

  // The run this page can still cancel. Taken from the whole list rather than
  // from `run` above, because only ONE run on a milestone can be live and it is
  // not necessarily the one answering for the version — a revalidation in flight
  // is live while the spec build that owns the current verdict is long settled.
  const liveRun = runList.find((r) => !isTerminalRun(r.state));
  const cancel = useCancelRun(projectName, version || undefined);
  // Cancel is ACCEPTED, not performed: the endpoint answers 202 the moment the
  // signal is queued, and the run turns cancelled only once the supervisor acts
  // and the runs poll observes it. isPending covers the HTTP round trip alone, so
  // this holds the button from the click until the run leaves the live state —
  // released by an error, the one case where clicking again is right.
  //
  // It stores the RUN, not a boolean. This page outlives the run it is watching:
  // a version can be re-judged, so one cancelled run is followed by another live
  // one on the same mounted page, and a latched boolean would leave the new run's
  // button disabled until a reload.
  const [cancelRequestedFor, setCancelRequestedFor] = useState<string | null>(null);
  const cancelling =
    cancel.isPending || (cancelRequestedFor === liveRun?.id && !cancel.isError);

  // Body rule: the log shows while there is no report to show at all, or the reader
  // asked for it. Nothing else — a state that forces the log makes the "View report"
  // button inert, because `?view=logs | absent` has no third value for a default to
  // yield to, so `onViewChange(undefined)` cannot outrank it.
  //
  // A repeat attempt in flight is NOT such a state, though it briefly was. Its
  // predecessor's report is real and is what the reader wants while the fix is being
  // re-checked; that it belongs to the previous attempt is said by the tile, in the
  // sentence and again in the tally.
  const showLogs = !settled || view === "logs";

  // The verdict tile stays visible in BOTH bodies — a verdict does not stop being
  // true because the reader switched to the log. `state` is what it leads with, so
  // a repair in flight reads as one instead of as a run that stopped.
  const tile = settled ? (
    <VerdictTile verdict={rawVerdict} state={state} {...(tally ? { tally } : {})} />
  ) : null;

  const chip = headerChip(verdict);
  const header = (
    <PageHeader
      title="Validation"
      backTo={{
        link: <Link to="/projects/$projectName" params={{ projectName }} />,
        label: "Back to Overview",
      }}
      {...(chip ? { status: chip } : {})}
      actions={
        <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
          {/* A validating run has the same escape hatch the Builds rail gives a
              coding one — same endpoint, same hook, same wording, because it is
              the same act on the same run. Absent once the run is terminal: the
              whole point of cancel is that the unbounded wait has no other
              expiry, and a settled run has nothing left to expire. */}
          {liveRun && (
            <Button
              size="small"
              color="inherit"
              variant="outlined"
              startIcon={<X size={16} />}
              disabled={cancelling}
              onClick={() => {
                setCancelRequestedFor(liveRun.id);
                cancel.mutate(liveRun.id);
              }}
              sx={{
                borderRadius: 999,
                color: "text.primary",
                borderColor: (t) => alpha(t.palette.text.primary, 0.3),
                "&:hover": {
                  borderColor: (t) => alpha(t.palette.text.primary, 0.55),
                },
              }}
            >
              {cancelling ? "Cancelling…" : "Cancel run"}
            </Button>
          )}
          {prUrl && (
            <Tooltip title="Open the validation PR">
              <IconButton
                component="a"
                href={prUrl}
                target="_blank"
                rel="noreferrer"
                aria-label="Validation pull request"
              >
                <GitPullRequest size={18} />
              </IconButton>
            </Tooltip>
          )}
          {settled &&
            (showLogs ? (
              <Button
                size="small"
                variant="outlined"
                startIcon={<FileText size={16} />}
                onClick={() => onViewChange(undefined)}
              >
                View report
              </Button>
            ) : (
              <Button
                size="small"
                variant="outlined"
                startIcon={<ScrollText size={16} />}
                onClick={() => onViewChange("logs")}
              >
                View logs
              </Button>
            ))}
        </Stack>
      }
    />
  );

  // A failed cancel rides WITH the header rather than in one body, because every
  // branch below renders the header and any of them can be on screen when the
  // write fails. The copy is the Builds rail's: a 503 here means the workflow
  // engine was unreachable and nothing was cancelled, so retrying is the fix.
  const headerWithCancelError = (
    <>
      {header}
      {cancel.isError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {cancel.error instanceof Error
            ? cancel.error.message
            : "Failed to cancel the run"}
          . Nothing was cancelled — you can retry.
        </Alert>
      )}
    </>
  );

  if (status.isPending || (version !== "" && runs.isPending)) {
    return (
      <>
        {headerWithCancelError}
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading validation" />
        </Box>
      </>
    );
  }

  if (status.isError) {
    return (
      <>
        {headerWithCancelError}
        <Alert
          severity="error"
          action={<Button onClick={() => void status.refetch()}>Retry</Button>}
        >
          Failed to load validation
          {status.error instanceof Error && status.error.message
            ? `: ${status.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  // Nothing to show: no deployed version, or its run never reached validation.
  //
  // Keyed on the RAW verdict rather than the rendered state, which now carries the
  // lifecycle: `running` can arrive from the status poll an interval before the cycle
  // record reaches the run story, and counting that as "something to show" would
  // render an empty body under a "Validating" chip instead of this.
  if (!run || (!validationCycle && !validationView(rawVerdict))) {
    return (
      <>
        {headerWithCancelError}
        <EmptyState
          compact
          description="No validation has run yet — it runs automatically once the project's components are deployed to dev and the version's work is done."
        />
      </>
    );
  }

  if (rawVerdict === "skipped") {
    return (
      <>
        {headerWithCancelError}
        <EmptyState
          compact
          description="This version was not validated — it has no validation criteria, or it was an incident run, which gets no validation cycle."
        />
      </>
    );
  }

  // The two bodies, computed rather than returned, so ONE container below owns
  // every inset and every gap. Each used to return its own fragment and rely on
  // whatever spacing its children happened to carry: the report body got 24px from
  // ValidationView's own padding, the log feed got none, and the tile inset itself
  // — so the log sat 24px outside the tile and butted straight against it.
  // One feed per validating run, OLDEST first, so the version reads as a
  // chronology of attempts rather than only its latest.
  //
  // A feed per run rather than one stream over the milestone because the progress
  // endpoint is run-keyed, and the cost of that is near zero here: a settled run's
  // stream is finite — the server sends `done` and closes, and the client stops
  // without reattaching — so every historical attempt opens briefly and closes,
  // leaving at most ONE connection held open, since only the newest run can be live.
  const body = showLogs ? (
    <Stack spacing={2}>
      {attemptRuns.map((r) => (
        <RunFeed
          key={r.id}
          projectName={projectName}
          runId={r.id}
          cycleKinds={VALIDATION_CYCLE}
        />
      ))}
    </Stack>
  ) : criteria.isPending || (!criteria.isError && !criteria.data) ? (
    <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
      <CircularProgress aria-label="Loading validation report" />
    </Box>
  ) : criteria.isError ? (
    <Alert
      severity="error"
      action={<Button onClick={() => void criteria.refetch()}>Retry</Button>}
    >
      Failed to load the validation criteria
      {criteria.error instanceof Error && criteria.error.message
        ? `: ${criteria.error.message}`
        : ""}
    </Alert>
  ) : (
    <>
      {/* Only for a verdict that EXPECTED a report. `unreported` already said so,
          in the tile, with its cause — repeating it as a vague note would be
          weaker and say it twice. */}
      {report.isError && !missingReport && (
        <Alert severity="info">
          The run reached a verdict but its report wasn't found — showing the
          criteria without per-criterion results.
        </Alert>
      )}
      {/* The page owns its edges and its width, the same way SpecView says
          `<PageContent fullWidth noPadding>`: this view pads itself and centres a
          960px reading column for the Spec file pane, and a page wants neither —
          no page in this console caps its body, and PageContent already supplies
          the outer cap and the centring. */}
      <ValidationView
        noPadding
        fullWidth
        criteria={criteria.data.content}
        {...(report.data ? { report: report.data.content } : {})}
      />
    </>
  );

  return (
    <>
      {headerWithCancelError}
      {/* A Stack, not margins on the children: VerdictTile renders NOTHING for a
          verdict outside its five, and a Stack given no DOM node for `tile` leaves
          no phantom gap — which a `mb` on the tile could not express. A fragment
          body contributes no node either, so its Alert and the view below it are
          both direct children and both get the same rhythm. */}
      <Stack spacing={3}>
        {tile}
        {body}
      </Stack>
    </>
  );
}

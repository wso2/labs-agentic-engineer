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
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { FileText, ScrollText, X } from "@wso2/oxygen-ui-icons-react";
import { Link } from "@tanstack/react-router";
import { Fragment, useMemo, useState } from "react";
import {
  parseValidationCriteria,
  parseValidationReport,
  tallyCriterionMethods,
  tallyCriterionStates,
  ValidationView,
  type CriterionMethodCount,
  type CriterionTally,
  type ValidationCriteria,
} from "@aep/ui-validation-view";
import { PageHeader, type PageHeaderStatus } from "../../../components/PageHeader";
import type { StatusTone } from "../../../components/StatusChip";
import { EmptyState } from "../../../components/EmptyState";
import { GitHubRefChip } from "../../../components/GitHubRefChip";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuildRuns, useCancelRun } from "../../builds/api/queries";
import { useValidationLive } from "../hooks/useValidationLive";
import { validationLiveLine } from "../lib/liveLine";
import { RunFeed } from "../../builds/components/RunFeed";
import { isTerminalRun } from "../../builds/lib/runView";
import {
  validationState,
  validationView,
  type StageTone,
} from "../../projects/lib/pipeline";
import { useTask } from "../../tasks/api/queries";
import { useValidationCriteria, useValidationReport } from "../api/queries";
import {
  answeredRun,
  isRepairing,
  lastMergedValidationCycle,
  validatingRun,
} from "../lib/runs";
import { ApiRequestError } from "../../../api/errors";
import { PendingTile } from "./PendingTile";
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

// The two LIFECYCLE values in the validation vocabulary — the states that mean a run
// is live BECAUSE OF validation. `running` is a validation cycle in flight;
// `awaiting-fix` is the coding cycle repairing what one found, which is still the
// validation loop and still the unbounded wait cancel exists to expire. Every other
// value in the enum is a verdict, and a verdict is something the run already reached.
const VALIDATION_LIFECYCLE_STATES = new Set(["running", "awaiting-fix"]);

// Hoisted rather than written inline: an sx literal is a new object every render,
// which emotion has to re-serialize each time.
const CAPTION_SX = {
  display: "block",
  mb: 1,
  fontWeight: 700,
  letterSpacing: "0.08em",
  color: "text.secondary",
} as const;

/**
 * The line over the version's earlier validation runs.
 *
 * Drawn at the RUN boundary rather than between individual attempts, which is where
 * the Builds page draws its own ("EARLIER RUNS OF V1", `RunHistoryList`). That keeps
 * the caption on a boundary this page already owns — between feeds — so no feed has
 * to know what is rendered above it.
 *
 * Local, and matching the Builds page's captions by hand: three copies of this markup
 * now exist, and they should collapse into a shared component once a fourth caller
 * appears rather than dragging two Builds-page files into a validation change.
 */
function EarlierRunsCaption() {
  return (
    <Typography variant="caption" sx={CAPTION_SX}>
      EARLIER VALIDATION RUNS
    </Typography>
  );
}

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

// The authored oracle. Parsed here rather than reaching into ValidationView's
// internals: the tiles' copy names run concepts (`validation-unreported`, the
// milestone staying open) that the shared view package knows nothing about, and a
// second JSON.parse of a few-KB file inside a useMemo is a cheaper price than
// teaching that package about runs. A parse failure is `undefined` — the view below
// renders the error; the tiles simply say less.
function useOracle(criteria: string | undefined): ValidationCriteria | undefined {
  return useMemo(() => {
    if (!criteria) return undefined;
    const parsed = parseValidationCriteria(criteria);
    return "kind" in parsed ? undefined : parsed;
  }, [criteria]);
}

// The oracle joined with the run's report, as counts — what the verdict tile
// explains once an attempt has answered.
function useTally(
  oracle: ValidationCriteria | undefined,
  report: string | undefined,
): CriterionTally | undefined {
  return useMemo(() => {
    if (!oracle) return undefined;
    const parsed = report ? parseValidationReport(report) : undefined;
    const statuses = parsed && !("kind" in parsed) ? parsed : undefined;
    return tallyCriterionStates(oracle, statuses);
  }, [oracle, report]);
}

// The oracle alone, by method — what the pending tile says while the first attempt
// is still running, when there is no report to count.
function useMethods(
  oracle: ValidationCriteria | undefined,
): CriterionMethodCount[] | undefined {
  return useMemo(
    () => (oracle ? tallyCriterionMethods(oracle) : undefined),
    [oracle],
  );
}

/**
 * The Validation page: a read-only report of the deployed system against its
 * validation criteria, plus the validation cycle's live log. No writes.
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
  // PREVIOUS one. Keyed on that, this page said "Nothing validated yet" about
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
  // Whether ANY run on this version ever asked the question — the test the "not
  // validated" empty state below is written for. An incident run is deliberately
  // absent from the origins: it fixes one thing in a version already judged, and
  // re-validating the system for it would price every incident like a release.
  const run = validatingRun(runList);
  // The verdict and its report come from the run that ANSWERED, which a revalidation
  // makes a different row from the one being asked: it enters the loop at validation
  // with an empty verdict while the delivering run still holds the version's result.
  const answered = answeredRun(runList);
  const rawVerdict = answered?.validation?.verdict ?? "";
  const reportPath = answered?.validation?.reportPath ?? "";
  // Whether the attempt in flight is REPAIRING that verdict or re-asking it — the
  // difference between the self-heal loop (one run, repeating) and a revalidation.
  const repairing = isRepairing(runList);
  // What to SAY, which is not the same as what the run last concluded. A fatal
  // verdict on a live run is mid-loop: the platform files the failures as work and
  // validates again, so the column alone would announce a terminal failure over a
  // version the platform is repairing. The lifecycle half of that lives only on
  // deploy.validation, and joining the two is the shared mapper's job so this page
  // and the deployments board cannot disagree about the same run.
  const state = validationState(deploy?.validation ?? "", rawVerdict);
  const verdict = validationView(state);
  // Every run that actually produced an attempt, held in BOTH orders because the page
  // needs both and confusing them would be a silent bug. Separate from `run` above
  // because a run can own the question without having answered it yet (a revalidation
  // mid-flight), and because the attempts that matter may span several runs.
  //
  // Newest first, exactly as list-build-runs answers: the order the logs are DRAWN in,
  // so the attempt a reader came for leads the page.
  const feedRuns = runList.filter((r) =>
    (r.cycles ?? []).some((c) => c.kind === "validation"),
  );
  // Oldest first — the version's chronology. Every derivation below reads it through
  // `.at(-1)` to mean "the latest attempt", so this order is load-bearing.
  const attemptRuns = [...feedRuns].reverse();
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
  const reportCycle = lastMergedValidationCycle(runList);
  // The cycle carries the pull request's page as the webhook reported it. This
  // page used to build one from the project's repoUrl and the number, which is a
  // CLONE url — a `.git` suffix produced a link that 404s.
  //
  // Taken from the LATEST attempt rather than the merged one: mid-repeat the open
  // pull request is the one a reader wants, and it is the one this link is for.
  // The number rides along because the link STATES which pull request it opens: two
  // GitHub chips sit side by side here, and the log below repeats one of them.
  const prUrl = validationCycle?.prUrl;
  const prNumber = validationCycle?.prNumber ?? 0;
  // The validation issue is what FRAMED the attempt the PR above answers, so both
  // links are read off the same cycle and describe the same attempt.
  //
  // "Latest" is a formality here: EnsureValidationIssue is keyed by MILESTONE and
  // reopens the existing issue for a repeat attempt rather than minting a second
  // one, so every cycle on this page carries the same number. That is also why the
  // issue belongs in the header alone while the pull request repeats per cycle — one
  // issue per version, one PR per attempt.
  const issueNumber = validationCycle?.validationIssue ?? 0;
  // Only the NUMBER is on the wire; the cycle record has no issue URL. Asked of
  // get-task rather than composed from the project's repoUrl for the reason above:
  // that is a clone url. get-task serves this issue even though list-tasks hides it
  // — a detail read by number deliberately skips the population filter — and answers
  // with GitHub's own url. The hook is a no-op while the number is 0.
  const issue = useTask(projectName, issueNumber);
  const issueUrl = issue.data?.issueUrl;

  // The run reached an ANSWER — which is not the same as "everything passed", and
  // not the same as "there is a report". Hooks stay unconditional; `enabled` gates
  // them.
  const settled = rawVerdict !== "" && rawVerdict !== "skipped";
  // The version's FIRST attempt, still in flight: the loop says `running` and nothing
  // has concluded anything yet, so the oracle is the only thing there is to show — and
  // it is worth showing, because it says what is being checked and what will never be
  // checked by an agent at all.
  //
  // Deliberately not every `running` state. A repeat attempt has the previous
  // attempt's verdict and report, which the page renders with its numbers marked as
  // the last attempt's; replacing that with a page of Pending chips would throw away
  // the only results anyone has.
  //
  // This gates the PENDING fallback only. A criterion the current attempt is
  // actually working on gets a live status regardless (see `live` below), which is
  // what un-freezes a repeat attempt without inventing a row that says nothing:
  // "Pending" is a guess about every criterion, `Authoring…` is a fact about one.
  const awaitingFirstVerdict = state === "running" && rawVerdict === "";
  // `unreported` MEANS no report was committed at that commit, and the server
  // omits reportPath for it. Requesting the file anyway would 404 to rediscover
  // what the verdict already told us, and land the reader on a vague "wasn't
  // found" note instead of the tile that explains the breach.
  const missingReport = rawVerdict === "unreported";
  const criteria = useValidationCriteria(
    projectName,
    version,
    settled || awaitingFirstVerdict,
  );
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
  const oracle = useOracle(criteria.data?.content);
  const tally = useTally(oracle, report.data?.content);
  const methods = useMethods(oracle);
  // No oracle was ever authored — the Files API's answer for a version whose spec has
  // no criteria, and the reason its run will settle as `skipped`. Told apart from a
  // read that merely FAILED by the envelope's `code`, so a 500 or a dropped connection
  // still offers a retry instead of announcing there is nothing to validate.
  const criteriaAbsent =
    criteria.error instanceof ApiRequestError &&
    criteria.error.code === "not_found";

  // The run this page can still cancel: one that is live, AND live because of
  // validation.
  //
  // Liveness alone was the bug. Every run is live through its coding cycles, so a
  // first delivery still writing code offered "Cancel run" over a body reading "No
  // validation has run yet" — a button to kill a build on the one page that never
  // mentions it. The status read made exactly this mistake before it learned to
  // consult the run's latest cycle: "a live run whose current cycle is coding, fixing
  // or resolving a conflict has nothing to say about validation yet"
  // (status_stages.go). A live run with no verdict is not a validating run.
  //
  // Gated on the RENDERED STATE rather than on the live run's own cycles, because
  // `awaiting-fix` is not a fact any run row carries — it is the join of the lifecycle
  // with a verdict the loop repairs, and re-deriving it here is how this button and the
  // chip above it would come to disagree. The consequence is the rule worth keeping:
  // cancel is offered exactly while the header chip says the run is still in the loop.
  //
  // Taken from the whole list rather than from `run` above, because only ONE run on a
  // milestone can be live and it is not necessarily the one answering for the version —
  // a revalidation in flight is live while the spec build that owns the current verdict
  // is long settled.
  const liveRun = VALIDATION_LIFECYCLE_STATES.has(state)
    ? runList.find((r) => !isTerminalRun(r.state))
    : undefined;
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

  // Whether this page has a criteria view to offer at all — which is what the header
  // toggle switches to, so the two must be gated on one value or "View report" points
  // at nothing. True for a settled run (its report) and for a first attempt in flight
  // (the oracle, chipped with what is about to happen to it).
  //
  // It stays true when the criteria are ABSENT: the pending tile is then the whole
  // report body, saying why it is empty, and the header reads the same in both
  // running shapes rather than losing a button depending on what the spec authored.
  const canShowReport = settled || awaitingFirstVerdict;

  // Body rule: the log shows while there is no report to show at all, or the reader
  // asked for it. Nothing else — a state that forces the log makes the "View report"
  // button inert, because `?view=logs | absent` has no third value for a default to
  // yield to, so `onViewChange(undefined)` cannot outrank it.
  //
  // A repeat attempt in flight is NOT such a state, though it briefly was. Its
  // predecessor's report is real and is what the reader wants while the fix is being
  // re-checked; that it belongs to the previous attempt is said by the tile, in the
  // sentence and again in the tally.
  const showLogs = !canShowReport || view === "logs";

  // What the validating run is doing to each criterion right now. Opened only for
  // the report body: the log body already streams this same run through RunFeed,
  // and these statuses are rendered on the rows, which that body does not show.
  //
  // `liveRun` rather than the run answering for the version — only one run on a
  // milestone can be live, and a revalidation in flight is a different row from
  // the one holding the current verdict. The fold itself returns nothing unless
  // that run's newest validation cycle is still OPEN, so a repair cycle busy
  // writing code contributes no statuses and the previous report stands.
  const live = useValidationLive(projectName, liveRun?.id, !showLogs);
  // Said above the rows, and only in the two windows where the rows say nothing:
  // before any criterion has been picked up, and after they have all settled but
  // the report has not landed. Derived from the rows so it cannot contradict them.
  // Gated on a validation cycle actually being OPEN, not merely on there being no
  // statuses. Without that, a settled `unreported` verdict (whose report is never
  // fetched) and a repair cycle busy writing code both look identical to a run
  // that has not started, and the tile announced "Setting up the test harness…"
  // over a run that had finished or was doing something else entirely.
  const liveNote = live.active
    ? validationLiveLine(oracle, live.statuses, report.data !== undefined)
    : "";

  // The tile stays visible in BOTH bodies — a verdict does not stop being true
  // because the reader switched to the log, and neither does an attempt still being
  // under way. `state` is what the verdict tile leads with, so a repair in flight
  // reads as one instead of as a run that stopped.
  const tile = settled ? (
    <VerdictTile
      verdict={rawVerdict}
      state={state}
      repairing={repairing}
      {...(tally ? { tally } : {})}
      {...(liveNote ? { note: liveNote } : {})}
    />
  ) : awaitingFirstVerdict ? (
    <PendingTile
      noCriteria={criteriaAbsent}
      {...(methods ? { methods } : {})}
      {...(liveNote ? { note: liveNote } : {})}
    />
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
              the same act on the same run. Absent unless validation is what keeps
              the run alive: the whole point of cancel is that the unbounded wait
              has no other expiry, and a run that has answered — or has not reached
              the question yet — has nothing here left to expire. */}
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
          {/* Before the pull request, because the issue frames the work and the PR
              answers it — GitHub's own ordering. Absent when no cycle has minted an
              issue yet, and equally when the read that resolves its url failed:
              same rule as the PR beside it, which shows nothing rather than a link
              it cannot aim. */}
          {issueUrl && (
            <GitHubRefChip
              kind="issue"
              number={issueNumber}
              url={issueUrl}
              name="Validation issue"
              tooltip="Open the validation issue"
            />
          )}
          {/* Named "Validation …" rather than the bare default because the log below
              carries a chip for the same pull request — this one answers "the PR for
              this validation", that one "the PR this cycle produced". */}
          {prUrl && prNumber > 0 && (
            <GitHubRefChip
              kind="pull"
              number={prNumber}
              url={prUrl}
              name="Validation pull request"
              tooltip="Open the validation PR"
            />
          )}
          {canShowReport &&
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
          description="Nothing validated yet. After a build, your software is checked against the validation criteria in your spec; results appear here."
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
  // One feed per validating run, NEWEST first. The newest attempt is the one a reader
  // opened this view for — it is the one still being written — so it leads rather than
  // sitting below however much history the version accumulated. The version still
  // reads as a chronology: the boxes are numbered from the oldest, so the numbers
  // count down the page.
  //
  // A feed per run rather than the version-wide stream, and the reason is this
  // view's ordering rather than what the server can serve. `stream-build-progress`
  // does span a version's runs, but it emits them OLDEST first — a chronology —
  // while this page leads with the newest attempt on purpose. Per-run feeds are
  // also near free here: a settled run's stream is finite — the server sends
  // `done` and closes, and the client stops without reattaching — so every
  // historical attempt opens briefly and closes, leaving at most ONE connection
  // held open, since only the newest run can be live.
  const body = showLogs ? (
    <Stack spacing={2}>
      {feedRuns.map((r, i) => (
        <Fragment key={r.id}>
          {/* Before the SECOND feed, so the caption separates the run being read
              from the runs that came before it. Never rendered for a version
              validated by a single run, which is the ordinary case. */}
          {i === 1 && <EarlierRunsCaption />}
          <RunFeed
            projectName={projectName}
            runId={r.id}
            cycleKinds={VALIDATION_CYCLE}
            // Counted from the OLDEST validating run, like the cycle ordinal inside
            // the feed, so both numbers descend together down the page. Counted over
            // the runs this PAGE shows rather than the milestone's whole run list: a
            // run that never validated has no box here, so numbering the full list
            // would print "Run 3" and "Run 1" with no Run 2 anywhere. Nothing can
            // disagree with it either — no other surface numbers runs at all.
            runNumber={feedRuns.length - i}
            // Only the newest run may open a box, so exactly one log is open on the
            // page rather than one per feed — and it is the one still being written.
            expandNewest={i === 0}
          />
        </Fragment>
      ))}
    </Stack>
  ) : criteriaAbsent ? (
    // Nothing under the tile, which has already said there are no criteria and is
    // the whole body here. An empty report frame beneath it would be a heading over
    // nothing, and the reader's next move — the log — is one button away.
    null
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
        hideDescription
        // A first attempt in flight has no report by definition, so every row says
        // what is ABOUT to happen to it instead of nothing at all.
        awaitingReport={awaitingFirstVerdict}
        criteria={criteria.data.content}
        {...(report.data ? { report: report.data.content } : {})}
        live={live.statuses}
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

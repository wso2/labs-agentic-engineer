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

import { useMemo } from "react";
import { Alert, alpha, Box, Chip, Tooltip, Typography } from "@wso2/oxygen-ui";
import { Check, Sparkles, User, X } from "@wso2/oxygen-ui-icons-react";
import {
  parseValidationCriteria,
  type Criterion,
  type Requirement,
  type ValidationCriteria,
} from "./parse.js";
import {
  parseValidationReport,
  type CriterionReport,
  type ValidationReport,
} from "./report.js";
import { CRITERION_STATE_LABEL, runAnswers, runWorksOn } from "./counts.js";
import { shortCriterionId, shortRequirementId } from "./shortId.js";

// An accessible name must come from CONTENT here: an aria-label on a roleless
// element is ignored. Local because this package cannot reach the console's copy
// and @wso2/oxygen-ui does not re-export MUI's `visuallyHidden`.
const VISUALLY_HIDDEN = {
  position: "absolute",
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
  border: 0,
} as const;

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

/**
 * The row's first column, which holds the method glyph and nothing else, on every
 * surface. One narrow fixed width, so the ids beside it line up and the failure
 * block has something to measure its indent from.
 *
 * Narrow is the point: the verdict sits at the row's far end instead, so no chip
 * label can widen this column and none of them leaves slack in it. Raw px, because
 * a glyph's box is not a spacing step.
 */
const GUTTER = 22;

/**
 * The row's flex gap, as a theme spacing multiplier (`1` is 8px). A real spacing
 * step, so the theme owns it; the failure indent adds it to a px width by reading
 * the same token back through `theme.spacing`.
 */
const ROW_GAP = 1;

/**
 * The height of a row's first line. Every occupant takes this height and centres
 * its content in it, and the assertion takes it as `line-height`, so all three sit
 * in one band even when the assertion wraps. 24px is the small Chip's own height.
 *
 * Not `align-items: baseline`: a Chip is `inline-flex`/`center`, so it has no
 * in-flow line box and CSS synthesises its baseline from the bottom margin edge.
 * `line-height` needs the explicit `px` — bare 24 means 24x the font size.
 */
const ROW_LINE = 24;

/**
 * A requirement number or criterion letter on a soft neutral ground — a ground
 * rather than `1.`/`a)` because these are names, not positions: ids are stable by
 * contract (a spec file is named after its criterion), so a deleted requirement
 * leaves a gap that reads fine as names and as a fault as a list.
 *
 * `full` shows on hover when it differs — the handle for spec filenames,
 * report.json and naming a criterion to the agent, none of which take `short`.
 */
function IdMark({ short, full }: { short: string; full: string }) {
  const mark = (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        // The shared band, not padding of its own — see ROW_LINE.
        height: ROW_LINE,
        minWidth: 20,
        px: 0.75,
        borderRadius: 0.75,
        flexShrink: 0,
        bgcolor: theme.palette.action.hover,
        color: "text.secondary",
        ...mono,
      })}
    >
      {short}
    </Box>
  );
  return short === full ? mark : <Tooltip title={full}>{mark}</Tooltip>;
}

// The MUI/Oxygen Chip color union — kept local so the state map stays typed.
type ChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "error"
  | "info"
  | "success"
  | "warning";

// report.json status → chip colour. The LABEL comes from CRITERION_STATE_LABEL
// (counts.ts), which the consumer's tally reads too, so a row and the verdict tile
// above it cannot name the same status differently. Unknown statuses fall through
// to a neutral chip labelled verbatim.
// An icon for the two TERMINAL verdicts only. They are the answers a run produces
// and the pair a reader has to tell apart at a glance — without one, `Failed` is
// separated from `Passed` by hue alone, which is nothing to a red/green
// colour-blind reader. Every other status is the ABSENCE of an answer rather than
// one of them, so a mark there would compete for the distinction this pair needs.
const STATE_ICON: Record<string, typeof Check> = { pass: Check, fail: X };

/**
 * Shared by every chip a row can carry. Hoisted rather than written inline for the
 * reason GitHubRefChip records: an sx literal is a new object each render, which
 * emotion has to re-serialise.
 *
 * MUI insets a small chip's icon by 4px while its label sits at 8px, so an icon
 * crowds the border in a way no text does. Matching the label's inset makes a chip
 * that carries an icon start its content exactly where one without an icon starts
 * its text. Inert on the chips that never take an icon.
 */
const CHIP_SX = {
  flexShrink: 0,
  "& .MuiChip-icon": { ml: 1 },
} as const;

const STATE_COLOR: Record<string, ChipColor> = {
  pass: "success",
  fail: "error",
  not_run: "default",
  not_validated: "warning",
  manual: "default",
};

/**
 * Who checks a criterion — a mark, not a word.
 *
 * `e2e` takes the console's agent glyph (Sparkles at primary.main, as in the agent
 * chat and the nav), so the row inherits a meaning the reader already has.
 * Everything else falls to the person: neither `manual`, legacy `scenario`, nor
 * the `"unknown"` parse.ts gives a method-less criterion is ever automated, which
 * is why one sentence serves all three.
 */
function methodMark(method: string): {
  Icon: typeof Sparkles;
  color: string;
  title: string;
} {
  return runAnswers(method)
    ? {
        Icon: Sparkles,
        color: "primary.main",
        title: "Validated automatically by the agent.",
      }
    : {
        Icon: User,
        color: "text.secondary",
        title: "Requires manual validation.",
      };
}

/**
 * The gutter's occupant when no run is attached — the Spec view's whole case, and
 * a validation view whose report would not parse.
 *
 * Icon-only, so the sentence is repeated as hidden text. Tooltip does set its
 * title as an `aria-label`, but on a bare span that is ignored, so the name has to
 * come from content.
 */
function MethodIcon({ method }: { method: string }) {
  const { Icon, color, title } = methodMark(method);
  return (
    <Tooltip title={title}>
      {/* The gutter centres this in the shared band, so there is nothing vertical
          to do here. `flex` keeps the svg out of an inline box with its own
          leading. */}
      <Box component="span" sx={{ display: "flex", color, flexShrink: 0 }}>
        <Icon size={16} aria-hidden />
        <Box component="span" sx={VISUALLY_HIDDEN}>
          {title}
        </Box>
      </Box>
    </Tooltip>
  );
}

/**
 * What the report says qualifies a verdict, as one sentence — or nothing.
 *
 * The flags are independent in generate-report.mjs (`flaky` only on a pass,
 * `healed` decided before the status), so a repaired failure and a pass that was
 * both are each real. All four readings are written out rather than concatenated
 * from fragments, which would read as a list of tags.
 */
function verdictNote(
  status: string,
  report: CriterionReport | undefined,
): string | undefined {
  const flaky = report?.flaky ?? false;
  const healed = report?.healed ?? false;
  if (!flaky && !healed) return undefined;
  const verdict = CRITERION_STATE_LABEL[status] ?? status;
  if (flaky && healed) {
    return `${verdict}, but the test was flaky, and the agent repaired it.`;
  }
  if (flaky) return `${verdict}, but the test was flaky.`;
  return status === "fail"
    ? `${verdict}. The agent tried to repair the test.`
    : `${verdict} after the agent repaired the test.`;
}

/**
 * The per-criterion run-state chip (only rendered when a report is joined in).
 *
 * `note` is the report's qualifier on this verdict — flaky, healed, or both — and
 * rides the chip as a single `*`. It qualifies THIS word, so a chip of its own
 * would read as an independent fact and push the verdict out of the aligned
 * column. `string | undefined` rather than optional, since
 * `exactOptionalPropertyTypes` is on.
 */
function StateChip({ status, note }: { status: string; note: string | undefined }) {
  const label = CRITERION_STATE_LABEL[status] ?? status;
  const Icon = STATE_ICON[status];
  const chip = (
    <Chip
      size="small"
      variant="outlined"
      color={STATE_COLOR[status] ?? "default"}
      {...(Icon ? { icon: <Icon size={14} /> } : {})}
      label={
        note === undefined ? (
          label
        ) : (
          <>
            <span aria-hidden>{`${label}*`}</span>
            <Box component="span" sx={VISUALLY_HIDDEN}>
              {note}
            </Box>
          </>
        )
      }
      sx={CHIP_SX}
    />
  );
  return note === undefined ? chip : <Tooltip title={note}>{chip}</Tooltip>;
}

/**
 * What the RUN is doing to a criterion right now, keyed by criterion id.
 *
 * Carried as a plain map rather than folded here, because this package renders
 * and the consumer streams: the console builds it from the run's progress feed
 * (`progress_item` events), and the Spec view — which shows the same oracle with
 * no run attached — simply passes nothing.
 */
export type LiveStatuses = Readonly<Record<string, string>>;

// The in-flight vocabulary, local for the same reason "Pending" below is: these
// words describe work happening, and report.json only speaks in the past tense, so
// none of them belongs in CRITERION_STATE_LABEL. Its terminal words (`pass`/`fail`)
// do arrive on this feed and fall through to StateChip on purpose — a criterion
// that passed is the same fact whichever source said so.
const LIVE_LABEL: Record<string, string> = {
  planned: "Planned",
  exploring: "Exploring…",
  authoring: "Authoring…",
  running: "Running…",
  healing: "Healing…",
};

// Only `healing` is coloured. It is the run saying a criterion that WORKED has
// stopped working — the one live status that changes what a reader thinks is
// happening. Colouring ordinary progress would spend attention on the common case
// and leave nothing to spend on this one.
const LIVE_COLOR: Record<string, ChipColor> = { healing: "warning" };

/** For a criterion the pinned report has no row for — see CriterionChip. */
const DRIFT_LABEL = "No result";
const DRIFT_TOOLTIP =
  "The last validation run produced no result for this criterion.";

// The per-criterion chip while the run is still working on it.
function LiveChip({ status }: { status: string }) {
  return (
    <Chip
      size="small"
      variant="outlined"
      color={LIVE_COLOR[status] ?? "info"}
      label={LIVE_LABEL[status] ?? status}
      sx={CHIP_SX}
    />
  );
}

/**
 * The one chip a criterion's row carries, in precedence order.
 *
 * `manual` is the exception that shapes the order: such a criterion is answered
 * by a person, so a run signal must not speak for it. It skips the live status
 * and lands on the report's own `manual`, or on the same final word `awaiting`
 * would otherwise have given it.
 *
 * Otherwise live beats report, and that ordering is the whole point: a repeat
 * attempt carries the PREVIOUS attempt's report, so ranking the report higher
 * would freeze a criterion on the last run's verdict for the entire time the
 * current run spends re-working it. The report wins again the moment the cycle
 * settles, because the consumer stops supplying live statuses then.
 *
 * Only called when a run IS attached, so it always returns a chip. CriterionRow
 * handles the no-run case, showing who checks the criterion instead of what
 * happened to it.
 */
function CriterionChip({
  criterion,
  report,
  live,
  awaiting,
}: {
  criterion: Criterion;
  report: CriterionReport | undefined;
  live: string | undefined;
  awaiting: boolean;
}) {
  // `runWorksOn`, not `runAnswers`: the question is whether the run is WORKING on
  // this row, not whether it will answer it. The console's run-wide progress line
  // counts the same set, so refusing a status here would contradict it. `manual`
  // is excluded unconditionally — a person answers it, so "Planned" would promise
  // a result nobody is going to produce.
  if (live && runWorksOn(criterion.method)) {
    // pass/fail arrive on the live feed too — report.json's own words, so its chip.
    return LIVE_LABEL[live] ? (
      <LiveChip status={live} />
    ) : (
      <StateChip status={live} note={undefined} />
    );
  }
  if (report) {
    return (
      <StateChip status={report.status} note={verdictNote(report.status, report)} />
    );
  }

  // `runAnswers` here, the narrow question, because this branch is about the
  // VERDICT: a criterion the run will not answer gets its final word instead of
  // "Pending", which would promise a result the report goes on to contradict. The
  // method alone decides that word, which is why it can be said this early.
  //
  // "Pending" is local rather than a sixth CRITERION_STATE_LABEL entry: that map
  // is report.json's vocabulary, and this criterion has no report to name.
  if (awaiting) {
    if (!runAnswers(criterion.method)) {
      return (
        <StateChip
          status={criterion.method === "manual" ? "manual" : "not_validated"}
          note={undefined}
        />
      );
    }
    return (
      <Chip size="small" variant="outlined" label="Pending" sx={CHIP_SX} />
    );
  }

  // No attempt in flight, and the report has no row for this criterion. The
  // consumer reads the criteria at the branch tip and the report at the merge
  // commit of the attempt that wrote it, so a criterion authored since then cannot
  // have a result — the ordinary authoring loop, not a fault, hence neutral rather
  // than `warning`. Ranked below `awaiting` deliberately: claiming a row is out of
  // the run while a run works on it is the one thing this chip must never do.
  return (
    <Tooltip title={DRIFT_TOOLTIP}>
      <Chip
        size="small"
        variant="outlined"
        label={DRIFT_LABEL}
        sx={CHIP_SX}
      />
    </Tooltip>
  );
}

// One acceptance criterion: the method glyph, its letter, the assertion, the
// verdict at the far end when a run is attached, and for a failure the spec path
// and message beneath.
//
// Two marks, because they answer two different questions. The glyph says who
// CHECKS this criterion, which is a standing property of the criterion and true on
// every surface; the chip says what the last run MADE of it, which exists only
// where there is a run. They coincide on a manual criterion, whose verdict is
// "Manual" — the price of keeping "whose job is this" scannable in a fixed column
// on a page full of results, rather than making the reader read every verdict to
// find their own work.
//
// A qualifier like flaky still rides the verdict rather than becoming a third
// mark: it modifies that word and belongs beside it.
function CriterionRow({
  criterion,
  requirementId,
  report,
  live,
  awaiting,
  hasRun,
}: {
  criterion: Criterion;
  /** The card this row sits in, so the letter can drop the prefix it repeats. */
  requirementId: string;
  report: CriterionReport | undefined;
  live: string | undefined;
  awaiting: boolean;
  hasRun: boolean;
}) {
  const failed = report?.status === "fail";
  return (
    // RequirementCard draws the rule that separates rows; it lands on THIS box, so
    // a failure block stays inside the criterion it belongs to instead of being cut
    // off from its own assertion.
    <Box sx={{ py: 1 }}>
      {/* `flex-start` keeps the marks on the FIRST line of an assertion that wraps
          rather than centring them in it. Alignment WITHIN that line is
          ROW_LINE's job. */}
      <Box
        sx={{ display: "flex", gap: ROW_GAP, alignItems: "flex-start" }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            height: ROW_LINE,
            minWidth: GUTTER,
            flexShrink: 0,
          }}
        >
          <MethodIcon method={criterion.method} />
        </Box>
        <IdMark
          short={shortCriterionId(criterion.id, requirementId)}
          full={criterion.id}
        />
        <Typography
          variant="body2"
          sx={{ flexGrow: 1, lineHeight: `${ROW_LINE}px` }}
        >
          {criterion.must}
        </Typography>
        {/* The assertion grows, so the verdict is pushed to the row's far end.
            Boxed to the shared band for the same reason the gutter is, since a
            24px chip beside a 24px line needs saying once at each end. */}
        {hasRun && (
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              height: ROW_LINE,
              flexShrink: 0,
            }}
          >
            <CriterionChip
              criterion={criterion}
              report={report}
              live={live}
              awaiting={awaiting}
            />
          </Box>
        )}
      </Box>
      {/* Full-width beneath the row, indented to where the letter starts, so a
          long trace never crowds the assertion. */}
      {failed && (report?.failureLocation || report?.spec || report?.failure) && (
        <Box
          sx={(theme) => ({
            mt: 0.75,
            ml: `calc(${GUTTER}px + ${theme.spacing(ROW_GAP)})`,
          })}
        >
          {/* Prefer the reporter's `<file>:<line>`, which points at the failing
              assertion rather than the spec containing it. Admitted on its own,
              because a reporter can return a location with an empty message and
              dropping the block would throw away the only pointer there is. */}
          {(report?.failureLocation || report?.spec) && (
            <Typography variant="caption" color="text.secondary" sx={mono}>
              {report.failureLocation || report.spec}
            </Typography>
          )}
          {report?.failure && (
            <Box
              component="pre"
              sx={{
                // `m: 0` first: it is a shorthand, so declaring it after `mt`
                // silently overrode the gap this block is supposed to keep.
                m: 0,
                mt: 0.5,
                p: 1,
                borderRadius: 1,
                // A wash, not a saturated fill: the chip already says "failed",
                // so this surface's only job is to keep the longest text on the
                // page readable. The tint composites over whatever is beneath it,
                // so it holds in both themes.
                bgcolor: (theme) => alpha(theme.palette.error.main, 0.08),
                color: "text.primary",
                fontFamily: "monospace",
                fontSize: "0.75rem",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                maxHeight: 220,
                overflow: "auto",
              }}
            >
              {report.failure}
            </Box>
          )}
        </Box>
      )}
    </Box>
  );
}

function RequirementCard({
  requirement,
  statuses,
  live,
  awaiting,
  hasRun,
}: {
  requirement: Requirement;
  statuses: ValidationReport | undefined;
  live: LiveStatuses | undefined;
  awaiting: boolean;
  hasRun: boolean;
}) {
  const count = requirement.criteria.length;
  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        p: 2,
        // Twice the gap between two criteria, so a requirement boundary outweighs
        // a row boundary and the nesting shows in the rhythm.
        mb: 3,
      }}
    >
      {/* The number leads the statement, as the letter leads the assertion below,
          so the card reads the same way at both levels. Same shared-band idiom, so
          the number stays on the statement's first line when it wraps. */}
      <Box
        sx={{
          display: "flex",
          alignItems: "flex-start",
          gap: ROW_GAP,
          mb: count > 0 ? 1.5 : 0,
        }}
      >
        <IdMark
          short={shortRequirementId(requirement.id)}
          full={requirement.id}
        />
        <Typography
          variant="body1"
          sx={{ fontWeight: 500, lineHeight: `${ROW_LINE}px` }}
        >
          {requirement.statement}
        </Typography>
      </Box>
      {count === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No criteria.
        </Typography>
      ) : (
        // A rule on the TOP of every row, so the statement reads as the card's
        // header and the card's own border closes the list. Bounding
        // all-but-the-last instead leaves the first criterion unbounded above and a
        // one-criterion requirement with no rule at all. Owned here because it is a
        // property of the LIST, not of a row.
        <Box sx={{ "& > *": { borderTop: 1, borderColor: "divider" } }}>
          {requirement.criteria.map((c) => (
            <CriterionRow
              key={c.id}
              criterion={c}
              requirementId={requirement.id}
              report={statuses?.get(c.id)}
              live={live?.[c.id]}
              awaiting={awaiting}
              hasRun={hasRun}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}

function ValidationBody({
  criteria,
  statuses,
  live,
  noPadding,
  fullWidth,
  hideDescription,
  awaitingReport,
}: {
  criteria: ValidationCriteria;
  statuses: ValidationReport | undefined;
  live: LiveStatuses | undefined;
  /** Required, not optional: `exactOptionalPropertyTypes` is on, so the public
   *  props are defaulted at the boundary rather than forwarded as `undefined`. */
  noPadding: boolean;
  fullWidth: boolean;
  hideDescription: boolean;
  awaitingReport: boolean;
}) {
  const { requirements } = criteria;
  /**
   * Whether a RUN is attached, which decides what every row's gutter holds: its
   * status chip, or — with no run to report — who checks it.
   *
   * Read from `statuses`, NOT from the `report` prop. The prop is raw text and
   * parsing it can fail, leaving `statuses` undefined while a report WAS supplied;
   * keying off the prop would then hand every row the drift chip, announcing that
   * all of them post-date the last run when the truth is that the file is
   * unreadable. This way such a view degrades to the plain oracle, with the warning
   * Alert above it naming the real problem.
   */
  const hasRun = statuses !== undefined || awaitingReport;
  const reqCount = requirements.length;
  return (
    // `height`/`overflow` are the file-pane contract and stay unconditional: on a
    // page they are inert (PageContent's inner box has auto height, so the
    // percentage resolves to auto and nothing ever scrolls here). Only `p: 3`
    // renders differently between the two consumers, so only it is switched.
    <Box
      sx={{
        height: "100%",
        overflow: "auto",
        ...(noPadding ? {} : { p: 3 }),
      }}
    >
      <Box sx={fullWidth ? undefined : { maxWidth: 960, mx: "auto" }}>
        <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
          Validation Criteria
        </Typography>

        {/* What this document is, where it comes from, and what happens to it.
            Nothing else in the spec workspace says so, and the reader meets the
            criteria here before any run has produced a result to learn from. */}
        {!hideDescription && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
            Each criterion represents one thing your system must do, based on your
            requirements. After every deployment the ones that can be automated are
            checked against the deployed system, and the results appear under
            Validations. The rest you have to check yourself. To change one, ask the
            agent.
          </Typography>
        )}

        {/* No tally here. Counts belong with the verdict that explains them, which
            the consumer renders above this view; a second copy a screen lower says
            the same numbers twice. The margin separates the heading from the
            list. */}
        <Box sx={{ mt: 3 }}>
          {reqCount === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No validation criteria.
            </Typography>
          ) : (
            requirements.map((r) => (
              <RequirementCard
                key={r.id}
                requirement={r}
                statuses={statuses}
                live={live}
                awaiting={awaitingReport}
                hasRun={hasRun}
              />
            ))
          )}
        </Box>
      </Box>
    </Box>
  );
}

export interface ValidationViewProps {
  /** Raw validation-criteria.json text (the acceptance oracle). */
  criteria: string;
  /**
   * Raw tests/validation/report.json text. When present, per-criterion run
   * state is joined onto the oracle by criterion id and rendered as state chips
   * plus failure detail. Absent → the plain oracle (the Spec-view preview).
   */
  report?: string;
  /**
   * The consumer owns the padding. Default off, because this view's first home is
   * the Spec view's file pane, which hands each renderer an unpadded box — the
   * same contract OpenApiView is written to. A PAGE owns its own edges and its own
   * rhythm, so a page consumer opts out instead of the view guessing.
   */
  noPadding?: boolean;
  /**
   * Fill the consumer's width instead of centring the criteria in a 960px reading
   * column. Default off, for the same reason as `noPadding`: in the Spec view this
   * is a file preview beside a 280px file list, where a measured column reads
   * better than prose stretched across the pane. A console PAGE is the opposite —
   * no page in this app caps its body (see BuildsPage, DeploymentsPage), and
   * PageContent already supplies the outer 1400px cap and the centring.
   *
   * Separate from `noPadding` on purpose: a prop named for padding should not also
   * govern width. Oxygen's own PageContent draws the same line.
   */
  fullWidth?: boolean;
  /**
   * Drop the paragraph explaining what the criteria are. Default off, same reason
   * as the two above: the Spec view is where a reader first meets this document,
   * with nothing else on the page to say what it is for. The Validations page is
   * the opposite — the reader arrived there to read run results, and a sentence
   * telling them results appear under Validations is redundant on the page that
   * holds them.
   */
  hideDescription?: boolean;
  /**
   * Chip every criterion with what is ABOUT to happen to it, for a consumer showing
   * the oracle while a validation attempt is in flight: "Pending" for the ones an
   * agent will drive, "Manual" for the ones only a person can judge.
   *
   * Off by default, like its neighbours, and ignored for any criterion that
   * HAS a report — the Spec view's file preview shows the plain oracle with no run
   * attached to it, and chips there would name a run that does not exist.
   *
   * ANY attempt, not only a version's first: on a repeat attempt a criterion the
   * pinned report never covered is waiting on the run working right now. Safe,
   * because `report` outranks this — a covered row keeps the previous attempt's
   * verdict, and only uncovered rows read it.
   *
   * Named for the state rather than `pending`: a boolean prop by that name reads as
   * react-query's `isPending` — "still loading" — which is the opposite of what this
   * means. The criteria are loaded; the RESULTS are not.
   */
  awaitingReport?: boolean;

  /**
   * What the run is doing to each criterion right now — see LiveStatuses.
   *
   * Ranked ABOVE `report`, so a repeat attempt shows what it is re-working
   * instead of the last attempt's verdict. Supply it only while a cycle is
   * actually in flight: a stale map would keep overriding a settled report with
   * statuses nothing is still producing.
   */
  live?: LiveStatuses;
}

export function ValidationView({
  criteria,
  report,
  noPadding = false,
  fullWidth = false,
  hideDescription = false,
  awaitingReport = false,
  live,
}: ValidationViewProps) {
  const parsed = useMemo(() => parseValidationCriteria(criteria), [criteria]);
  // The report is optional and tolerant: a bad report never blocks the oracle —
  // it degrades to a non-blocking warning below and the criteria still render.
  const parsedReport = useMemo(
    () => (report ? parseValidationReport(report) : undefined),
    [report],
  );
  const reportError =
    parsedReport && "kind" in parsedReport ? parsedReport : undefined;
  const statuses =
    parsedReport && !("kind" in parsedReport) ? parsedReport : undefined;

  if ("kind" in parsed) {
    return (
      <Box sx={noPadding ? {} : { p: 3 }}>
        <Alert severity="error">
          Couldn't parse validation-criteria.json: {parsed.message}
        </Alert>
      </Box>
    );
  }
  return (
    <>
      {reportError && (
        <Box sx={noPadding ? {} : { px: 3, pt: 2 }}>
          <Alert severity="warning">
            Couldn't parse the validation report: {reportError.message}
          </Alert>
        </Box>
      )}
      <ValidationBody
        criteria={parsed}
        statuses={statuses}
        live={live}
        noPadding={noPadding}
        fullWidth={fullWidth}
        hideDescription={hideDescription}
        awaitingReport={awaitingReport}
      />
    </>
  );
}

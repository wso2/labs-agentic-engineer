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

import { forwardRef, useMemo, type ComponentPropsWithoutRef } from "react";
import { Alert, alpha, Box, Chip, Tooltip, Typography } from "@wso2/oxygen-ui";
import { Check } from "@wso2/oxygen-ui-icons-react";
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
import {
  CRITERION_STATE_LABEL,
  METHOD_COLOR,
  METHOD_FALLBACK_COLOR,
  METHOD_LABEL,
  tallyCriterionMethods,
} from "./counts.js";

// The method colours are solid behind a badge here. Text color is computed for
// contrast (getContrastText), so labels stay readable in both themes — the same
// approach as the DesignView type badges and the OpenAPI viewer's method badges.
// One line saying who does the checking, shown on hover. Only the two methods a
// design turn can author carry one; anything else gets a bare badge rather than
// an invented explanation.
const METHOD_TOOLTIP: Record<string, string> = {
  e2e: "Validated automatically by the agent.",
  manual: "Requires manual validation.",
};
// Requirement ids are structural, so a muted slate keeps them from competing
// with the colored method badges.
const REQ_COLOR = "#546e7a";

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

// The MUI/Oxygen Chip color union — kept local so the state map stays typed.
type ChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "error"
  | "info"
  | "success"
  | "warning";

// report.json status → the chip colour shown on a criterion when a run report is
// joined in. The LABEL comes from CRITERION_STATE_LABEL (counts.ts), which the
// consumer's tally line reads too — so a criterion's chip and the summary above
// it can never call the same status by two different names. Unknown statuses fall
// through to a neutral chip labelled verbatim.
const STATE_COLOR: Record<string, ChipColor> = {
  pass: "success",
  fail: "error",
  not_run: "default",
  not_validated: "warning",
  manual: "default",
};

type SolidBadgeProps = { label: string; color: string } & ComponentPropsWithoutRef<"span">;

// Forwards its ref and any remaining props onto the Box, because Tooltip hands
// its child both a ref and the hover/focus handlers that open it. The console
// wraps un-forwarding components in an inline-flex Box instead (see the kind chip
// in SkillsSection), which works here too — but that comment calls itself out as
// standing in for a ref the child could not hold, and this badge is local enough
// to just hold one.
const SolidBadge = forwardRef<HTMLSpanElement, SolidBadgeProps>(function SolidBadge(
  { label, color, ...rest },
  ref,
) {
  return (
    <Box
      component="span"
      ref={ref}
      {...rest}
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        px: 1,
        py: 0.5,
        borderRadius: 1,
        flexShrink: 0,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        bgcolor: color,
        color: theme.palette.getContrastText(color),
      })}
    >
      {label}
    </Box>
  );
});

// Says who checks a criterion, with a tooltip carrying what the word means —
// the badge alone cannot, and it is the reader's first encounter with the
// distinction. `count` is passed only by the summary tally, which shows the same
// badge with its total appended, so both surfaces stay in step by construction.
function MethodBadge({ method, count }: { method: string; count?: number }) {
  const label = METHOD_LABEL[method] ?? method;
  const badge = (
    <SolidBadge
      label={count === undefined ? label : `${label} ${count}`}
      color={METHOD_COLOR[method] ?? METHOD_FALLBACK_COLOR}
    />
  );
  const tooltip = METHOD_TOOLTIP[method];
  return tooltip ? <Tooltip title={tooltip}>{badge}</Tooltip> : badge;
}

// The per-criterion run-state chip (only rendered when a report is joined in).
function StateChip({ status }: { status: string }) {
  return (
    <Chip
      size="small"
      variant="outlined"
      color={STATE_COLOR[status] ?? "default"}
      {...(status === "pass" ? { icon: <Check size={14} /> } : {})}
      label={CRITERION_STATE_LABEL[status] ?? status}
      sx={{ flexShrink: 0 }}
    />
  );
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

// The in-flight vocabulary, LOCAL for the same reason "Pending" below is: these
// words describe work happening, and report.json can only describe work in the
// past tense, so none of them belongs in CRITERION_STATE_LABEL. Its two terminal
// words (`pass`/`fail`) DO arrive on the live feed, and deliberately fall through
// to StateChip — a criterion that has passed reads the same whether the news came
// from the feed or from the report, because it is the same fact.
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

// The per-criterion chip while the run is still working on it.
function LiveChip({ status }: { status: string }) {
  return (
    <Chip
      size="small"
      variant="outlined"
      color={LIVE_COLOR[status] ?? "info"}
      label={LIVE_LABEL[status] ?? status}
      sx={{ flexShrink: 0 }}
    />
  );
}

/**
 * The one chip a criterion's row carries, in precedence order.
 *
 * `manual` wins over everything. Such a criterion is answered by a person, so
 * the run will never answer it — that is what the method means — and any chip
 * promising a result is a claim the eventual report contradicts. It outranks a
 * live status as well as a report: a run legitimately reports progress for a
 * manual criterion (its test plan names every criterion, not only the ones an
 * agent will work), and rendering that as a run state left the row promising a
 * result beside a badge saying nobody would produce one.
 *
 * Then live over report, and the ordering there is the whole point: a repeat
 * attempt carries the PREVIOUS attempt's report, so ranking the report higher
 * would freeze a criterion on the last run's verdict for the entire time the
 * current run spends re-working it. The report wins again the moment the cycle
 * settles, because the consumer stops supplying live statuses then.
 *
 * `awaiting` last, and only it can yield nothing: the Spec view renders this
 * same pane with no run attached, where a chip would name a run that does not
 * exist.
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
  if (criterion.method === "manual") return <StateChip status="manual" />;
  // pass/fail arrive on the live feed too — report.json's own words, so its chip.
  if (live) return LIVE_LABEL[live] ? <LiveChip status={live} /> : <StateChip status={live} />;
  if (report) return <StateChip status={report.status} />;
  // "Pending" is local rather than a sixth CRITERION_STATE_LABEL entry: that map
  // is report.json's vocabulary, and a criterion with no report has no status to
  // name.
  if (awaiting) return <Chip size="small" variant="outlined" label="Pending" sx={{ flexShrink: 0 }} />;
  return null;
}

// One acceptance criterion: method badge, its id, the atomic assertion, its
// status chip (CriterionChip decides which one wins), healed/flaky markers, and
// — for a failure — the spec path and message beneath.
function CriterionRow({
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
  const failed = report?.status === "fail";
  return (
    // RequirementCard draws the rule that separates rows; it lands on THIS box, so
    // a failure block stays inside the criterion it belongs to instead of being cut
    // off from its own assertion.
    <Box sx={{ py: 1 }}>
      <Box sx={{ display: "flex", gap: 1.5, alignItems: "flex-start" }}>
        <Box sx={{ minWidth: 92, flexShrink: 0, pt: "1px" }}>
          <MethodBadge method={criterion.method} />
        </Box>
        <Typography component="span" sx={{ ...mono, flexShrink: 0 }}>
          {criterion.id}
        </Typography>
        <Typography variant="body2" sx={{ flexGrow: 1 }}>
          {criterion.must}
        </Typography>
        {report?.flaky && (
          <Chip size="small" variant="outlined" color="warning" label="flaky" sx={{ flexShrink: 0 }} />
        )}
        {report?.healed && (
          <Chip size="small" variant="outlined" label="healed" sx={{ flexShrink: 0 }} />
        )}
        <CriterionChip
          criterion={criterion}
          report={report}
          live={live}
          awaiting={awaiting}
        />
      </Box>
      {/* Failure detail sits full-width beneath the row (indented past the
          method badge) so a long trace never crowds the assertion. */}
      {failed && (report?.failureLocation || report?.spec || report?.failure) && (
        <Box sx={{ mt: 0.75, ml: "108px" }}>
          {/* Prefer the reporter's `<file>:<line>`, which points at the failing
              assertion rather than merely the spec that contains it. The gate
              above admits it on its own: a reporter can hand back a location with
              an empty message, and dropping the block then would throw away the
              only pointer to the failing assertion the run produced. */}
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
                // A wash, not a saturated fill. The state chip on the row above
                // already says "failed", so the surface's job is to be READABLE —
                // a stack trace is the longest text on the page and it was set in
                // monospace on solid error.main. The tint composites over
                // whichever surface is beneath it, so it holds in both themes;
                // same idiom as StatusChip's soft tones.
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
}: {
  requirement: Requirement;
  statuses: ValidationReport | undefined;
  live: LiveStatuses | undefined;
  awaiting: boolean;
}) {
  const count = requirement.criteria.length;
  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        p: 2,
        // Twice the gap between two criteria (16px). These were both 12px, so a
        // requirement boundary carried the same weight as a row boundary and the
        // nesting was invisible in the rhythm.
        mb: 3,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 0.75 }}>
        <SolidBadge label={requirement.id} color={REQ_COLOR} />
        <Typography variant="caption" color="text.secondary">
          {count} {count === 1 ? "criterion" : "criteria"}
        </Typography>
      </Box>
      <Typography
        variant="body1"
        sx={{ fontWeight: 500, mb: count > 0 ? 1.5 : 0 }}
      >
        {requirement.statement}
      </Typography>
      {count === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No criteria.
        </Typography>
      ) : (
        // A rule on the TOP of every row, not between rows. Bottom-of-all-but-last
        // left the first criterion as the only one with no boundary above it, so it
        // read as belonging to the statement in a way its siblings did not — and it
        // made a one-criterion requirement render with no rule at all. This way the
        // statement is the card's header, every criterion is bounded the same, and
        // the card's own border closes the list at the bottom.
        //
        // Owned here rather than by CriterionRow because it is a property of the
        // LIST; the rows get their own box because the badge row and the statement
        // above are their siblings.
        <Box sx={{ "& > *": { borderTop: 1, borderColor: "divider" } }}>
          {requirement.criteria.map((c) => (
            <CriterionRow
              key={c.id}
              criterion={c}
              report={statuses?.get(c.id)}
              live={live?.[c.id]}
              awaiting={awaiting}
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
  // Per-method tally for the summary header, from counts.ts so the consumer's own
  // method line (the console's tile, while an attempt is still running) counts the
  // same criteria in the same order. The per-run-state tally is deliberately NOT
  // here: it belongs with the verdict it explains, which the consumer renders above
  // this view (tallyCriterionStates), and duplicating it here would put the same
  // numbers on the page twice.
  const methods = useMemo(() => tallyCriterionMethods(criteria), [criteria]);
  // Every criterion has exactly one method, so the tally is a partition of them.
  const total = methods.reduce((n, m) => n + m.count, 0);

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
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1, maxWidth: "72ch" }}>
            Each criterion represents one thing your software must do, based on your
            requirements. After every deployment they are checked against the running
            software, and the results appear under Validations. To change one, ask the
            agent.
          </Typography>
        )}

        {/* Summary — totals plus a colored tally per verification method */}
        <Box
          sx={{
            mt: 1,
            mb: 3,
            display: "flex",
            alignItems: "center",
            gap: 1.5,
            flexWrap: "wrap",
          }}
        >
          <Typography variant="body2" color="text.secondary">
            {reqCount} {reqCount === 1 ? "requirement" : "requirements"} ·{" "}
            {total} {total === 1 ? "criterion" : "criteria"}
          </Typography>
          {methods.map(({ method, count }) => (
            <MethodBadge key={method} method={method} count={count} />
          ))}
        </Box>

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
            />
          ))
        )}
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

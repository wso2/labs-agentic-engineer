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
  ButtonBase,
  Chip,
  Collapse,
  InputAdornment,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  ClipboardCheck,
  FileText,
  ListFilter,
  Search,
  X,
} from "@wso2/oxygen-ui-icons-react";
import { useMemo, useState, type ReactNode } from "react";
import {
  featureScenarios,
  parseFeatureFile,
  type AcceptanceFeature,
  type AcceptanceRule,
  type AcceptanceScenario,
  type AcceptanceStep,
} from "./parseFeature.js";
import {
  ANY_STATUS,
  deriveStatuses,
  deriveTags,
  haystack,
  isFiltering,
  matchesFilter,
  NO_FILTER,
  NO_RESULT,
  type ScenarioFilter,
} from "./filter.js";
import {
  NO_RESULT_NOTE,
  OUTCOME_ICON,
  outcomeLabel,
  outcomeTone,
  type OutcomeTone,
} from "./outcomes.js";
import { tokenizeStep } from "./tokenize.js";
import {
  decidingStep,
  isReportParseError,
  parseAcceptanceReport,
  scenarioKey,
  type AcceptanceReport,
  type ReportScenario,
  type ReportStep,
} from "./report.js";

/**
 * The generic, not a webfont. Nothing in this repo loads one for code, and the
 * reference design's JetBrains Mono is deliberately not adopted.
 */
const MONO = "monospace";

/** Radii the theme's 12px base cannot express as a multiple. */
const R8 = "8px";
const PILL = "20px";

/** The refusal tag, as the authoring skill writes it. */
const NEGATIVE_TAG = "@negative";

/** The disclosure column. Every row has one, so it is never slack. */
const CHEVRON = 20;
const ROW_GAP = 1;
/** The shared band a row's first line occupies — the small Chip's own height. */
const ROW_LINE = 24;
/** Where a row's content starts, and so where everything under it lines up. */
const INDENT = `${CHEVRON + 8}px`;
/** The keyword column. Wide enough for `Given`, the longest of them. */
const KEYWORD = 52;
/** The reference design's scroll budget for the list. */
const BODY_MAX_HEIGHT = 640;

function Caret({ open, size = 16 }: { readonly open: boolean; readonly size?: number }) {
  return (
    <Box
      component="span"
      sx={{
        display: "inline-flex",
        flexShrink: 0,
        lineHeight: 0,
        color: "text.secondary",
        opacity: 0.55,
        transform: open ? "rotate(90deg)" : "rotate(0deg)",
        transition: "transform .15s linear",
      }}
    >
      <ChevronRight size={size} />
    </Box>
  );
}

/**
 * A tag.
 *
 * `emphasis` tells the two kinds apart, and it is a difference of FORM as much
 * as colour. `@negative` is a property a reviewer scans for — all-happy-path is
 * the commonest defect in a generated spec — so it is marked. `@story-N` is a
 * citation: it points at a requirement, and a pill would dress a reference up as
 * a discrete object you might click. It reads as what it is, quiet mono text.
 *
 * The marked one takes INFO, not the brand accent: orange says "the product's
 * own thing", which a refusal is not, where blue says "an informational
 * property", which it is. It shares `info.main` with the literals emphasised
 * inside a step, and that is fine — a mark after a sentence and mono text inside
 * an expanded box do not read as one signal. Amber is the hue it may never
 * take: amber means a person has to look, and a refusal scenario is ordinary
 * spec rather than something to act on.
 */
function TagPill({ tag, emphasis = false }: { readonly tag: string; readonly emphasis?: boolean }) {
  return (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        flexShrink: 0,
        fontFamily: MONO,
        fontSize: "0.6875rem",
        fontWeight: 500,
        lineHeight: 1.5,
        ...(emphasis
          ? {
              px: 1.25,
              py: 0.25,
              borderRadius: PILL,
              color: "info.dark",
              bgcolor: alpha(theme.palette.info.main, 0.12),
            }
          : { color: "text.secondary" }),
      })}
    >
      {tag}
    </Box>
  );
}

const TONE_PALETTE: Record<Exclude<OutcomeTone, "default">, "success" | "error" | "warning"> = {
  success: "success",
  error: "error",
  warning: "warning",
};

/**
 * The run's answer, as a soft pill.
 *
 * It keeps its glyph although the reference design's pill is text-only:
 * ADR-0016 requires a mark on an outcome precisely so the set can be told apart
 * at a glance rather than read one at a time, and four outcomes make that
 * bite harder than the two it was written for.
 */
function OutcomePill({ outcome }: { readonly outcome: string }) {
  const tone = outcomeTone(outcome);
  const Icon = OUTCOME_ICON[outcome];
  const label = outcomeLabel(outcome);
  const pill = (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        gap: 0.5,
        flexShrink: 0,
        px: 1.25,
        py: 0.25,
        borderRadius: PILL,
        fontSize: "0.6875rem",
        fontWeight: 500,
        lineHeight: 1.5,
        whiteSpace: "nowrap",
        ...(tone === "default"
          ? { color: "text.secondary", bgcolor: theme.palette.action.hover }
          : {
              color: theme.palette[TONE_PALETTE[tone]].main,
              bgcolor: alpha(theme.palette[TONE_PALETTE[tone]].main, 0.12),
            }),
      })}
    >
      {Icon ? <Icon size={12} /> : null}
      {label}
    </Box>
  );
  return outcome === NO_RESULT ? <Tooltip title={NO_RESULT_NOTE}>{pill}</Tooltip> : pill;
}

interface RenderedStep {
  readonly keyword: string;
  readonly text: string;
  readonly command?: string;
  readonly exit?: number;
  readonly observed?: string;
  /** The run stopped before this step — it is spec, not evidence. */
  readonly unreached: boolean;
}

/**
 * Pairs the specification's steps with what the run did.
 *
 * The report's steps win where it has them: they carry the evidence and their
 * own `text`. Anything the specification has beyond them was never reached,
 * which is how a blocked scenario visibly STOPS partway — the thing the raw
 * JSON hides.
 */
function renderedSteps(
  spec: readonly AcceptanceStep[],
  reported: ReportScenario | undefined,
): readonly RenderedStep[] {
  if (reported === undefined) {
    return spec.map((s) => ({ keyword: s.keyword, text: s.text, unreached: false }));
  }
  const ran: RenderedStep[] = reported.steps.map((s) => ({ ...s, unreached: false }));
  const rest = spec.slice(ran.length).map((s) => ({
    keyword: s.keyword,
    text: s.text,
    unreached: true,
  }));
  return [...ran, ...rest];
}

/**
 * One step, and whatever the run recorded against it.
 *
 * The box is mono because a step is a written artefact, and the literals inside
 * it are emphasised because they are what make it falsifiable. `observed` is
 * the exception and takes the body face: it is a sentence the agent wrote, not
 * something anyone typed, and on a blocked step it runs past four hundred
 * characters — which is unreadable set in a monospace.
 */
function StepLine({ step }: { readonly step: RenderedStep }) {
  return (
    <Box
      sx={{
        display: "flex",
        gap: ROW_GAP,
        alignItems: "flex-start",
        py: 0.25,
        opacity: step.unreached ? 0.45 : 1,
      }}
    >
      <Box
        component="span"
        sx={{
          width: KEYWORD,
          flexShrink: 0,
          fontFamily: MONO,
          fontSize: "0.8125rem",
          fontWeight: 600,
          lineHeight: 1.6,
          color: "text.primary",
        }}
      >
        {step.keyword}
      </Box>
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        <Box
          component="span"
          sx={{
            display: "block",
            fontFamily: MONO,
            fontSize: "0.8125rem",
            lineHeight: 1.6,
            wordBreak: "break-word",
          }}
        >
          {tokenizeStep(step.text).map((token, i) => (
            <Box
              key={i}
              component="span"
              sx={token.emphasis ? { color: "info.main" } : undefined}
            >
              {token.text}
            </Box>
          ))}
        </Box>
        {step.command !== undefined && (
          // Clamped, because a real one runs to 400 characters of `--fn`
          // predicate. It is provenance; `observed` below is the payload.
          <Tooltip title={step.command}>
            <Box
              component="span"
              sx={{
                display: "-webkit-box",
                WebkitLineClamp: 2,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
                mt: 0.25,
                fontFamily: MONO,
                fontSize: "0.6875rem",
                lineHeight: 1.6,
                color: "text.secondary",
                opacity: 0.75,
                wordBreak: "break-word",
              }}
            >
              {step.command}
            </Box>
          </Tooltip>
        )}
        {step.observed !== undefined && (
          <Typography
            variant="caption"
            sx={{ display: "block", mt: 0.25, fontSize: "0.75rem", color: "text.secondary" }}
          >
            {step.observed}
          </Typography>
        )}
      </Box>
      {step.unreached ? (
        <Box
          component="span"
          sx={{ flexShrink: 0, fontSize: "0.6875rem", lineHeight: 1.6, color: "text.secondary" }}
        >
          not reached
        </Box>
      ) : (
        // Only a nonzero exit is worth a mark. A green tick on every step would
        // put one on the very step that blocked a scenario, whose command
        // succeeded at proving a control was absent.
        step.exit !== undefined &&
        step.exit !== 0 && (
          <Box
            component="span"
            sx={{
              flexShrink: 0,
              fontFamily: MONO,
              fontSize: "0.6875rem",
              lineHeight: 1.6,
              color: "error.main",
            }}
          >
            {`exit ${step.exit}`}
          </Box>
        )
      )}
    </Box>
  );
}

function StepBox({ steps }: { readonly steps: readonly RenderedStep[] }) {
  return (
    <Box
      sx={{
        mt: 1,
        px: 1.5,
        py: 1.25,
        borderRadius: R8,
        border: 1,
        borderColor: "divider",
        bgcolor: "action.hover",
      }}
    >
      {steps.map((s, i) => (
        <StepLine key={`${s.keyword}-${i}`} step={s} />
      ))}
    </Box>
  );
}

interface ScenarioRowProps {
  readonly scenario: AcceptanceScenario;
  readonly reported: ReportScenario | undefined;
  readonly hasRun: boolean;
  readonly awaiting: boolean;
  readonly open: boolean;
  readonly onToggle: () => void;
}

function ScenarioRow({ scenario, reported, hasRun, awaiting, open, onToggle }: ScenarioRowProps) {
  const steps = renderedSteps(scenario.steps, reported);
  const why =
    reported !== undefined && reported.outcome !== "passed"
      ? decidingStep(reported)?.observed
      : undefined;
  const showPill = hasRun && !(reported === undefined && awaiting);

  return (
    <Box sx={{ py: 1 }}>
      <ButtonBase
        onClick={onToggle}
        aria-expanded={open}
        sx={{
          // `calc(100% + 16px)`, NOT `100%`. The row bleeds its hover surface
          // 8px past the text column on both sides (`mx: -1`) and pads back in
          // (`px: 1`), so the content lines up with the rule band above it. At
          // `width: 100%` the border box is already fixed, so the negative right
          // margin widens nothing — it bleeds left only — and the right padding
          // then eats 8px from a box sitting 8px short. That put this row's
          // outcome pill 16px inboard of the rule's @story-N and the feature's
          // count, which are the two things it has to line up with.
          width: "calc(100% + 16px)",
          display: "flex",
          gap: ROW_GAP,
          alignItems: "flex-start",
          textAlign: "left",
          px: 1,
          mx: -1,
          borderRadius: R8,
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", height: ROW_LINE, width: CHEVRON, flexShrink: 0 }}>
          <Caret open={open} size={14} />
        </Box>
        {/* The marks sit INSIDE the sentence, not in a column beside it: they
            qualify it, and flowing with the text means they follow the last
            word — onto line two when a long name wraps — so a name is never
            truncated to hold a column's width. */}
        <Typography
          variant="body2"
          sx={{ flexGrow: 1, minWidth: 0, lineHeight: `${ROW_LINE}px` }}
        >
          {scenario.name}
          {scenario.kind !== "Scenario" && (
            <Box
              component="span"
              sx={{
                ml: 1,
                fontFamily: MONO,
                fontSize: "0.6875rem",
                fontWeight: 500,
                color: "text.secondary",
              }}
            >
              {`${scenario.kind}:`}
            </Box>
          )}
          {/* `negative`, not the tags: @negative inherits Feature -> Rule ->
              Scenario, so a scenario under a prohibition rule carries the
              property without carrying the tag — 22 of this repo's 77 refusals
              are that shape, and reading the raw tags showed them nothing. */}
          {scenario.negative && (
            <Box component="span" sx={{ ml: 1 }}>
              <TagPill tag={NEGATIVE_TAG} emphasis />
            </Box>
          )}
          {scenario.tags
            .filter((t) => t !== NEGATIVE_TAG)
            .map((t) => (
              <Box key={t} component="span" sx={{ ml: 1 }}>
                <TagPill tag={t} />
              </Box>
            ))}
        </Typography>
        {showPill && (
          <Box sx={{ display: "flex", alignItems: "center", height: ROW_LINE, flexShrink: 0 }}>
            <OutcomePill outcome={reported?.outcome ?? NO_RESULT} />
          </Box>
        )}
      </ButtonBase>

      {/* With nothing open by default this is the only thing on the page saying
          WHY, so a reader can triage without opening anything. */}
      {!open && why !== undefined && (
        <Typography
          variant="body2"
          sx={{
            ml: INDENT,
            mt: 0.25,
            color: "text.secondary",
            display: "-webkit-box",
            WebkitLineClamp: 1,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
          }}
        >
          {why}
        </Typography>
      )}

      <Collapse in={open} unmountOnExit>
        <Box sx={{ ml: INDENT }}>
          {steps.length === 0 ? (
            <Typography variant="body2" sx={{ mt: 1, color: "text.secondary" }}>
              This scenario records no steps.
            </Typography>
          ) : (
            <StepBox steps={steps} />
          )}
        </Box>
      </Collapse>
    </Box>
  );
}

/** A rule, and the scenarios that illustrate it. */
function RuleBand({
  feature,
  rule,
  scenarios,
  report,
  awaiting,
  openKeys,
  onToggle,
}: {
  readonly feature: AcceptanceFeature;
  readonly rule: AcceptanceRule;
  readonly scenarios: readonly AcceptanceScenario[];
  readonly report: AcceptanceReport | undefined;
  readonly awaiting: boolean;
  readonly openKeys: ReadonlySet<string>;
  readonly onToggle: (key: string) => void;
}) {
  return (
    <Box sx={{ mb: 2, "&:last-of-type": { mb: 0 } }}>
      {(rule.text !== "" || rule.tags.length > 0) && (
        <Box sx={{ display: "flex", alignItems: "flex-start", gap: ROW_GAP, mb: 0.5 }}>
          <Typography
            variant="body2"
            sx={{ fontWeight: 500, flexGrow: 1, minWidth: 0, lineHeight: `${ROW_LINE}px` }}
          >
            {rule.text}
          </Typography>
          {/* References only. A rule that is ITSELF a prohibition carries
              @negative, and every scenario under it inherits — so each row shows
              the mark for itself and repeating it here says the same thing one
              level up. */}
          <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, flexShrink: 0, height: ROW_LINE }}>
            {rule.tags
              .filter((t) => t !== NEGATIVE_TAG)
              .map((t) => (
                <TagPill key={t} tag={t} />
              ))}
          </Box>
        </Box>
      )}
      <Box sx={{ ml: 1.5, "& > * + *": { borderTop: 1, borderColor: "divider" } }}>
        {scenarios.map((scenario) => {
          const key = scenarioKey(feature.name, rule.text, scenario.name);
          return (
            <ScenarioRow
              key={key}
              scenario={scenario}
              reported={report?.byKey.get(key)}
              hasRun={report !== undefined}
              awaiting={awaiting}
              open={openKeys.has(key)}
              onToggle={() => onToggle(key)}
            />
          );
        })}
      </Box>
    </Box>
  );
}

/** One capability: a collapsible group, headed by its own band. */
function FeatureGroup({
  feature,
  visible,
  report,
  awaiting,
  open,
  onToggleGroup,
  openKeys,
  onToggle,
}: {
  readonly feature: AcceptanceFeature;
  readonly visible: ReadonlySet<string>;
  readonly report: AcceptanceReport | undefined;
  readonly awaiting: boolean;
  readonly open: boolean;
  readonly onToggleGroup: () => void;
  readonly openKeys: ReadonlySet<string>;
  readonly onToggle: (key: string) => void;
}) {
  const all = featureScenarios(feature);
  const shown = all.filter(({ scenario }) => visible.has(scenario.name));
  const refusals = all.filter(({ scenario }) => scenario.negative).length;

  // NOT the file path, though the reference design prints one: the lexicon
  // forbids quoting a repo path to the user, and a filename is never a label.
  // The counts are what a reader can actually use.
  const counts = [
    `${feature.rules.length} ${feature.rules.length === 1 ? "rule" : "rules"}`,
    `${all.length} ${all.length === 1 ? "scenario" : "scenarios"}`,
    ...(refusals > 0 ? [`${refusals} ${refusals === 1 ? "refusal" : "refusals"}`] : []),
  ].join(" · ");

  const rules = feature.rules
    .map((rule) => ({
      rule,
      scenarios: rule.scenarios.filter((s) => visible.has(s.name)),
    }))
    .filter((r) => r.scenarios.length > 0);

  return (
    <Box sx={{ "& + &": { borderTop: 1, borderColor: "divider" } }}>
      <ButtonBase
        onClick={onToggleGroup}
        aria-expanded={open}
        sx={{
          width: "100%",
          display: "flex",
          gap: 1.25,
          alignItems: "flex-start",
          textAlign: "left",
          px: 2,
          py: 1.5,
          bgcolor: "action.hover",
          "&:hover": { bgcolor: "action.selected" },
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", height: ROW_LINE, flexShrink: 0 }}>
          <Caret open={open} />
        </Box>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            height: ROW_LINE,
            flexShrink: 0,
            color: "text.secondary",
          }}
        >
          <FileText size={16} />
        </Box>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="body1" sx={{ fontWeight: 500, lineHeight: `${ROW_LINE}px` }}>
            {feature.name}
          </Typography>
          <Typography variant="caption" sx={{ color: "text.secondary", opacity: 0.8 }}>
            {counts}
          </Typography>
        </Box>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, flexShrink: 0, height: ROW_LINE }}>
          {feature.tags
            .filter((t) => t !== NEGATIVE_TAG)
            .map((t) => (
              <TagPill key={t} tag={t} />
            ))}
        </Box>
        <Box
          component="span"
          sx={{
            flexShrink: 0,
            minWidth: 74,
            textAlign: "right",
            lineHeight: `${ROW_LINE}px`,
            fontSize: "0.6875rem",
            color: "text.secondary",
          }}
        >
          {`${shown.length} of ${all.length}`}
        </Box>
      </ButtonBase>

      <Collapse in={open} unmountOnExit>
        <Box sx={{ px: 2, pt: 1.5, pb: 2 }}>
          {feature.background.length > 0 && (
            <Box sx={{ mb: 2 }}>
              <Typography
                variant="caption"
                sx={{ fontFamily: MONO, fontWeight: 600, color: "text.secondary" }}
              >
                Background
              </Typography>
              <StepBox
                steps={feature.background.map((s) => ({
                  keyword: s.keyword,
                  text: s.text,
                  unreached: false,
                }))}
              />
            </Box>
          )}
          {rules.map((r, i) => (
            <RuleBand
              key={`${r.rule.text}-${i}`}
              feature={feature}
              rule={r.rule}
              scenarios={r.scenarios}
              report={report}
              awaiting={awaiting}
              openKeys={openKeys}
              onToggle={onToggle}
            />
          ))}
        </Box>
      </Collapse>
    </Box>
  );
}

export interface AcceptanceFeatureSource {
  /** Repo-relative, e.g. `specs/acceptance/bought-items.feature`. */
  readonly path: string;
  readonly content: string;
}

export interface AcceptanceViewProps {
  readonly features: readonly AcceptanceFeatureSource[];
  /** Raw `tests/acceptance/report.json`. Omit for the specification alone. */
  readonly report?: string;
  readonly noPadding?: boolean;
  readonly fullWidth?: boolean;
  readonly hideDescription?: boolean;
  /**
   * An attempt is in flight, so a scenario the report does not cover is not yet
   * a scenario the run declined to cover — it gets no pill rather than
   * `No result`.
   */
  readonly awaitingReport?: boolean;
}

export function AcceptanceView({
  features,
  report: rawReport,
  noPadding = false,
  fullWidth = false,
  hideDescription = false,
  awaitingReport = false,
}: AcceptanceViewProps) {
  const [openKeys, setOpenKeys] = useState<ReadonlySet<string>>(() => new Set());
  // Groups are open until closed: with a handful of capabilities, opening them
  // by hand before anything can be read is friction with no payoff.
  const [closedGroups, setClosedGroups] = useState<ReadonlySet<string>>(() => new Set());
  const [filter, setFilter] = useState<ScenarioFilter>(NO_FILTER);

  const toggleIn = (
    set: (fn: (prev: ReadonlySet<string>) => ReadonlySet<string>) => void,
    key: string,
  ) =>
    set((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  const parsed = useMemo(
    () =>
      features
        .map((f) => parseFeatureFile(f.path, f.content))
        .filter((f): f is AcceptanceFeature => f !== null),
    [features],
  );

  const reportResult = useMemo(
    () => (rawReport === undefined ? undefined : parseAcceptanceReport(rawReport)),
    [rawReport],
  );
  const reportError =
    reportResult !== undefined && isReportParseError(reportResult) ? reportResult.error : undefined;
  const report =
    reportResult !== undefined && !isReportParseError(reportResult) ? reportResult : undefined;

  const tags = useMemo(() => deriveTags(parsed), [parsed]);
  const statuses = useMemo(
    () => deriveStatuses(parsed, report, awaitingReport),
    [parsed, report, awaitingReport],
  );

  /** Every scenario, with what it takes to filter it and the key it opens by. */
  const indexed = useMemo(
    () =>
      parsed.flatMap((feature) =>
        featureScenarios(feature).map(({ rule, scenario }) => {
          const key = scenarioKey(feature.name, rule.text, scenario.name);
          const reported = report?.byKey.get(key);
          return {
            key,
            name: scenario.name,
            hay: haystack({
              feature: feature.name,
              rule: rule.text,
              scenario: scenario.name,
              tags: scenario.tags,
              steps: reported?.steps ?? scenario.steps,
            }),
            tags: [...feature.tags, ...rule.tags, ...scenario.tags],
            outcome: reported?.outcome ?? NO_RESULT,
          };
        }),
      ),
    [parsed, report],
  );

  const visible = useMemo(
    () => new Set(indexed.filter((s) => matchesFilter(s, filter)).map((s) => s.name)),
    [indexed, filter],
  );

  const orphans = useMemo(() => {
    if (report === undefined) return [];
    const known = new Set(indexed.map((s) => s.key));
    return report.scenarios.filter((s) => !known.has(scenarioKey(s.feature, s.rule, s.scenario)));
  }, [indexed, report]);

  const groups = parsed.filter(
    (f) => featureScenarios(f).some(({ scenario }) => visible.has(scenario.name)) || !isFiltering(filter),
  );

  const allKeys = indexed.map((s) => s.key);
  const everythingOpen = allKeys.length > 0 && allKeys.every((k) => openKeys.has(k));

  const matchLabel = isFiltering(filter)
    ? `${visible.size} of ${indexed.length} scenarios match`
    : `${indexed.length} ${indexed.length === 1 ? "scenario" : "scenarios"}`;

  const totals = useMemo(() => {
    const refusals = indexed.length === 0 ? 0 : parsed.flatMap(featureScenarios).filter(({ scenario }) => scenario.negative).length;
    const rules = parsed.reduce((n, f) => n + f.rules.length, 0);
    return [
      `${parsed.length} ${parsed.length === 1 ? "capability" : "capabilities"}`,
      `${rules} ${rules === 1 ? "rule" : "rules"}`,
      `${indexed.length} ${indexed.length === 1 ? "scenario" : "scenarios"}`,
      ...(refusals > 0 ? [`${refusals} ${refusals === 1 ? "refusal" : "refusals"}`] : []),
    ].join(" · ");
  }, [parsed, indexed]);

  if (parsed.length === 0 && orphans.length === 0) {
    return (
      <Shell noPadding={noPadding} fullWidth={fullWidth}>
        <Box
          sx={{
            textAlign: "center",
            py: 8,
            px: 2,
            border: 1,
            borderStyle: "dashed",
            borderColor: "divider",
            borderRadius: 1,
          }}
        >
          <Box sx={{ display: "flex", justifyContent: "center", opacity: 0.3, mb: 2 }}>
            <ClipboardCheck size={48} />
          </Box>
          <Typography variant="h6" gutterBottom>
            No acceptance criteria yet
          </Typography>
          <Typography variant="body2" sx={{ color: "text.secondary", maxWidth: 480, mx: "auto" }}>
            They are written from your requirements when the design is generated.
          </Typography>
        </Box>
      </Shell>
    );
  }

  return (
    <Shell noPadding={noPadding} fullWidth={fullWidth}>
      {/* The heading belongs to the specification view. On the Validations page
          the page title already says Validations, and a second heading under the
          verdict would repeat it — which is naming rule 1. */}
      {report === undefined && reportError === undefined && (
        <>
          <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
            Acceptance criteria
          </Typography>
          {!hideDescription && (
            <Typography variant="body2" sx={{ mt: 1, maxWidth: 720, color: "text.secondary" }}>
              Each scenario is one concrete example of a rule your product must follow, taken from
              your requirements alone. After every deployment they are driven against the deployed
              system and the results appear under Validations. To change one, ask the agent.
            </Typography>
          )}
          <Typography
            variant="caption"
            sx={{ display: "block", mt: 0.5, mb: 3, color: "text.secondary", opacity: 0.8 }}
          >
            {totals}
          </Typography>
        </>
      )}

      {reportError !== undefined && (
        <Alert severity="warning" sx={{ mb: 3 }}>
          {`The last run's report could not be read (${reportError}), so this is the specification alone.`}
        </Alert>
      )}

      <Box
        sx={{
          border: 1,
          borderColor: "divider",
          borderRadius: 1,
          overflow: "hidden",
          bgcolor: "background.acrylic",
          backdropFilter: "blur(10px)",
        }}
      >
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            gap: 1.25,
            px: 2,
            py: 1.75,
            borderBottom: 1,
            borderColor: "divider",
          }}
        >
          <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
            <TextField
              size="small"
              value={filter.query}
              onChange={(e) => setFilter((f) => ({ ...f, query: e.target.value }))}
              placeholder="Filter by scenario, step or capability"
              inputProps={{ "aria-label": "Filter scenarios" }}
              sx={{ flex: 1, minWidth: 220, "& .MuiOutlinedInput-root": { height: 40, borderRadius: R8 } }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <Box sx={{ display: "flex", color: "text.secondary", opacity: 0.7 }}>
                      <Search size={16} />
                    </Box>
                  </InputAdornment>
                ),
                // Oxygen's own SearchBar cannot carry this: it hardcodes
                // `endAdornment` after spreading slotProps.input, so anything
                // passed in is overwritten, and the base that accepts one is not
                // re-exported from the package root.
                ...(filter.query !== ""
                  ? {
                      endAdornment: (
                        <InputAdornment position="end">
                          <ButtonBase
                            onClick={() => setFilter((f) => ({ ...f, query: "" }))}
                            aria-label="Clear the filter"
                            sx={{ borderRadius: "50%", p: 0.25, color: "text.secondary" }}
                          >
                            <X size={15} />
                          </ButtonBase>
                        </InputAdornment>
                      ),
                    }
                  : {}),
              }}
            />
            <Button
              variant="outlined"
              onClick={() => setOpenKeys(everythingOpen ? new Set() : new Set(allKeys))}
              startIcon={everythingOpen ? <ChevronsDownUp size={16} /> : <ChevronsUpDown size={16} />}
              sx={{ height: 40, flexShrink: 0 }}
            >
              {everythingOpen ? "Collapse all" : "Expand all"}
            </Button>
          </Box>

          {(statuses.length > 0 || tags.length > 0) && (
            <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
              <Box
                component="span"
                sx={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 0.75,
                  fontSize: "0.75rem",
                  color: "text.secondary",
                }}
              >
                <ListFilter size={14} />
                Filters
              </Box>
              {statuses.length > 0 && (
                <ToggleButtonGroup
                  size="small"
                  exclusive
                  value={filter.status}
                  onChange={(_, next: string | null) =>
                    setFilter((f) => ({ ...f, status: next ?? ANY_STATUS }))
                  }
                  aria-label="Filter by outcome"
                  sx={{
                    bgcolor: "action.hover",
                    borderRadius: R8,
                    p: 0.25,
                    "& .MuiToggleButtonGroup-grouped": {
                      border: 0,
                      borderRadius: "6px",
                      px: 1.5,
                      py: 0,
                      // Exact, not a floor: the natural height jumps the row.
                      height: 28,
                      textTransform: "none",
                      fontSize: "0.75rem",
                      fontWeight: 500,
                      color: "text.secondary",
                      "&.Mui-selected": {
                        bgcolor: "background.paper",
                        color: "text.primary",
                        boxShadow: 1,
                        "&:hover": { bgcolor: "background.paper" },
                      },
                    },
                  }}
                >
                  {statuses.map((s) => (
                    <ToggleButton key={s || "none"} value={s}>
                      {s === ANY_STATUS ? "All" : outcomeLabel(s)}
                    </ToggleButton>
                  ))}
                </ToggleButtonGroup>
              )}
              {tags.map((t) => {
                const active = filter.tags.includes(t);
                return (
                  <Chip
                    key={t}
                    size="small"
                    clickable
                    label={t}
                    aria-pressed={active}
                    onClick={() =>
                      setFilter((f) => ({
                        ...f,
                        tags: active ? f.tags.filter((x) => x !== t) : [...f.tags, t],
                      }))
                    }
                    sx={(theme) => ({
                      borderRadius: PILL,
                      fontFamily: MONO,
                      fontSize: "0.6875rem",
                      fontWeight: 500,
                      ...(active
                        ? {
                            bgcolor: "primary.main",
                            color: "primary.contrastText",
                            "&:hover": { bgcolor: "primary.dark" },
                          }
                        : {
                            bgcolor: "transparent",
                            color: "text.secondary",
                            border: 1,
                            borderColor: "divider",
                            "&:hover": { bgcolor: theme.palette.action.hover },
                          }),
                    })}
                  />
                );
              })}
              {isFiltering(filter) && (
                <Button size="small" onClick={() => setFilter(NO_FILTER)} sx={{ minWidth: 0 }}>
                  Reset
                </Button>
              )}
            </Box>
          )}

          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            {matchLabel}
          </Typography>
        </Box>

        {/* The card owns the scroll, which is also what keeps the toolbar above
            it on screen — there is no sticky positioning anywhere in this
            console and none is needed. */}
        <Box
          role="region"
          aria-label="Acceptance scenarios"
          sx={{ maxHeight: BODY_MAX_HEIGHT, overflowY: "auto" }}
        >
          {visible.size === 0 && orphans.length === 0 ? (
            <Box sx={{ textAlign: "center", px: 2, py: 6 }}>
              <Typography variant="body1" sx={{ fontWeight: 500 }}>
                No matching scenarios
              </Typography>
              <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
                Adjust the search term, the outcome or the tags.
              </Typography>
              <Button onClick={() => setFilter(NO_FILTER)} sx={{ mt: 1.5 }}>
                Clear filters
              </Button>
            </Box>
          ) : (
            <>
              {groups.map((feature) => (
                <FeatureGroup
                  key={feature.file}
                  feature={feature}
                  visible={visible}
                  report={report}
                  awaiting={awaitingReport}
                  open={!closedGroups.has(feature.file)}
                  onToggleGroup={() => toggleIn(setClosedGroups, feature.file)}
                  openKeys={openKeys}
                  onToggle={(key) => toggleIn(setOpenKeys, key)}
                />
              ))}
              {orphans.length > 0 && (
                <OrphanGroup
                  orphans={orphans}
                  openKeys={openKeys}
                  onToggle={(key) => toggleIn(setOpenKeys, key)}
                />
              )}
            </>
          )}
        </Box>
      </Box>
    </Shell>
  );
}

function Shell({
  noPadding,
  fullWidth,
  children,
}: {
  readonly noPadding: boolean;
  readonly fullWidth: boolean;
  readonly children: ReactNode;
}) {
  return (
    <Box sx={{ height: "100%", overflow: "auto", ...(noPadding ? {} : { p: 3 }) }}>
      {/* `fullWidth` means NO CAP — the page owns its width, and no page in this
          console caps its body: PageContent already supplies the outer cap and
          the centring. Capping here anyway left the report card short of the
          verdict tile above it, which is an Alert and uncapped. The 1080 is the
          reference design's CARD width and belongs to the pane case only. */}
      <Box sx={fullWidth ? undefined : { maxWidth: 1080, mx: "auto" }}>{children}</Box>
    </Box>
  );
}

/** Scenarios the run answered that the specification no longer declares. */
function OrphanGroup({
  orphans,
  openKeys,
  onToggle,
}: {
  readonly orphans: readonly ReportScenario[];
  readonly openKeys: ReadonlySet<string>;
  readonly onToggle: (key: string) => void;
}) {
  return (
    <Box sx={{ borderTop: 1, borderColor: "divider", opacity: 0.8 }}>
      <Box sx={{ px: 2, py: 1.5, bgcolor: "action.hover" }}>
        <Typography
          variant="caption"
          sx={{ display: "block", fontWeight: 700, letterSpacing: "0.08em", color: "text.secondary" }}
        >
          NOT IN THE CURRENT SPECIFICATION
        </Typography>
        <Typography variant="caption" sx={{ color: "text.secondary", opacity: 0.8 }}>
          {orphans.length === 1
            ? "This run answered one scenario that has since been rewritten or removed."
            : `This run answered ${orphans.length} scenarios that have since been rewritten or removed.`}
        </Typography>
      </Box>
      <Box sx={{ px: 2, py: 1 }}>
        {orphans.map((s) => {
          const key = scenarioKey(s.feature, s.rule, s.scenario);
          return (
            <ScenarioRow
              key={key}
              scenario={{ name: s.scenario, kind: "Scenario", line: s.line ?? 0, tags: [], negative: false, steps: [] }}
              reported={s}
              hasRun
              awaiting={false}
              open={openKeys.has(key)}
              onToggle={() => onToggle(key)}
            />
          );
        })}
      </Box>
    </Box>
  );
}

export type { ReportStep };

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

import { useMemo, useState, type ReactNode } from "react";
import { AcceptanceView } from "@aep/ui-acceptance-view";
import { Alert, Box, Button, ButtonBase, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import { stamp } from "../../../lib/stamp";
import { useBuilds } from "../../builds/api/builds";
import { useValidationSnapshot, useVersionLedger } from "../../builds/api/runs";
import { NextStepsBar } from "../../builds/components/NextStepsBar";
import { LogLines } from "../../builds/components/CardSection";
import { useNextInterview } from "../../builds/hooks/useNextInterview";
import { useRunProgress } from "../../builds/hooks/useRunProgress";
import { fixedBy, isBuilding, versionRows } from "../../builds/model/ledger";
import { nextSteps } from "../../builds/model/nextSteps";
import { agentLogLines } from "../../builds/model/run";
import { groupByFeature, validationOutcome } from "../../builds/model/validation";
import { CardOverlay } from "../../projects/components/CardOverlay";
import { PHONE } from "../../shell/layout";
import { useRevalidate, useValidation } from "../api/validations";
import { attemptResult, attemptsOf, noAttemptsReason, revalidateRefusal, type Attempt, type VerdictTone } from "../model/attempts";
import { ValidationByFeature } from "./ValidationByFeature";

// A version's Validation card, over the Validation ledger: every attempt down
// its side, newest first, and the one picked (the newest by default) by
// feature, each scenario passed or failed with what it expected and got. A
// failing scenario on the newest attempt offers Fix (a repair build, vX.1);
// the header revalidates. An attempt's full report and its log open beside it.
// Copied from the old console's validation page (ValidationMilestonePage,
// ReportCard).

const TONE_COLOUR: Record<NonNullable<VerdictTone>, string> = {
  primary: "primary.main",
  success: "success.main",
  warning: "warning.main",
  error: "error.main",
};

function toneColour(tone: VerdictTone): string {
  return tone ? TONE_COLOUR[tone] : "text.secondary";
}

function AttemptList({ attempts, chosen, onChoose }: { attempts: Attempt[]; chosen: number; onChoose: (n: number) => void }) {
  return (
    <Box
      component="nav"
      aria-label="Attempts"
      sx={{
        width: 220,
        flexShrink: 0,
        borderRight: 1,
        borderColor: "divider",
        overflowY: "auto",
        px: 1,
        pt: 1.75,
        pb: 4,
        display: "flex",
        flexDirection: "column",
        gap: 0.25,
        [PHONE]: { width: "auto", borderRight: 0, borderBottom: 1, flexDirection: "row", overflowX: "auto", pb: 1 },
      }}
    >
      <Typography
        component="h3"
        sx={{ px: 1.25, pt: 1, pb: 0.5, fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary", [PHONE]: { display: "none" } }}
      >
        Attempts
      </Typography>
      {attempts.map((a) => {
        const result = attemptResult(a.cycle);
        const selected = a.number === chosen;
        return (
          <ButtonBase
            key={a.cycle.id}
            onClick={() => onChoose(a.number)}
            aria-current={selected ? "true" : undefined}
            sx={{
              display: "flex",
              flexDirection: "column",
              alignItems: "flex-start",
              px: 1.25,
              py: 0.75,
              borderRadius: 1.5,
              flexShrink: 0,
              bgcolor: selected ? "action.selected" : "transparent",
              "&:hover": { bgcolor: selected ? "action.selected" : "action.hover" },
            }}
          >
            <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
              Attempt {a.number}
            </Typography>
            <Typography component="span" variant="caption" sx={{ color: toneColour(result.tone) }}>
              {result.label}
              <Box component="span" sx={{ color: "text.secondary" }}>
                {" · "}
                {stamp(a.cycle.endedAt ?? a.cycle.createdAt)}
              </Box>
            </Typography>
          </ButtonBase>
        );
      })}
    </Box>
  );
}

/** The attempt's validation cycle, from its run's progress stream, newest first; mounted only while open. */
function AttemptLog({ projectName, attempt }: { projectName: string; attempt: Attempt }) {
  const progress = useRunProgress(projectName, attempt.runId);
  const events = progress.cycles.find((c) => c.cycle.id === attempt.cycle.id)?.events ?? [];
  const empty =
    progress.phase === "ended"
      ? "No log kept for this attempt."
      : progress.phase === "reconnecting"
        ? "Connection lost, reconnecting…"
        : "Attaching to the attempt's log…";
  return <LogLines lines={agentLogLines(events)} empty={empty} maxHeight={360} />;
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title} sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
      <Typography component="h4" variant="body2" sx={{ fontWeight: 600, px: 1.75, py: 1, borderBottom: 1, borderColor: "divider" }}>
        {title}
      </Typography>
      {children}
    </Box>
  );
}

function AttemptView({ projectName, version, attempt, newest }: { projectName: string; version: string; attempt: Attempt; newest: boolean }) {
  const { cycle } = attempt;
  const settled = Boolean(cycle.endedAt);
  const landed = !settled || Boolean(cycle.mergeSha);
  const snapshot = useValidationSnapshot(projectName, version, cycle.id, landed, settled);
  const builds = useBuilds(projectName);
  const ledger = useVersionLedger(projectName);
  const nextInterview = useNextInterview(projectName);
  const [shown, setShown] = useState<"report" | "log" | null>(null);

  const data = snapshot.data;
  const groups = useMemo(() => (data ? groupByFeature(data) : null), [data]);
  const outcome = settled && groups && data?.report ? validationOutcome(groups) : null;
  const build = builds.data?.find((b) => b.version === version);
  const rows = ledger.data && builds.data ? versionRows(ledger.data, builds.data) : [];
  const fix = fixedBy(rows, version);
  // Fix repairs what the version's final validation failed, so only the newest attempt offers it.
  const next =
    newest && outcome && outcome.failing.length > 0
      ? nextSteps({
          version,
          outcome,
          fixedBy: fix ? { version: fix.version, building: isBuilding(fix.status) } : null,
          latest: rows[0]?.version ?? version,
          nextInterview,
        })
      : null;
  const result = attemptResult(cycle);
  const toggle = (which: "report" | "log") => setShown((s) => (s === which ? null : which));

  let body: ReactNode;
  if (!landed) {
    body = <Typography variant="body2" color="text.secondary">This attempt never landed, so it has no report.</Typography>;
  } else if (snapshot.isError) {
    body = <Alert severity="info">This attempt produced no report: it reached no usable results.</Alert>;
  } else if (!groups) {
    body = <Skeleton variant="rounded" height={140} aria-label="Loading the attempt's report" />;
  } else {
    body = (
      <ValidationByFeature
        projectName={projectName}
        groups={groups}
        version={version}
        builtHere={build && !build.fixes ? build.features.map((f) => f.id) : []}
        baseline={data?.baseline?.version ?? null}
        settled={Boolean(outcome)}
        failingActions={next && <NextStepsBar projectName={projectName} next={next} />}
      />
    );
  }

  return (
    <Box sx={{ maxWidth: 860, display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1.5, flexWrap: "wrap" }}>
        <Typography component="h2" sx={{ fontSize: "1.25rem", fontWeight: 600 }}>
          Attempt {attempt.number}
        </Typography>
        <Typography role="status" variant="body2" sx={{ fontWeight: 600, color: toneColour(result.tone) }}>
          {result.label}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {settled ? `ended ${stamp(cycle.endedAt)}` : `started ${stamp(cycle.createdAt)}`}
        </Typography>
        <Box sx={{ flex: 1 }} />
        <Button size="small" variant={shown === "report" ? "contained" : "outlined"} disabled={!data} aria-pressed={shown === "report"} onClick={() => toggle("report")}>
          Report
        </Button>
        <Button size="small" variant={shown === "log" ? "contained" : "outlined"} aria-pressed={shown === "log"} onClick={() => toggle("log")}>
          Log
        </Button>
        {cycle.prUrl && cycle.prNumber ? (
          <Button size="small" href={cycle.prUrl} target="_blank" rel="noreferrer">
            PR #{cycle.prNumber}
          </Button>
        ) : null}
      </Box>
      {shown === "report" && data && (
        <Panel title={`Attempt ${attempt.number}'s report`}>
          <AcceptanceView
            noPadding
            fullWidth
            hideDescription
            features={data.criteria}
            awaitingReport={!data.report}
            {...(data.report ? { report: data.report } : {})}
          />
        </Panel>
      )}
      {shown === "log" && (
        <Panel title={`Attempt ${attempt.number}'s log`}>
          <AttemptLog projectName={projectName} attempt={attempt} />
        </Panel>
      )}
      {body}
    </Box>
  );
}

function Revalidate({ detail, revalidate, hasVerdict }: {
  detail: { live: boolean; deployed: boolean } | undefined;
  revalidate: ReturnType<typeof useRevalidate>;
  hasVerdict: boolean;
}) {
  const refusal = detail ? revalidateRefusal(detail) : null;
  const button = (
    <Button
      size="small"
      variant="outlined"
      disabled={!detail || refusal !== null}
      loading={revalidate.isPending}
      onClick={() => revalidate.mutate()}
    >
      {hasVerdict ? "Revalidate" : "Validate"}
    </Button>
  );
  // A disabled button swallows the hover its tooltip needs; the span keeps the reason reachable.
  return refusal ? (
    <Tooltip title={refusal}>
      <span>{button}</span>
    </Tooltip>
  ) : (
    button
  );
}

export function ValidationCard({ projectName, version }: { projectName: string; version: string }) {
  const detail = useValidation(projectName, version);
  const revalidate = useRevalidate(projectName, version);
  const attempts = useMemo(() => attemptsOf(detail.data?.runs ?? []), [detail.data]);
  const [picked, setPicked] = useState<number | null>(null);
  const newest = attempts[0];
  const attempt = attempts.find((a) => a.number === picked) ?? newest;
  const hasVerdict = attempts.some((a) => Boolean(a.cycle.validationVerdict));

  let body: ReactNode;
  if (detail.isError) {
    body = (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" action={<Button onClick={() => void detail.refetch()}>Retry</Button>}>
          {detail.error.message}
        </Alert>
      </Box>
    );
  } else if (!detail.data) {
    body = (
      <Box sx={{ p: 3 }}>
        <Skeleton variant="rounded" height={160} sx={{ maxWidth: 860 }} />
      </Box>
    );
  } else if (!attempt) {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ p: 3, maxWidth: "64ch" }}>
        {noAttemptsReason(detail.data.state, detail.data.live)}
      </Typography>
    );
  } else {
    body = (
      <Box sx={{ display: "flex", height: "100%", minHeight: 0, [PHONE]: { flexDirection: "column" } }}>
        <AttemptList attempts={attempts} chosen={attempt.number} onChoose={setPicked} />
        <Box sx={{ flex: 1, minWidth: 0, overflowY: "auto", px: 3.5, pt: 3, pb: 11, [PHONE]: { px: 2, pb: 17.5 } }}>
          <AttemptView key={attempt.cycle.id} projectName={projectName} version={version} attempt={attempt} newest={attempt === newest} />
        </Box>
      </Box>
    );
  }

  return (
    <CardOverlay
      card="validation"
      title={`Validation ${version}`}
      fill
      actions={<Revalidate detail={detail.data} revalidate={revalidate} hasVerdict={hasVerdict} />}
    >
      <Box sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
        {revalidate.error && (
          <Alert severity="error" sx={{ m: 2, mb: 0 }} onClose={() => revalidate.reset()}>
            {revalidate.error.message}
          </Alert>
        )}
        <Box sx={{ flex: 1, minHeight: 0, overflowY: "auto" }}>{body}</Box>
      </Box>
    </CardOverlay>
  );
}

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
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Divider,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { useCancelRun, useCycleBuilds } from "../api/queries";
import { useRunProgress } from "../hooks/useRunProgress";
import { buildGlance } from "../lib/runGlance";
import {
  buildCycles,
  isTerminalRun,
  runHold,
  runKind,
  runKindLabel,
  runStateChip,
  spentBudgets,
  terminalReasonText,
} from "../lib/runView";
import { provisioningStage } from "../lib/provisioning";
import { sessionIssues, sessionStages } from "../lib/sessionSpine";
import { runDuration, runStamp } from "../lib/format";
import { ProvisioningGates } from "./ProvisioningGates";
import { RunDelivered } from "./RunDelivered";
import { RunGlanceStrip } from "./RunGlanceStrip";
import { RunHoldNotice } from "./RunHoldNotice";
import { RunNowPanel } from "./RunNowPanel";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type TaskView = components["schemas"]["TaskView"];

/**
 * The platform working, said quietly: a spinner sized to the title on a soft
 * surface. Deliberately NOT a RunHoldNotice — its leading-edge rule marks
 * something that needs reading, and its wrapping layout let the spinner break
 * onto its own line. Progress needs neither.
 */
function RunBusy({ title, body }: { title: string; body: string }) {
  return (
    <Stack
      direction="row"
      spacing={1.5}
      sx={{
        alignItems: "flex-start",
        p: 2,
        borderRadius: 1.5,
        bgcolor: (t) => alpha(t.palette.info.main, 0.06),
      }}
    >
      <CircularProgress
        size={18}
        thickness={4.5}
        aria-label={title}
        sx={{ mt: 0.25, flexShrink: 0 }}
      />
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="subtitle2">{title}</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.25 }}>
          {body}
        </Typography>
      </Box>
    </Stack>
  );
}

/**
 * One run of the version's milestone loop, NOW-FIRST.
 *
 * The flow is a strip — every stage on one line, the current one badged — and
 * only that stage gets words. This replaced a rail that rendered all six stages
 * expanded, each with its note, issues and a 420px log: correct for reading a
 * finished run end to end, and far too much for the question a reader actually
 * arrives with, which is what is happening right now.
 *
 * What is NOT lost: every stage keeps its note in the strip's tooltip, the
 * agent's log is one click down in the NOW panel's drawer, and each issue links
 * to its own page. What IS lost is reading every stage's detail at once — the
 * deliberate trade the redesign makes.
 *
 * Cancel is quiet and ABSENT while planning: cancel is a signal to the
 * supervisor, and during the plan window there is no supervisor yet to receive
 * it.
 */
export function RunStory({
  projectName,
  tag,
  run,
  milestone,
}: {
  projectName: string;
  tag: string;
  run: MilestoneRunView;
  /** The milestone's issue plane, or undefined while it is still loading. */
  milestone?: { gates: TaskView[]; work: TaskView[] };
}) {
  const cancel = useCancelRun(projectName, tag);
  // Cancel is ACCEPTED, not performed: the endpoint answers 202 the moment the
  // signal is queued, and the run only turns cancelled when the supervisor
  // processes it and the runs poll observes that — seconds later. isPending
  // covers just the HTTP round trip, so on its own the button re-armed while
  // the run was still winding down. This flag keeps it held from the click
  // until the run leaves the live state (the whole button unmounts then) —
  // released only by an error, which is the one case where clicking again is
  // the right thing to do.
  const [cancelRequested, setCancelRequested] = useState(false);
  const cancelling = cancel.isPending || (cancelRequested && !cancel.isError);
  const chip = runStateChip(run);
  const terminal = isTerminalRun(run.state);
  const planning = run.state === "planning";
  const work = milestone?.work ?? [];
  const gates = milestone?.gates ?? [];
  // runHold DEFERS an open-gate wait to the provisioning section — it expects
  // the gate story, with its who-is-acting rows and its "resolve connections"
  // way out, to be ON the card. The rail that carried it is gone, so the card
  // mounts ProvisioningGates itself whenever a gate is not yet resolved;
  // without this, a run stalled on a connection reads as a routine wait.
  const gateStage = provisioningStage(gates);
  const openGateStory =
    gateStage && gateStage.state !== "done" ? gateStage : null;

  const hold = runHold(
    run,
    milestone && {
      gates: milestone.gates,
      openWork: milestone.work.filter((t) => t.derivedStatus === "pending")
        .length,
    },
  );
  const reason = terminalReasonText(run.terminalReason ?? "");
  const spent = spentBudgets(run.budgets);
  const started = runStamp(run.startedAt ?? run.createdAt);
  const ended = runStamp(run.endedAt);
  // How long it has been going, or took — the design's "· 18 min". A live run
  // re-renders this on the runs poll, so it stays honest without a timer.
  const span = runDuration(run.startedAt ?? run.createdAt, run.endedAt);
  // A spent budget on a succeeded run is a footnote, not an alarm.
  const tone = run.state === "succeeded" ? "text.secondary" : "error.main";

  // Validation is not this surface's session — the deployment is what gets
  // validated, and its verdict renders there. But an OPEN validation cycle is
  // still the answer to "why is this session running when everything is
  // green", so the delivered banner names it.
  const cycles = buildCycles(run.cycles);
  const validating = run.cycles.some(
    (c) => c.kind === "validation" && !c.endedAt,
  );
  // The glance narrates the CURRENT build session. Earlier sessions of the same
  // run are history, listed below the card — putting them inside it turned the
  // card into the rail it replaced.
  const current = cycles.at(-1);

  // A settled run opens no stream until asked; a live one streams unprompted,
  // because it is what the reader came to watch.
  const [logRequested, setLogRequested] = useState(false);
  const showLog = !terminal || logRequested;
  const progress = useRunProgress(projectName, run.id, showLog);
  const { data: builds } = useCycleBuilds(
    projectName,
    tag,
    current?.id ?? "",
    Boolean(current?.mergeSha),
  );

  const stages = current ? sessionStages({ cycle: current, work, builds }) : [];
  // Numbered WITHIN the strip, not across the run. The rail this replaced
  // counted straight through every session (…6, 7, 8) because all of them were
  // on screen at once; the strip shows one session, so run-wide numbering read
  // as "step 7 of 5". Which session it is comes from the label above instead.
  const glance = buildGlance(stages);
  const issues = current ? sessionIssues(current, work) : undefined;
  const lines = current
    ? (progress.cycles.find((c) => c.cycle.id === current.id)?.lines ?? [])
    : [];

  // Is there anything below the header worth ruling off? The strip and NOW when
  // a cycle exists; the "never dispatched" line on a settled run; the gate
  // story when a connection is unresolved; the waiting notice otherwise —
  // unless a hold above is already saying it.
  const hasBody =
    Boolean(current) || terminal || Boolean(openGateStory) || !hold;

  // The card carries state in its EDGE, not a fill — but only for the two
  // states worth a ring: something is moving (info), or something broke
  // (error). Ringing a merely-waiting run in amber painted the whole card
  // orange for a state that is not an alarm, and the chip already says it.
  const edge = chip.tone === "info" || chip.tone === "error" ? chip.tone : null;

  return (
    <Card
      variant="outlined"
      sx={{
        bgcolor: (t) => alpha(t.palette.text.primary, 0.02),
        ...(edge && {
          borderColor: (t) => alpha(t.palette[edge].main, 0.35),
        }),
      }}
    >
      <CardContent sx={{ "&:last-child": { pb: 2.5 } }}>
        <Stack
          direction="row"
          spacing={1.5}
          sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 1 }}
        >
          {/* No version here either — the page header's picker owns it, and
              every run on this page belongs to that version by construction. */}
          <StatusChip
            label={chip.label}
            tone={chip.tone}
            appearance="soft"
            dot
          />
          <StatusChip
            label={runKindLabel(runKind(run))}
            tone="neutral"
            appearance="soft"
          />
          <Typography variant="body2" color="text.secondary">
            {started ? `Started ${started}` : ""}
            {ended ? ` → ${ended}` : ""}
            {span ? ` · ${span}` : ""}
          </Typography>
          <Box sx={{ flexGrow: 1 }} />
          {!terminal && !planning && (
            // Quiet by design: cancelling a session is a destructive escape
            // hatch, not the thing a reader came to do. A filled button here
            // competed with the stage strip for the eye and read as the page's
            // primary action.
            <Button
              size="small"
              color="inherit"
              variant="outlined"
              startIcon={<X size={16} />}
              disabled={cancelling}
              onClick={() => {
                setCancelRequested(true);
                cancel.mutate(run.id);
              }}
              // Neutral edge, taking its colour from the text: near-white on a
              // dark theme, near-black on a light one. A warning-coloured
              // outline made an escape hatch look like an alarm, and put a
              // second orange on a card that already has an orange accent.
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
        </Stack>

        {hold &&
          (hold.kind === "planning" ? (
            // Planning is progress, not a hold — same rule as the pre-dispatch
            // wait below: no leading-edge rule, spinner sized to the title.
            <Box sx={{ mt: 2 }}>
              <RunBusy title={hold.title} body={hold.body} />
            </Box>
          ) : (
            <RunHoldNotice
              tone={hold.tone}
              title={hold.title}
              body={hold.body}
            />
          ))}

        {cancel.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {cancel.error instanceof Error
              ? cancel.error.message
              : "Failed to cancel the run"}
            . Nothing was cancelled — you can retry.
          </Alert>
        )}

        {(reason || spent.length > 0) && (
          <Stack spacing={0.5} sx={{ mt: 2 }}>
            {reason && (
              <Typography variant="body2" color={tone}>
                {reason}
              </Typography>
            )}
            {spent.length > 0 && (
              <Typography
                variant="caption"
                color={tone}
                sx={{ fontVariantNumeric: "tabular-nums" }}
              >
                {`Budget spent: ${spent.map((b) => `${b.label} ${b.text}`).join(" · ")}`}
              </Typography>
            )}
          </Stack>
        )}

        {/* A planning run has provably no build sessions — the supervisor that
            dispatches them has not been started yet. And when a hold above has
            already explained the wait, there is no body to rule off — a divider
            with nothing under it reads as something that failed to render. */}
        {!planning && hasBody && (
          <>
            <Divider sx={{ my: 2 }} />

            {current ? (
              <Stack spacing={2}>
                <RunGlanceStrip
                  stages={glance.stages}
                  nowIndex={glance.nowIndex}
                />
                {/* Once every stage of the cycle is done AND the milestone has
                    nothing left, the result is the whole story — true the
                    moment deployment goes green, BEFORE the supervisor settles
                    the run, so the gate is the flow (nowIndex) plus the issue
                    plane, not run.state. Both halves matter: gating on
                    `succeeded` alone hid a finished flow behind a bare "every
                    stage is done" line, and gating on the flow alone claimed
                    "all agent work is done" on a run PARKED between sessions
                    with open issues still ahead — a fact the platform never
                    established. Failed and cancelled runs keep the panel,
                    because their story is where they stopped. */}
                {glance.nowIndex === null &&
                run.state !== "failed" &&
                run.state !== "cancelled" &&
                // The issue plane must have LOADED before the banner reads it:
                // work=[] also means "not arrived yet", and a banner that says
                // "no issues needed changing" and then rewrites itself is the
                // card contradicting itself across two seconds.
                milestone !== undefined &&
                (work.length > 0
                  ? work.every((t) => t.derivedStatus === "merged")
                  : run.state === "succeeded") ? (
                  <RunDelivered
                    projectName={projectName}
                    milestoneTitle={run.milestoneTitle}
                    cycles={cycles}
                    work={work}
                    validating={validating}
                    {...(run.endedAt ? { endedAt: run.endedAt } : {})}
                  />
                ) : (
                  <RunNowPanel
                    projectName={projectName}
                    glance={glance}
                    issues={issues?.issues ?? []}
                    {...(issues?.caption
                      ? { issuesCaption: issues.caption }
                      : {})}
                    lines={lines}
                    logPhase={progress.phase}
                    showLog={showLog}
                    onOpenLog={() => setLogRequested(true)}
                  />
                )}
              </Stack>
            ) : terminal ? (
              // Over, and it never started one. A spinner here would promise
              // work that is never coming.
              <Typography variant="body2" color="text.secondary">
                No build session was ever dispatched — the run settled before
                the supervisor started one.
              </Typography>
            ) : openGateStory ? (
              // The gate story runHold deferred here: each connection, who is
              // acting on it, and the way out — instead of a generic wait.
              <ProvisioningGates
                projectName={projectName}
                gates={gates}
                state={openGateStory.state}
              />
            ) : hold ? null : (
              // Live, no cycle yet, and no hold above already explaining the
              // wait: the supervisor is between dispatches. Busy rather than
              // static, because "nothing here" and "about to start" look
              // identical in text and are opposite things to a reader.
              //
              // Not a RunHoldNotice: this is progress, not a hold — the rule
              // down a notice's leading edge marks something that needs
              // reading, and a spinner already carries the "working" signal on
              // its own. A plain quiet surface, spinner sized to the title.
              <RunBusy
                title="Waiting to dispatch the first build session"
                body="Nothing has started yet — the supervisor dispatches a build session as soon as this run's predicate clears, and the stages appear here as it does."
              />
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

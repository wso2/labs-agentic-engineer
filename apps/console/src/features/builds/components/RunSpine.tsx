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
import { Box, Button, Stack, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useCycleBuilds } from "../api/queries";
import { connectionTail } from "../lib/feedTail";
import { useRunProgress, type RunProgressCycle, type RunProgressPhase } from "../hooks/useRunProgress";
import { provisioningStage } from "../lib/provisioning";
import { BUILD_CYCLE_KINDS, buildSessionLabel, isTerminalRun } from "../lib/runView";
import { SESSION_STAGE_COUNT, sessionIssues, sessionStages } from "../lib/sessionSpine";
import { AgentLogPanel } from "./AgentLogLines";
import { CycleBuilds } from "./CycleBuilds";
import { DeploymentStage } from "./DeploymentStage";
import { IssueChips } from "./IssueChips";
import { ProvisioningGates } from "./ProvisioningGates";
import { SpineLabel, StageRow } from "./StageRow";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type RunCycleView = components["schemas"]["RunCycleView"];
type TaskView = components["schemas"]["TaskView"];

// A RUN AS ONE NUMBERED FLOW.
//
//   1  Provisioning     the connections this version needs
//   2  Coding agent     the runner Job, up to the pull request it opens
//   3  Pull request     the handover from agent to platform
//   4  Merge            the auto-merge policy's decision
//   5  Builds           the fan-out one merge causes
//   6  Deployment       a green build deploys itself
//
// One rail, one sequence, numbered straight through. A fix or conflict session
// re-enters at Coding agent and keeps counting (7, 8, …) under a label saying
// which session it is — the loop is real, and pretending a re-entry is a fresh
// start would hide the very thing a reader is trying to understand.
//
// Provisioning leads because that is the order the platform works in: the
// dispatch predicate is run-level, so no session starts while any gate is open.
// It appears only when the version needs connections at all.
//
// This replaced a two-level layout — a run rail whose entries were collapsible
// sessions, each holding its own five-dot sub-spine. Two orderings on one screen
// is one too many; the dots summarising a sequence that is now always on screen
// summarised nothing.

/** Every stage's detail is inline, so the run feed is the only thing still
 *  fetched on demand: a settled run opens no stream until somebody asks for a
 *  log. See `logsOpen` below. */
function SessionStages({
  projectName,
  tag,
  cycle,
  index,
  work,
  lines,
  logPhase,
  /** This session's first stage number in the run's one flow. */
  stepFrom,
  /** Show the "Build session N" boundary above the first stage. */
  labelled,
  /** The run's last session draws no continuing rail below its last stage. */
  last,
  showLog,
  onOpenLog,
}: {
  projectName: string;
  tag: string;
  cycle: RunCycleView;
  index: number;
  work: TaskView[];
  lines: RunProgressCycle["lines"];
  logPhase: RunProgressPhase;
  stepFrom: number;
  labelled: boolean;
  last: boolean;
  showLog: boolean;
  onOpenLog: () => void;
}) {
  // Builds are DERIVED from the cluster on read, so they are not free the way
  // the run read is — but every session's Builds stage is on screen now, so
  // every merged session is asked. The query stops polling once its builds are
  // all complete, so a settled session costs one read and never asks again.
  const { data: builds } = useCycleBuilds(projectName, tag, cycle.id, Boolean(cycle.mergeSha));

  const stages = sessionStages({ cycle, work, builds });
  const issues = sessionIssues(cycle, work);
  // The agent's Job exits when it opens its pull request, so a running agent is
  // one with neither. That only sizes the log surface — whether the log is on
  // screen at all is the RUN's question, not this session's.
  const agentRunning = !cycle.endedAt && !cycle.prNumber;

  return (
    <>
      {labelled && <SpineLabel>{buildSessionLabel(cycle, index)}</SpineLabel>}
      {stages.map((stage, i) => {
        const lastStage = last && i === stages.length - 1;
        const step = stepFrom + i;
        if (stage.id === "agent") {
          return (
            <StageRow key={stage.id} stage={stage} step={step} last={lastStage}>
              <Stack spacing={1.5}>
                <IssueChips
                  projectName={projectName}
                  issues={issues.issues}
                  caption={issues.caption}
                />
                {/* The agent's own output, in the stage that owns it: tall while
                    it runs, shorter once it has exited — but never gone, because
                    that log is the record of how the code got written. */}
                {showLog ? (
                  <AgentLogPanel
                    lines={lines}
                    phase={logPhase}
                    agentRunning={agentRunning}
                    maxHeight={agentRunning ? 420 : 260}
                  />
                ) : (
                  // The one thing still behind a click. Attaching to the feed
                  // replays every session's history, which is not worth doing to
                  // a finished version nobody asked to read.
                  <Box>
                    <Button size="small" variant="outlined" onClick={onOpenLog}>
                      Show agent log
                    </Button>
                  </Box>
                )}
              </Stack>
            </StageRow>
          );
        }
        if (stage.id === "builds") {
          return (
            <StageRow key={stage.id} stage={stage} step={step} last={lastStage}>
              <CycleBuilds projectName={projectName} builds={builds} />
            </StageRow>
          );
        }
        if (stage.id === "deploy") {
          return (
            <StageRow key={stage.id} stage={stage} step={step} last={lastStage}>
              <DeploymentStage projectName={projectName} state={stage.state} />
            </StageRow>
          );
        }
        return <StageRow key={stage.id} stage={stage} step={step} last={lastStage} />;
      })}
    </>
  );
}

/**
 * One run's whole flow.
 *
 * The session RECORDS come from the run read, not from the stream: a session
 * exists the moment it is dispatched, and waiting for its first log line to
 * render it would hide the boot window entirely. The stream then fills each
 * session's lines in.
 */
export function RunSpine({
  projectName,
  tag,
  run,
  gates,
  work,
}: {
  projectName: string;
  tag: string;
  run: MilestoneRunView;
  /** Every gate in the milestone, resolved ones included. */
  gates: TaskView[];
  /** The milestone's agent-work issues, or [] while they load. */
  work: TaskView[];
}) {
  // Validation is not this surface's session — the deployment is what gets
  // validated, and its verdict renders there.
  const cycles = run.cycles.filter((c) =>
    (BUILD_CYCLE_KINDS as readonly string[]).includes(c.kind),
  );
  const provisioning = provisioningStage(gates);

  // A LIVE run streams without being asked — it is what the user came to watch,
  // and the log must not vanish the moment the agent opens its pull request and
  // the run moves on. A settled one opens no connection until somebody asks for
  // a log, which is what keeps a finished version from replaying every session's
  // history on load. One flag for the run, because there is one stream per run.
  const [logRequested, setLogRequested] = useState(false);
  const showLog = !isTerminalRun(run.state) || logRequested;
  const progress = useRunProgress(projectName, run.id, showLog);
  const linesByCycle = new Map(progress.cycles.map((c) => [c.cycle.id, c.lines]));

  // Step numbers are assigned from the session COUNT alone — every session
  // contributes exactly SESSION_STAGE_COUNT stages — so the rail can number
  // itself before any session's cluster-derived facts have arrived.
  const firstSessionStep = provisioning ? 2 : 1;

  if (cycles.length === 0) {
    return (
      <Box>
        {provisioning && (
          <StageRow stage={provisioning} step={1} last>
            <ProvisioningGates
              projectName={projectName}
              gates={gates}
              state={provisioning.state}
            />
          </StageRow>
        )}
        <Typography variant="body2" color="text.secondary">
          No build session has been dispatched yet — the run is waiting on its
          dispatch predicate.
        </Typography>
      </Box>
    );
  }

  // Connection only: a settled run is already drawn by the stage rows above, so
  // a second sentence saying it ended would be the same news twice.
  const tail = connectionTail(progress.phase);

  return (
    <Box>
      {provisioning && (
        <StageRow stage={provisioning} step={1}>
          <ProvisioningGates
            projectName={projectName}
            gates={gates}
            state={provisioning.state}
          />
        </StageRow>
      )}
      {cycles.map((cycle, i) => (
        <SessionStages
          key={cycle.id}
          projectName={projectName}
          tag={tag}
          cycle={cycle}
          index={i}
          work={work}
          lines={linesByCycle.get(cycle.id) ?? []}
          logPhase={progress.phase}
          stepFrom={firstSessionStep + i * SESSION_STAGE_COUNT}
          // The first session is the flow; a LATER one is a re-entry, and that
          // is what needs announcing.
          labelled={i > 0}
          last={i === cycles.length - 1}
          showLog={showLog}
          onOpenLog={() => setLogRequested(true)}
        />
      ))}
      {tail && (
        <Typography variant="caption" color="text.secondary" sx={{ pl: 3.25 }}>
          {tail}
        </Typography>
      )}
    </Box>
  );
}

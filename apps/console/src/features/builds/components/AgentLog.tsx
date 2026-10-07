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
import { Box, ButtonBase, Link, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { stamp } from "../../../lib/stamp";
import { useRunProgress, type RunProgressCycle, type RunProgressState } from "../hooks/useRunProgress";
import { agentLogLines, buildCycles } from "../model/run";
import { LogLines } from "./CardSection";

// The coding agent's log: what it did, newest first, one box per build
// session (a run's coding, fix or conflict cycle), each with its pull request.
// A version delivered by several runs (a run that fixed what an earlier one
// left) lists the earlier ones under it, each streaming only once opened.

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

function connection(phase: RunProgressState["phase"]): string | null {
  if (phase === "connecting") return "Attaching to the run's log…";
  if (phase === "reconnecting") return "Connection lost, reconnecting…";
  return null;
}

function Session({ section, label }: { section: RunProgressCycle; label: string }) {
  const { cycle, events } = section;
  return (
    <Box sx={{ "& + &": { borderTop: 1, borderColor: "divider" } }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: 1.75, py: 0.75 }}>
        <Typography variant="caption" sx={{ fontWeight: 600 }}>
          {label}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {cycle.kind} · {stamp(cycle.createdAt)}
          {cycle.endedAt ? "" : " · writing now"}
        </Typography>
        <Box sx={{ flex: 1 }} />
        {cycle.prUrl && cycle.prNumber ? (
          <Link href={cycle.prUrl} target="_blank" rel="noreferrer" variant="caption">
            PR #{cycle.prNumber}
          </Link>
        ) : null}
      </Box>
      <LogLines lines={agentLogLines(events)} empty="No output yet." maxHeight={280} />
    </Box>
  );
}

/** One run's build sessions, newest first, from its progress stream. */
function RunSessions({ progress, runNumber }: { progress: RunProgressState; runNumber: number | null }) {
  const sessions = progress.cycles.filter((c) => buildCycles([c.cycle]).length > 0);
  const note = connection(progress.phase);
  return (
    <Box>
      {sessions.length === 0 ? (
        <Typography variant="body2" color="text.secondary" sx={{ px: 1.75, py: 1.25 }}>
          {note ?? "No output yet: the run's first agent has not written a line."}
        </Typography>
      ) : (
        [...sessions].reverse().map((section, i) => {
          const n = sessions.length - i;
          return (
            <Session
              key={section.cycle.id}
              section={section}
              label={runNumber === null ? `Session ${n}` : `Run ${runNumber} · session ${n}`}
            />
          );
        })
      )}
      {note && sessions.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", px: 1.75, pb: 1 }}>
          {note}
        </Typography>
      )}
    </Box>
  );
}

function EarlierRun({ projectName, run, runNumber }: { projectName: string; run: MilestoneRunView; runNumber: number }) {
  const [open, setOpen] = useState(false);
  return (
    <Box sx={{ borderTop: 1, borderColor: "divider" }}>
      <ButtonBase
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        sx={{ width: "100%", justifyContent: "flex-start", px: 1.75, py: 1, "&:hover": { bgcolor: "action.hover" } }}
      >
        <Typography variant="caption" sx={{ fontWeight: 600 }}>
          Run {runNumber} · {stamp(run.createdAt)} · {run.state}
        </Typography>
      </ButtonBase>
      {open && <EarlierRunSessions projectName={projectName} runId={run.id} runNumber={runNumber} />}
    </Box>
  );
}

function EarlierRunSessions({ projectName, runId, runNumber }: { projectName: string; runId: string; runNumber: number }) {
  const progress = useRunProgress(projectName, runId);
  return <RunSessions progress={progress} runNumber={runNumber} />;
}

/**
 * The log of every run that delivered the version, newest first. The newest
 * run's stream is the card's own (it drives the phase strip too); an earlier
 * run opens its own when the reader opens it.
 */
export function AgentLog({
  projectName,
  runs,
  progress,
}: {
  projectName: string;
  /** Newest first. */
  runs: MilestoneRunView[];
  progress: RunProgressState;
}) {
  const [newest, ...earlier] = runs;
  if (!newest) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ px: 1.75, py: 1.25 }}>
        Nothing has started for this version yet: the log appears once its first build session does.
      </Typography>
    );
  }
  const numbered = runs.length > 1;
  return (
    <Box>
      <RunSessions progress={progress} runNumber={numbered ? runs.length : null} />
      {earlier.map((run, i) => (
        <EarlierRun key={run.id} projectName={projectName} run={run} runNumber={runs.length - 1 - i} />
      ))}
    </Box>
  );
}

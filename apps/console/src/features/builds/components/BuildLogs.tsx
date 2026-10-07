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
import { Alert, Box, Button, Skeleton, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useCycleBuilds } from "../api/runs";
import { useBuildLog } from "../hooks/useBuildLog";
import { componentBuildState, type ComponentBuildState } from "../model/run";
import { LogLines } from "./CardSection";

// The build logs: the component builds the version's merge fanned out to,
// each with its outcome and, opened, its log. Copied from the old console's
// build page (CycleBuilds, useBuildLog).

type CycleBuild = components["schemas"]["CycleBuild"];

const STATE: Record<ComponentBuildState, { colour: string; label: (b: CycleBuild) => string }> = {
  running: { colour: "primary.main", label: (b) => b.status || "Running" },
  succeeded: { colour: "success.main", label: () => "Succeeded" },
  failed: { colour: "error.main", label: () => "Failed" },
  other: { colour: "text.secondary", label: (b) => b.status || "Completed" },
};

function BuildLog({ projectName, build }: { projectName: string; build: CycleBuild }) {
  const log = useBuildLog(projectName, build.component, build.buildName);
  if (log.error) return <LogLines lines={[]} empty={log.error} />;
  if (log.loading) return <LogLines lines={[]} empty="Loading the build log…" />;
  const lines = log.entries.map((e) => e.log);
  return (
    <LogLines
      lines={log.complete ? lines : [...lines, "…tailing"]}
      empty={log.complete ? "No log kept for this build; its outcome is above." : "This build has not written anything yet."}
    />
  );
}

function BuildRow({ projectName, build }: { projectName: string; build: CycleBuild }) {
  const [open, setOpen] = useState(false);
  const state = STATE[componentBuildState(build)];
  return (
    <Box sx={{ "& + &": { borderTop: 1, borderColor: "divider" } }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.25, px: 1.75, py: 1, flexWrap: "wrap" }}>
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {build.component}
        </Typography>
        <Typography variant="caption" sx={{ color: state.colour }}>
          {state.label(build)}
        </Typography>
        {build.attempt > 1 && (
          // The one automatic re-run a red build gets: a second attempt means the first failed.
          <Typography variant="caption" color="warning.main">
            attempt {build.attempt}
          </Typography>
        )}
        <Box sx={{ flex: 1 }} />
        <Button size="small" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          {open ? "Hide log" : "Show log"}
        </Button>
      </Box>
      {open && <BuildLog projectName={projectName} build={build} />}
    </Box>
  );
}

/** The component builds of the version's newest merge; `cycleId` is that merge's cycle, absent before one. */
export function BuildLogs({ projectName, version, cycleId }: { projectName: string; version: string; cycleId: string | undefined }) {
  const builds = useCycleBuilds(projectName, version, cycleId);
  const say = (text: string) => (
    <Typography variant="body2" color="text.secondary" sx={{ px: 1.75, py: 1.25 }}>
      {text}
    </Typography>
  );
  if (!cycleId) return say("Build logs appear once a session's pull request merges and the components rebuild.");
  if (builds.isError) {
    return (
      <Alert severity="error" sx={{ m: 1.5 }} action={<Button onClick={() => void builds.refetch()}>Retry</Button>}>
        {builds.error.message}
      </Alert>
    );
  }
  if (!builds.data) return <Skeleton variant="rounded" height={48} sx={{ m: 1.5 }} />;
  if (builds.data.length === 0) return say("The merge has not reached the cluster's builds yet.");
  return (
    <Box>
      {builds.data.map((b) => (
        <BuildRow key={b.buildName} projectName={projectName} build={b} />
      ))}
    </Box>
  );
}

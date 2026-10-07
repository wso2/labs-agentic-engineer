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

import type { ReactNode } from "react";
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, ButtonBase, Skeleton, Typography } from "@wso2/oxygen-ui";
import { stamp } from "../../../lib/stamp";
import { projectLabel, useProject } from "../../projects/api/queries";
import { PHONE } from "../../shell/layout";
import { useBuilds } from "../api/builds";
import { useVersionLedger } from "../api/runs";
import { useBuildAction } from "../buildPicker";
import { useBuildOutcome } from "../hooks/useBuildOutcome";
import { isBuilding, versionRows, versionState, type VersionRow } from "../model/ledger";
import { offeredRows } from "../model/picker";
import { BuildButton } from "./BuildButton";

// Build history, a project's Page: the ledger of its versions, newest first,
// each with its state, what it built and when. A version opens its Build card
// over this page.

const RowLink = createLink(ButtonBase);

function names(list: string[]): string {
  return list.length <= 1 ? (list[0] ?? "") : `${list.slice(0, -1).join(", ")} and ${list.at(-1)}`;
}

/** Before the first build: what is ready for it, and the button that starts it. */
function NoBuilds({ projectName }: { projectName: string }) {
  const build = useBuildAction(projectName);
  const ready = build.offer ? offeredRows(build.offer).flatMap((r) => (r.kind === "feature" ? [r.name] : [])) : [];
  return (
    <Box sx={{ border: 1, borderStyle: "dashed", borderColor: "divider", borderRadius: 2.5, px: 2, py: 1.75, maxWidth: "64ch" }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        No builds yet.
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {ready.length
          ? `${names(ready)} ${ready.length === 1 ? "is" : "are"} designed and ready for ${build.offer?.version ?? "v1"}.`
          : "A build starts once something is designed. The spec and the design cards say what is left."}
      </Typography>
    </Box>
  );
}

const COLUMNS = "minmax(64px, 96px) minmax(110px, 160px) minmax(0, 1fr) minmax(110px, 140px)";

function HistoryRow({ projectName, row }: { projectName: string; row: VersionRow }) {
  const { outcome } = useBuildOutcome(projectName, isBuilding(row.status) ? undefined : row.version);
  const state = versionState(row.status, outcome, row.regressions);
  return (
    <RowLink
      to="/projects/$projectName/builds/$version"
      params={{ projectName, version: row.version }}
      sx={{
        display: "grid",
        gridTemplateColumns: COLUMNS,
        gap: 1.5,
        alignItems: "baseline",
        justifyContent: "stretch",
        textAlign: "start",
        px: 1.75,
        py: 1.25,
        "& + &": { borderTop: 1, borderColor: "divider" },
        "&:hover": { bgcolor: "action.hover" },
        [PHONE]: { gridTemplateColumns: "minmax(56px, 72px) minmax(0, 1fr)", rowGap: 0.25 },
      }}
    >
      <Typography component="span" sx={{ fontFamily: "monospace", fontWeight: 600 }}>
        {row.version}
      </Typography>
      <Typography component="span" variant="body2" sx={{ color: state.tone ? `${state.tone}.main` : "text.secondary" }}>
        {state.label}
      </Typography>
      <Typography component="span" variant="body2" color="text.secondary" sx={{ minWidth: 0, [PHONE]: { gridColumn: "2" } }}>
        {row.fixes ? `fixes ${row.fixes}` : row.features.join(", ") || "—"}
      </Typography>
      <Typography component="span" variant="caption" color="text.secondary" sx={{ textAlign: "end", [PHONE]: { gridColumn: "2", textAlign: "start" } }}>
        {stamp(row.completedAt ?? row.startedAt)}
      </Typography>
    </RowLink>
  );
}

export function BuildHistory({ projectName }: { projectName: string }) {
  const project = useProject(projectName);
  const ledger = useVersionLedger(projectName);
  const builds = useBuilds(projectName);
  const rows = ledger.data && builds.data ? versionRows(ledger.data, builds.data) : null;

  let body: ReactNode;
  if (ledger.isError || builds.isError) {
    const retry = () => void Promise.all([ledger.refetch(), builds.refetch()]);
    body = (
      <Alert severity="error" action={<Button onClick={retry}>Retry</Button>}>
        Couldn't load the builds: {(ledger.error ?? builds.error)?.message}
      </Alert>
    );
  } else if (!rows) {
    body = <Skeleton variant="rounded" height={160} />;
  } else if (rows.length === 0) {
    body = <NoBuilds projectName={projectName} />;
  } else {
    body = (
      <Box component="nav" aria-label="Versions" sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
        <Box
          aria-hidden
          sx={{
            display: "grid",
            gridTemplateColumns: COLUMNS,
            gap: 1.5,
            px: 1.75,
            py: 0.75,
            bgcolor: "background.default",
            borderBottom: 1,
            borderColor: "divider",
            "& > *": { fontSize: "0.6875rem", letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" },
            [PHONE]: { display: "none" },
          }}
        >
          <span>Version</span>
          <span>Status</span>
          <span>Features</span>
          <Box component="span" sx={{ textAlign: "end" }}>
            When
          </Box>
        </Box>
        {rows.map((row) => (
          <HistoryRow key={row.version} projectName={projectName} row={row} />
        ))}
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, maxWidth: 960 }}>
      <Box sx={{ display: "flex", alignItems: "flex-end", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1 }}>
          <Typography variant="caption" color="text.secondary">
            {project.data ? projectLabel(project.data) : projectName}
          </Typography>
          <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
            Builds
          </Typography>
        </Box>
        <BuildButton projectName={projectName} />
      </Box>
      {body}
    </Box>
  );
}

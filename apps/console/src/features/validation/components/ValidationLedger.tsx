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
import type { components } from "../../../generated/aep-api";
import { stamp } from "../../../lib/stamp";
import { projectLabel, useProject } from "../../projects/api/queries";
import { PHONE } from "../../shell/layout";
import { useValidation, useValidations } from "../api/validations";
import { verdictView } from "../model/attempts";

// The Validation ledger, a project's Page: every version's validation, newest
// version first, with its verdict, how many attempts it took and when it was
// last judged. A version opens its Validation card over this page.

type ValidationSummary = components["schemas"]["ValidationSummary"];

const RowLink = createLink(ButtonBase);

const COLUMNS = "minmax(64px, 96px) minmax(120px, 1fr) minmax(80px, 110px) minmax(110px, 140px)";

/** How many attempts the version took: its own read, not polled (the ledger's poll says when it moves). */
function AttemptCount({ projectName, tag }: { projectName: string; tag: string }) {
  const detail = useValidation(projectName, tag, false);
  if (!detail.data) return <>…</>;
  const n = detail.data.runs.reduce((sum, r) => sum + r.cycles.length, 0);
  return <>{n === 0 ? "none" : `${n} attempt${n === 1 ? "" : "s"}`}</>;
}

function LedgerRow({ projectName, row }: { projectName: string; row: ValidationSummary }) {
  const verdict = verdictView(row.state);
  return (
    <RowLink
      to="/projects/$projectName/validations/$version"
      params={{ projectName, version: row.tag }}
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
        {row.tag}
      </Typography>
      <Typography component="span" variant="body2" sx={{ color: verdict?.tone ? `${verdict.tone}.main` : "text.secondary" }}>
        {verdict?.label ?? "Not validated"}
      </Typography>
      <Typography component="span" variant="body2" color="text.secondary" sx={{ [PHONE]: { gridColumn: "2" } }}>
        <AttemptCount projectName={projectName} tag={row.tag} />
      </Typography>
      <Typography component="span" variant="caption" color="text.secondary" sx={{ textAlign: "end", [PHONE]: { gridColumn: "2", textAlign: "start" } }}>
        {stamp(row.endedAt ?? row.startedAt) || "—"}
      </Typography>
    </RowLink>
  );
}

export function ValidationLedger({ projectName }: { projectName: string }) {
  const project = useProject(projectName);
  const validations = useValidations(projectName);

  let body: ReactNode;
  if (validations.isError) {
    body = (
      <Alert severity="error" action={<Button onClick={() => void validations.refetch()}>Retry</Button>}>
        {validations.error.message}
      </Alert>
    );
  } else if (!validations.data) {
    body = <Skeleton variant="rounded" height={160} />;
  } else if (validations.data.length === 0) {
    body = (
      <Box sx={{ border: 1, borderStyle: "dashed", borderColor: "divider", borderRadius: 2.5, px: 2, py: 1.75, maxWidth: "64ch" }}>
        <Typography variant="body2" color="text.secondary">
          Nothing to validate yet. Once a version is built and deployed, it is checked against the acceptance scenarios
          in its spec, and the result is listed here.
        </Typography>
      </Box>
    );
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
          <span>Verdict</span>
          <span>Attempts</span>
          <Box component="span" sx={{ textAlign: "end" }}>
            When
          </Box>
        </Box>
        {validations.data.map((row) => (
          <LedgerRow key={row.tag} projectName={projectName} row={row} />
        ))}
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, maxWidth: 960 }}>
      <Box>
        <Typography variant="caption" color="text.secondary">
          {project.data ? projectLabel(project.data) : projectName}
        </Typography>
        <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
          Validation
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          Each version, once deployed, is checked against the acceptance scenarios in its spec.
        </Typography>
      </Box>
      {body}
    </Box>
  );
}

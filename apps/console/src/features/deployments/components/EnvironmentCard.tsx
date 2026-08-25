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
import {
  Box,
  Card,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import { CircleCheck, CircleAlert, Lock } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { StatusChip } from "../../../components/StatusChip";
import { runStamp } from "../../builds/lib/format";
import { deploymentChip, validationCell } from "../lib/status";

type ProjectDeployment = components["schemas"]["ProjectDeployment"];

/**
 * One environment, as its own column (ADR-0020 §5).
 *
 * The board this belongs to replaced a single top-to-bottom rail. The rail read
 * well for "promote this version" and badly for "what is running where" — which
 * is the question this page is opened with, and which two columns answer at a
 * glance.
 */
export function EnvironmentCard({
  name,
  deployment,
  emptyNote,
  gateNote,
  action,
}: {
  name: string;
  /** The newest deployment in this environment, or undefined if none. */
  deployment: ProjectDeployment | undefined;
  /** What this environment says when nothing has ever reached it. */
  emptyNote: string;
  /** The rule that governs getting here — shown when there is no action. */
  gateNote?: string | undefined;
  action?: ReactNode;
}) {
  const chip = deployment ? deploymentChip(deployment) : null;
  const verdict = deployment ? validationCell(deployment.validation) : null;
  const healthy = chip?.tone === "success";

  return (
    <Card
      variant="outlined"
      sx={{
        p: 2.25,
        height: "100%",
        // A live environment is bordered in its own state colour, so the two
        // columns are distinguishable before either is read.
        ...(healthy && { borderColor: "success.main" }),
        ...(chip?.tone === "error" && { borderColor: "error.main" }),
      }}
    >
      <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
        <Typography variant="h6" sx={{ fontWeight: 700, letterSpacing: "-0.02em" }}>
          {name}
        </Typography>
        {chip ? (
          <StatusChip
            label={chip.label}
            tone={chip.tone}
            appearance="soft"
            dot
          />
        ) : (
          <StatusChip label="Nothing deployed" tone="neutral" appearance="soft" />
        )}
        <Box sx={{ flex: 1 }} />
        {deployment && (
          <Typography variant="caption" color="text.secondary">
            {runStamp(deployment.deployedAt)}
          </Typography>
        )}
      </Stack>

      <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
        {deployment ? (
          <>
            Running{" "}
            <Box component="span" sx={{ fontWeight: 600, color: "text.primary" }}>
              {deployment.tag}
            </Box>
            {/* The design shows "· 4 of 4 components live" here. That count
                lives on the deployment DETAIL read, and fetching it per card
                would cost one extra request per environment for a fragment of a
                sentence — the card links to where it is already shown. */}
          </>
        ) : (
          emptyNote
        )}
      </Typography>

      {/* The validation strip. Present only when there is a deployment to have
          judged — an environment with nothing in it has no verdict, and showing
          "Not run" there would read as a failure rather than an absence. */}
      {deployment && verdict && (
        <Stack
          direction="row"
          spacing={1.25}
          sx={{
            alignItems: "center",
            mt: 1.75,
            px: 1.5,
            py: 1.25,
            borderRadius: 1,
            border: 1,
            borderColor: (t) =>
              verdict.tone === "success"
                ? alpha(t.palette.success.main, 0.4)
                : "divider",
            bgcolor: (t) =>
              verdict.tone === "success"
                ? alpha(t.palette.success.main, 0.05)
                : "transparent",
          }}
        >
          {verdict.tone === "success" ? (
            <CircleCheck size={15} aria-hidden />
          ) : (
            <CircleAlert size={15} aria-hidden />
          )}
          <Typography
            variant="caption"
            sx={{ flex: 1, minWidth: 0, color: `${verdict.tone === "neutral" ? "text.secondary" : `${verdict.tone}.main`}` }}
          >
            {verdict.tone === "success"
              ? `Validation passed — ${verdict.label}`
              : verdict.label === "Not run"
                ? "Validation has not run for this deployment"
                : `Validation — ${verdict.label}`}
          </Typography>
        </Stack>
      )}

      {action ? (
        <Box sx={{ mt: 1.5 }}>{action}</Box>
      ) : gateNote ? (
        // The rule, stated where the button would be. A disabled button with no
        // explanation is the thing this replaces.
        <Stack
          direction="row"
          spacing={1.25}
          sx={{
            alignItems: "center",
            mt: 1.75,
            px: 1.5,
            py: 1.25,
            borderRadius: 1,
            border: 1,
            borderStyle: "dashed",
            borderColor: "divider",
            bgcolor: "action.hover",
          }}
        >
          <Lock size={14} aria-hidden />
          <Typography variant="caption" color="text.secondary" sx={{ flex: 1, minWidth: 0 }}>
            {gateNote}
          </Typography>
        </Stack>
      ) : null}
    </Card>
  );
}

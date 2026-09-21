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

import { Box, Button, Stack, Typography } from "@wso2/oxygen-ui";
import { Info } from "@wso2/oxygen-ui-icons-react";
import type { StatusTone } from "../../../components/StatusChip";
import type { ConnectionTableRow } from "../lib/deploymentDetail";
import type { ConnectionLine } from "../lib/deploymentFlow";
import type { ConnectionRow } from "../lib/promotion";
import { PageSection } from "./PageSection";

// The environment's Dependencies table (ADR-0032, the Deployment Detail
// design; §6 section 3): what the design depends on, who uses it, the keys it
// carries, and whether this environment holds values for it. The word is
// "dependency" throughout — the design's own — where this surface used to say
// "connection". The keys are MASKED, always —
// nothing reads a value back, so the column says what is set rather than what
// it is set to. Edit re-collects a Project External's values in development
// (the board's own surface); a platform-provisioned connection carries no
// action — the design already reads on the Spec view. What the design drew
// and this does not build: the value itself,
// and "Compare with Production" — production values live only in the promote
// dialog, as page state.

const TONE: Record<ConnectionLine["state"], StatusTone> = {
  set: "success",
  provisioned: "success",
  missing: "warning",
  platform: "neutral",
  unknown: "neutral",
};

const COLUMNS = "minmax(0, 1.1fr) 140px minmax(0, 1.3fr) 130px 88px";

export function DependenciesTable({
  environmentLabel: label,
  rows,
  onEdit,
}: {
  /** What to call the environment on screen — the pipeline's display name,
   *  whatever it called it. */
  environmentLabel: string;
  rows: ConnectionTableRow[];
  onEdit: (row: ConnectionRow) => void;
}) {
  return (
    <PageSection
      title="Dependencies"
      caption={`${rows.length} dependenc${rows.length === 1 ? "y" : "ies"} · values for ${label}`}
      flush
    >
      {rows.length === 0 ? (
        <Typography variant="body2" color="text.secondary" sx={{ px: 2.25, py: 1.5 }}>
          This design declares no dependencies.
        </Typography>
      ) : (
        <Box role="table" aria-label={`Dependencies on ${label}`}>
          <Box
            role="row"
            sx={{
              display: "grid",
              gridTemplateColumns: COLUMNS,
              gap: 1.5,
              px: 2.25,
              py: 0.75,
              bgcolor: "action.hover",
            }}
          >
            {["Dependency", "Type", `Keys on ${label}`, "Status", ""].map((h, i) => (
              <Typography
                key={i}
                role="columnheader"
                variant="caption"
                color="text.secondary"
                sx={{ fontWeight: 700, letterSpacing: "0.06em", textTransform: "uppercase" }}
              >
                {h}
              </Typography>
            ))}
          </Box>
          {rows.map(({ line, type, usedBy, keys }) => (
            <Box
              key={line.row.id}
              role="row"
              aria-label={line.row.name}
              sx={{
                display: "grid",
                gridTemplateColumns: COLUMNS,
                gap: 1.5,
                alignItems: "center",
                px: 2.25,
                py: 1.25,
                borderTop: 1,
                borderColor: "divider",
              }}
            >
              <Box role="cell" sx={{ minWidth: 0 }}>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>
                  {line.row.name}
                </Typography>
                {usedBy.length > 0 && (
                  <Typography variant="caption" color="text.secondary">
                    used by {usedBy.join(", ")}
                  </Typography>
                )}
              </Box>
              <Typography role="cell" variant="body2" color="text.secondary">
                {type}
              </Typography>
              <Box role="cell" sx={{ minWidth: 0, display: "flex", flexWrap: "wrap", gap: 1 }}>
                {keys.length > 0 ? (
                  keys.map((k) => (
                    <Typography
                      key={k}
                      component="span"
                      variant="caption"
                      sx={{ fontFamily: "monospace", whiteSpace: "nowrap" }}
                    >
                      {k} <Box component="span" sx={{ color: "text.disabled" }} aria-label="masked">••••</Box>
                    </Typography>
                  ))
                ) : (
                  <Typography variant="caption" color="text.secondary">
                    provisioned by the platform — nothing to enter
                  </Typography>
                )}
              </Box>
              <Stack role="cell" direction="row" spacing={0.75} sx={{ alignItems: "center" }}>
                <Box
                  aria-hidden
                  sx={{
                    width: 6,
                    height: 6,
                    borderRadius: "50%",
                    bgcolor: TONE[line.state] === "neutral" ? "text.disabled" : `${TONE[line.state]}.main`,
                  }}
                />
                <Typography
                  variant="caption"
                  sx={{ color: TONE[line.state] === "neutral" ? "text.secondary" : `${TONE[line.state]}.main` }}
                >
                  {line.label || "Unknown"}
                </Typography>
              </Stack>
              <Box role="cell" sx={{ display: "flex", justifyContent: "flex-end" }}>
                {line.configure && (
                  <Button
                    size="small"
                    variant="outlined"
                    color="inherit"
                    aria-label={`Edit ${line.row.name} values`}
                    onClick={() => onEdit(line.row)}
                  >
                    Edit
                  </Button>
                )}
              </Box>
            </Box>
          ))}
        </Box>
      )}
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: "center", px: 2.25, py: 1, borderTop: 1, borderColor: "divider" }}
      >
        <Box component={Info} size={13} aria-hidden sx={{ color: "text.secondary", flexShrink: 0 }} />
        <Typography variant="caption" color="text.secondary">
          Changing a value re-provisions the dependency on this environment. Secrets are sealed by the platform and never shown.
        </Typography>
      </Stack>
    </PageSection>
  );
}

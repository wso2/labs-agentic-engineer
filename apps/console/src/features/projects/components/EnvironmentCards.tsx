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
  Box,
  Button,
  Card,
  CardContent,
  Divider,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import { ArrowRight, Lock } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import type { ValidationCounts } from "../../validation/lib/verdict";
import { agoLabel, type EnvironmentRow } from "../lib/deploymentLedger";
import { canPromote } from "../lib/promotion";
import { ProjectSignInPanel } from "./SignInPanel";
import { VerdictBanner } from "./VerdictBanner";

type DeployStage = components["schemas"]["DeployStage"];

/** The card's frame: outlined, and edged in the environment's own colour only
 *  when it has something to say — green when serving, red when broken, blue
 *  while moving. A quiet environment keeps the plain divider. */
function EnvironmentCard({
  row,
  children,
}: {
  row: EnvironmentRow;
  children: React.ReactNode;
}) {
  const tone = row.status.tone;
  return (
    <Card
      variant="outlined"
      // Both cards fill the row's height, so the pair reads as one band
      // whatever either of them has to say. Development is always the taller —
      // it carries the verdict, the promotion and the test users — and letting
      // Production stop short of it left the board looking half-drawn.
      sx={{
        height: "100%",
        ...(tone !== "neutral" && {
          borderColor: (t) => alpha(t.palette[tone === "primary" ? "primary" : tone].main, 0.35),
        }),
      }}
    >
      <CardContent sx={{ "&:last-child": { pb: 2.25 } }}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 0.5 }}>
          <Typography variant="h6" sx={{ fontWeight: 700, letterSpacing: "-0.01em" }}>
            {row.label}
          </Typography>
          <StatusChip
            label={row.status.label}
            tone={row.status.tone}
            appearance="soft"
            dot
          />
          <Box sx={{ flex: 1 }} />
          {row.deployedAt && (
            <Typography variant="caption" color="text.secondary">
              {agoLabel(row.deployedAt)}
            </Typography>
          )}
        </Stack>
        {children}
      </CardContent>
    </Card>
  );
}

/** "Running v1 · 4 of 4 components live" — the card's one-line fact. */
function RunningLine({ row }: { row: EnvironmentRow }) {
  const bound = row.cards.filter((c) => c.deployment).length;
  if (bound === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
        {row.environment === "development"
          ? "Nothing running yet — agents deploy to dev when a build merges."
          : "Nothing running yet"}
      </Typography>
    );
  }
  return (
    <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
      Running{" "}
      {row.version ? (
        <Box component="span" sx={{ fontWeight: 600, color: "text.primary" }}>
          {row.version}
        </Box>
      ) : null}
      {row.version ? " · " : ""}
      {row.live} of {row.total} components live
    </Typography>
  );
}

/**
 * The two environment cards (ADR-0027, artboard 1c): Development carries the
 * verdict and the promotion, Production carries the gate. Both say what runs
 * there and how much of it is up.
 */
export function EnvironmentCards({
  projectName,
  development,
  production,
  deploy,
  validation,
  connectionCount,
  configured,
  onPromote,
}: {
  projectName: string;
  development: EnvironmentRow;
  production: EnvironmentRow;
  deploy?: DeployStage | undefined;
  /** The dev deployment's validation evidence. */
  validation: { verdict: string; repairing: boolean; counts?: ValidationCounts | undefined };
  /** Connections needing production values; null while the read is in flight. */
  connectionCount: number | null;
  /** How many of them already hold their production values. */
  configured: number;
  onPromote: () => void;
}) {
  // Promotion is offered only while production is empty and dev has a version
  // to offer; whether it is ENABLED is the deploy aggregate's call (canPromote).
  const promotable = Boolean(deploy?.version && production.cards.length === 0);
  const devGreen =
    deploy?.status === "deployed" &&
    development.total > 0 &&
    development.live === development.total;

  return (
    <Box
      sx={{
        display: "grid",
        gap: 2,
        // stretch, not start: the two cards share a height (see EnvironmentCard).
        alignItems: "stretch",
        gridTemplateColumns: { xs: "1fr", md: "repeat(2, minmax(0, 1fr))" },
      }}
    >
      <EnvironmentCard row={development}>
        <RunningLine row={development} />
        {deploy && (
          <Box sx={{ mt: 1.75 }}>
            <VerdictBanner
              projectName={projectName}
              validation={deploy.validation}
              verdict={validation.verdict}
              repairing={validation.repairing}
              {...(validation.counts ? { counts: validation.counts } : {})}
            />
          </Box>
        )}
        {deploy && promotable && (
          <Stack
            direction="row"
            spacing={1.5}
            sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 1, mt: 1.5 }}
          >
            <span
              {...(!canPromote(deploy) && {
                title: "Enabled once the dev deployment settles and validation has its say",
              })}
            >
              <Button
                variant="contained"
                disabled={!canPromote(deploy)}
                onClick={onPromote}
                endIcon={<ArrowRight size={16} aria-hidden />}
              >
                Promote {deploy.version} to production
              </Button>
            </span>
            <Typography variant="caption" color="text.secondary">
              Opens a dialog to collect live configuration.
            </Typography>
          </Stack>
        )}
        {devGreen && (
          <>
            <Divider sx={{ my: 2 }} />
            <ProjectSignInPanel projectName={projectName} />
          </>
        )}
      </EnvironmentCard>

      <EnvironmentCard row={production}>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          {production.cards.length > 0 ? (
            <>
              Running · {production.live} of {production.total} components live
            </>
          ) : (
            <>
              Nothing running yet
              {connectionCount !== null && connectionCount > 0 && (
                <>
                  {" · "}
                  <Box
                    component="span"
                    sx={{
                      color: configured === connectionCount ? "success.main" : "warning.main",
                    }}
                  >
                    {configured} of {connectionCount} live configuration values set
                  </Box>
                </>
              )}
            </>
          )}
        </Typography>
        {production.cards.length === 0 && (
          <Stack
            direction="row"
            spacing={1.25}
            sx={{
              alignItems: "center",
              mt: 1.75,
              px: 1.5,
              py: 1.25,
              border: 1,
              borderStyle: "dashed",
              borderColor: "divider",
              borderRadius: 2,
              bgcolor: "action.hover",
            }}
          >
            <Box component={Lock} size={14} aria-hidden sx={{ color: "text.secondary", flexShrink: 0 }} />
            <Typography variant="body2" color="text.secondary">
              Only a version whose validation has passed can be promoted here.
            </Typography>
          </Stack>
        )}
      </EnvironmentCard>
    </Box>
  );
}

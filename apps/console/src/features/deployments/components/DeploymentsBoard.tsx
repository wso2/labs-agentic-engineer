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

import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  ListingTable,
  MenuItem,
  Skeleton,
  Stack,
  TextField,
  Typography,
  alpha,
  type Theme,
} from "@wso2/oxygen-ui";
import { ArrowRight, Rocket, RotateCcw } from "@wso2/oxygen-ui-icons-react";
import { Link, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { runStamp } from "../../builds/lib/format";
import { buildDuration, secondsDuration } from "../../builds/lib/ledger";
import { useProjectDeployments } from "../api/queries";
import {
  currentDeployment,
  deploymentChip,
  isDeploymentLive,
  isPromotable,
  validationCell,
} from "../lib/status";
import { EnvironmentCard } from "./EnvironmentCard";

type ProjectDeployment = components["schemas"]["ProjectDeployment"];

/**
 * Deployments: two environment cards, then every deployment (ADR-0020 §5).
 *
 * Both cards derive from the SAME list the table renders, so they can never
 * disagree about what is running — one read, one truth.
 */

const DEVELOPMENT = "development";
const PRODUCTION = "production";

const COLUMNS = [
  { key: "version", label: "Version", width: 90 },
  { key: "milestone", label: "Milestone" },
  { key: "environment", label: "Environment", width: 130 },
  { key: "status", label: "Status", width: 140 },
  { key: "validation", label: "Validation", width: 165 },
  { key: "duration", label: "Duration", width: 105 },
  { key: "deployed", label: "Deployed", width: 140 },
];

const ENV_FILTERS = [
  { value: "all", label: "All environments" },
  { value: DEVELOPMENT, label: "Development" },
  { value: PRODUCTION, label: "Production" },
];

export function DeploymentsBoard({
  projectName,
  onPromote,
  onRedeploy,
}: {
  projectName: string;
  /** Opens the existing promote dialog — unchanged, only relocated. */
  onPromote?: (deployment: ProjectDeployment) => void;
  /** Redeploy has no platform surface yet; the page owns saying so. */
  onRedeploy?: () => void;
}) {
  const deployments = useProjectDeployments(projectName);
  const navigate = useNavigate();
  const [envFilter, setEnvFilter] = useState("all");

  // Memoised because `?? []` mints a NEW array on every render while the query
  // has no data, which would make both useMemo deps below change every time and
  // defeat them entirely.
  const items = useMemo(() => deployments.data ?? [], [deployments.data]);
  const dev = useMemo(() => currentDeployment(items, DEVELOPMENT), [items]);
  const prod = useMemo(() => currentDeployment(items, PRODUCTION), [items]);

  const rows = useMemo(
    () => (envFilter === "all" ? items : items.filter((d) => d.environment === envFilter)),
    [items, envFilter],
  );

  const backTo = {
    link: <Link to="/projects/$projectName" params={{ projectName }} />,
    label: "Back to Overview",
  };

  if (deployments.isPending) {
    return (
      <>
        <PageHeader title="Deployments" backTo={backTo} />
        <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" } }}>
          <Skeleton variant="rounded" height={180} />
          <Skeleton variant="rounded" height={180} />
        </Box>
        <Skeleton variant="rounded" height={220} sx={{ mt: 2 }} />
      </>
    );
  }

  if (deployments.isError) {
    return (
      <>
        <PageHeader title="Deployments" backTo={backTo} />
        <Alert
          severity="error"
          action={<Button onClick={() => void deployments.refetch()}>Retry</Button>}
        >
          Failed to load deployments
          {deployments.error instanceof Error && deployments.error.message
            ? `: ${deployments.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  const promotable = isPromotable(dev);

  return (
    <>
      <PageHeader
        title="Deployments"
        backTo={backTo}
        actions={
          <Button
            variant="outlined"
            startIcon={<RotateCcw size={15} />}
            disabled={!dev || !onRedeploy}
            sx={{ borderRadius: 999 }}
            onClick={() => onRedeploy?.()}
          >
            Redeploy
          </Button>
        }
      />

      {/* The two environments, side by side (ADR-0020 §5). */}
      <Box
        sx={{
          display: "grid",
          gap: 2,
          gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
          mb: 2,
        }}
      >
        <EnvironmentCard
          name="Development"
          deployment={dev}
          emptyNote="Nothing running yet — a version deploys here as its tasks merge."
          {...(promotable && onPromote && dev
            ? {
                action: (
                  <Button
                    variant="contained"
                    endIcon={<ArrowRight size={15} />}
                    sx={{ borderRadius: 999 }}
                    onClick={() => onPromote(dev)}
                  >
                    Promote {dev.tag} to production
                  </Button>
                ),
              }
            : dev
              ? {
                  gateNote:
                    "Only a version whose validation has passed can be promoted to production.",
                }
              : {})}
        />
        <EnvironmentCard
          name="Production"
          deployment={prod}
          emptyNote="Nothing running yet — promote a validated version from development."
          {...(prod
            ? {}
            : {
                gateNote:
                  "Only a version whose validation has passed can be promoted here.",
              })}
        />
      </Box>

      {items.length === 0 ? (
        <EmptyState
          icon={<Rocket size={48} />}
          title="No deployments yet"
          description="A version deploys to development as its tasks merge. Nothing has reached an environment yet."
        />
      ) : (
        <ListingTable.Container sx={{ width: "100%" }}>
          <ListingTable.Toolbar
            actions={
              <TextField
                select
                size="small"
                value={envFilter}
                onChange={(e) => setEnvFilter(e.target.value)}
                label="Environment"
                sx={{ width: 200 }}
              >
                {ENV_FILTERS.map((o) => (
                  <MenuItem key={o.value} value={o.value}>
                    {o.label}
                  </MenuItem>
                ))}
              </TextField>
            }
          >
            <Stack direction="row" spacing={1.25} sx={{ alignItems: "baseline" }}>
              <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
                Deployments
              </Typography>
              <Typography variant="caption" color="text.secondary">
                all environments · last 30 days
              </Typography>
            </Stack>
          </ListingTable.Toolbar>

          {rows.length === 0 ? (
            <EmptyState
              compact
              description="No deployments in that environment. Clear the filter to see them all."
              action={<Button onClick={() => setEnvFilter("all")}>Clear filter</Button>}
            />
          ) : (
            <ListingTable density="standard">
              <ListingTable.Head>
                <ListingTable.Row>
                  {COLUMNS.map((c) => (
                    <ListingTable.Cell key={c.key} sx={{ width: c.width ?? "auto" }}>
                      {c.label}
                    </ListingTable.Cell>
                  ))}
                </ListingTable.Row>
              </ListingTable.Head>
              <ListingTable.Body>
                {rows.map((d) => (
                  <DeploymentRow
                    key={d.id}
                    deployment={d}
                    onOpen={() =>
                      void navigate({
                        to: "/projects/$projectName/deployments/$deploymentId",
                        params: { projectName, deploymentId: d.id },
                      })
                    }
                  />
                ))}
              </ListingTable.Body>
            </ListingTable>
          )}
        </ListingTable.Container>
      )}
    </>
  );
}

function DeploymentRow({
  deployment,
  onOpen,
}: {
  deployment: ProjectDeployment;
  onOpen: () => void;
}) {
  const chip = deploymentChip(deployment);
  const verdict = validationCell(deployment.validation);
  const live = isDeploymentLive(deployment);
  // A settled deployment reports its own span; one still going counts up from
  // the deploy stamp rather than showing nothing.
  const duration =
    secondsDuration(deployment.durationSeconds) ||
    buildDuration(deployment.deployedAt) ||
    "—";

  return (
    <ListingTable.Row
      hover
      clickable
      onClick={onOpen}
      {...(live
        ? { sx: { bgcolor: (t: Theme) => alpha(t.palette.info.main, 0.05) } }
        : {})}
    >
      <ListingTable.Cell>
        <Typography
          variant="subtitle2"
          sx={{ fontWeight: 600, fontVariantNumeric: "tabular-nums" }}
        >
          {deployment.tag}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell sx={{ minWidth: 0 }}>
        <Typography
          variant="body2"
          sx={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}
          title={deployment.milestoneTitle ?? undefined}
        >
          {deployment.milestoneTitle || "—"}
        </Typography>
        {deployment.commit?.sha && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ fontFamily: "monospace", display: "block", mt: 0.25 }}
          >
            {deployment.commit.sha.slice(0, 7)}
            {deployment.commit.branch ? ` · ${deployment.commit.branch}` : ""}
          </Typography>
        )}
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" color="text.secondary" sx={{ textTransform: "capitalize" }}>
          {deployment.environment}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        <StatusChip label={chip.label} tone={chip.tone} appearance="soft" dot />
      </ListingTable.Cell>

      <ListingTable.Cell>
        {/* A verdict gets a pill; a run in flight stays a sentence, because
            "18 of 24 checked" is progress, not a result. */}
        {verdict.chip ? (
          <StatusChip label={verdict.label} tone={verdict.tone} appearance="soft" dot />
        ) : (
          <Typography
            variant="caption"
            sx={{ color: verdict.tone === "info" ? "info.main" : "text.secondary" }}
          >
            {verdict.label}
          </Typography>
        )}
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums" }}>
          {duration}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" color="text.secondary">
          {runStamp(deployment.deployedAt)}
        </Typography>
      </ListingTable.Cell>
    </ListingTable.Row>
  );
}

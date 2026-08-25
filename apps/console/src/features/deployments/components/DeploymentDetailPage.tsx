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
  Avatar,
  Box,
  Button,
  Card,
  Collapse,
  IconButton,
  Link as MuiLink,
  MenuItem,
  Skeleton,
  Snackbar,
  Stack,
  TextField,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import {
  ChevronDown,
  ChevronRight,
  Compass,
  ExternalLink,
  FlaskConical,
  GitHub,
  MessageSquare,
  RotateCcw,
} from "@wso2/oxygen-ui-icons-react";
import { createLink, Link } from "@tanstack/react-router";
import { ApiRequestError } from "../../../api/errors";
import { EmptyState } from "../../../components/EmptyState";
import { LogLine, LogNote, LogSurface } from "../../../components/LogSection";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { runStamp } from "../../builds/lib/format";
import { secondsDuration } from "../../builds/lib/ledger";
import { useProjectStatus } from "../../projects/api/queries";
import {
  DEFAULT_LOG_WINDOW_SECONDS,
  useProjectDeployment,
  useRuntimeLogs,
} from "../api/queries";
import {
  componentAction,
  componentChip,
  componentTally,
  deploymentChip,
} from "../lib/status";

type DeploymentComponent = components["schemas"]["DeploymentComponent"];

const LinkButton = createLink(Button);
const RouterLink = createLink(MuiLink);

const WINDOWS = [
  { value: 900, label: "Last 15m" },
  { value: DEFAULT_LOG_WINDOW_SECONDS, label: "Last 1h" },
  { value: 21600, label: "Last 6h" },
  { value: 86400, label: "Last 24h" },
];

const ACTION_ICON = {
  flask: FlaskConical,
  external: ExternalLink,
  chat: MessageSquare,
} as const;

/**
 * One deployment (ADR-0020 §6).
 *
 * Before this, no deployment could be opened: the board showed what was running
 * now and nothing else was addressable. This page is what a table row points at.
 */
export function DeploymentDetailPage({
  projectName,
  deploymentId,
}: {
  projectName: string;
  deploymentId: string;
}) {
  const detail = useProjectDeployment(projectName, deploymentId);
  const [redeployNotice, setRedeployNotice] = useState(false);
  const status = useProjectStatus(projectName);
  const repoUrl = status.data?.repoUrl?.replace(/\/+$/, "").replace(/\.git$/, "");

  const backTo = {
    link: <Link to="/projects/$projectName/deployments" params={{ projectName }} />,
    label: "Back to Deployments",
  };

  if (detail.isPending) {
    return (
      <>
        <PageHeader title="Deployment" backTo={backTo} />
        <Skeleton variant="rounded" height={320} />
      </>
    );
  }

  if (detail.isError) {
    // A 404 here is a dead link, not a broken page — say which of the two it is
    // rather than showing the same red card for both. Branching on the
    // envelope's machine-readable `code`, never on the message: the BFF owns
    // that sentence and may reword it, and a message match would silently fall
    // back to the red card the day it does.
    const missing =
      detail.error instanceof ApiRequestError && detail.error.code === "not_found";
    return (
      <>
        <PageHeader title="Deployment" backTo={backTo} />
        {missing ? (
          <EmptyState
            icon={<Compass size={48} />}
            title="No such deployment"
            description="This deployment no longer exists, or the link points at another project."
            action={
              <LinkButton
                variant="contained"
                to="/projects/$projectName/deployments"
                params={{ projectName }}
              >
                Back to Deployments
              </LinkButton>
            }
          />
        ) : (
          <Alert
            severity="error"
            action={<Button onClick={() => void detail.refetch()}>Retry</Button>}
          >
            Failed to load the deployment
            {detail.error instanceof Error && detail.error.message
              ? `: ${detail.error.message}`
              : ""}
          </Alert>
        )}
      </>
    );
  }

  const { deployment, components: parts } = detail.data;
  const chip = deploymentChip(deployment);
  const tally = componentTally(parts ?? undefined);
  const environment =
    deployment.environment.charAt(0).toUpperCase() + deployment.environment.slice(1);

  const facts: Array<{ label: string; value: React.ReactNode }> = [
    { label: "Deployed", value: runStamp(deployment.deployedAt) },
    {
      label: "Duration",
      value: (
        <Box component="span" sx={{ fontVariantNumeric: "tabular-nums" }}>
          {secondsDuration(deployment.durationSeconds) || "—"}
        </Box>
      ),
    },
    { label: "Milestone", value: deployment.milestoneTitle || "—" },
    {
      label: "Commit",
      value: deployment.commit?.sha ? (
        <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
          <MuiLink
            href={repoUrl ? `${repoUrl}/commit/${deployment.commit.sha}` : undefined}
            target="_blank"
            rel="noreferrer"
            sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}
          >
            {deployment.commit.sha.slice(0, 7)}
          </MuiLink>
          {deployment.commit.branch && (
            <Typography variant="caption" color="text.secondary">
              {deployment.commit.branch}
            </Typography>
          )}
          {repoUrl && (
            <MuiLink
              href={`${repoUrl}/commit/${deployment.commit.sha}`}
              target="_blank"
              rel="noreferrer"
              sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, fontSize: "0.75rem" }}
            >
              <GitHub size={13} /> GitHub
            </MuiLink>
          )}
        </Stack>
      ) : (
        "—"
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title={`${environment} · ${deployment.tag}`}
        backTo={backTo}
        actions={
          <Button
            variant="outlined"
            startIcon={<RotateCcw size={15} />}
            sx={{ borderRadius: 999 }}
            onClick={() => setRedeployNotice(true)}
          >
            Redeploy
          </Button>
        }
      />

      <Card variant="outlined">
        <Stack
          direction="row"
          spacing={1.5}
          sx={{ alignItems: "center", px: 2.25, py: 1.75, borderBottom: 1, borderColor: "divider" }}
        >
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
            {environment} · {deployment.tag}
          </Typography>
          <StatusChip label={chip.label} tone={chip.tone} appearance="soft" dot />
          {tally && (
            <Typography variant="caption" color="text.secondary">
              {tally.ready} of {tally.total} components live
            </Typography>
          )}
          <Box sx={{ flex: 1 }} />
          <RouterLink
            to="/projects/$projectName/builds/$tag"
            params={{ projectName, tag: deployment.tag }}
            underline="hover"
            sx={{ fontSize: "0.8125rem" }}
          >
            View the build that shipped this
          </RouterLink>
        </Stack>

        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4, minmax(0, 1fr))" },
            borderBottom: 1,
            borderColor: "divider",
          }}
        >
          {facts.map((f) => (
            <Box
              key={f.label}
              sx={{
                px: 2.25,
                py: 1.625,
                minWidth: 0,
                borderRight: 1,
                borderColor: "divider",
                "&:last-of-type": { borderRight: 0 },
              }}
            >
              <Typography
                variant="overline"
                color="text.secondary"
                sx={{ fontWeight: 700, letterSpacing: "0.07em" }}
              >
                {f.label}
              </Typography>
              <Typography
                variant="body2"
                component="div"
                sx={{ mt: 0.625, overflow: "hidden", textOverflow: "ellipsis" }}
              >
                {f.value}
              </Typography>
            </Box>
          ))}
        </Box>

        <Box sx={{ p: 2.25 }}>
          {(parts ?? []).length === 0 ? (
            // A failed deployment put nothing in the environment. Saying so is
            // the answer; an empty list with no explanation is not.
            <EmptyState
              compact
              bordered
              description={
                deployment.status === "failed"
                  ? "This deployment failed before any component reached the environment."
                  : "No components are recorded for this deployment yet."
              }
            />
          ) : (
            <Stack spacing={1.25}>
              {(parts ?? []).map((component) => (
                <ComponentRow
                  key={component.name}
                  projectName={projectName}
                  environment={deployment.environment}
                  component={component}
                />
              ))}
            </Stack>
          )}
        </Box>
      </Card>

      {/* Drawn in the design, but there is no redeploy endpoint. Refetching
          under this label would look like it worked and change nothing. */}
      <Snackbar
        open={redeployNotice}
        autoHideDuration={6000}
        onClose={() => setRedeployNotice(false)}
      >
        <Alert severity="info" onClose={() => setRedeployNotice(false)}>
          Redeploy isn&apos;t wired to the platform yet — a version redeploys
          today by merging new work into it.
        </Alert>
      </Snackbar>
    </>
  );
}

function ComponentRow({
  projectName,
  environment,
  component,
}: {
  projectName: string;
  environment: string;
  component: DeploymentComponent;
}) {
  const [open, setOpen] = useState(false);
  const [windowSeconds, setWindowSeconds] = useState(DEFAULT_LOG_WINDOW_SECONDS);
  const chip = componentChip(component);
  const action = componentAction(component);
  const ActionIcon = action.icon ? ACTION_ICON[action.icon] : null;

  // Only an OPEN row reads its log — a deployment with six components must not
  // fire six log requests on mount.
  const logs = useRuntimeLogs(
    projectName,
    component.name,
    environment,
    windowSeconds,
    open,
  );

  return (
    <Card
      variant="outlined"
      sx={{
        overflow: "hidden",
        ...(open && { borderColor: "info.main" }),
      }}
    >
      <Stack direction="row" spacing={1.75} sx={{ alignItems: "center", px: 1.75, py: 1.375 }}>
        <Avatar sx={{ width: 24, height: 24, fontSize: "0.6875rem", fontWeight: 600 }}>
          {component.name.charAt(0).toUpperCase()}
        </Avatar>
        <Typography variant="body2" sx={{ fontWeight: 500, width: 150, flexShrink: 0 }}>
          {component.name}
        </Typography>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{
            flex: 1,
            minWidth: 0,
            fontFamily: "monospace",
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
        >
          {component.releaseName}
        </Typography>
        <StatusChip label={chip.label} tone={chip.tone} appearance="soft" dot />
        {action.label && ActionIcon && component.endpointUrl && (
          <Button
            size="small"
            variant="outlined"
            color="primary"
            href={component.endpointUrl}
            target="_blank"
            rel="noreferrer"
            startIcon={<ActionIcon size={13} />}
            sx={{ borderRadius: 999, height: 28, flexShrink: 0 }}
          >
            {action.label}
          </Button>
        )}
        <IconButton
          size="small"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          aria-label={open ? `Hide ${component.name} log` : `Show ${component.name} log`}
        >
          {open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
        </IconButton>
      </Stack>

      <Collapse in={open} unmountOnExit>
        {component.endpointUrl && (
          <Stack
            direction="row"
            spacing={1.25}
            sx={{ alignItems: "center", px: 1.75, py: 1.375, pl: 6.625, borderTop: 1, borderColor: "divider" }}
          >
            <Typography variant="caption" color="text.secondary" sx={{ width: 40 }}>
              URL
            </Typography>
            <MuiLink
              href={component.endpointUrl}
              target="_blank"
              rel="noreferrer"
              sx={{ fontFamily: "monospace", fontSize: "0.78125rem" }}
            >
              {component.endpointUrl}
            </MuiLink>
            <ExternalLink size={13} aria-hidden />
          </Stack>
        )}

        <Stack
          direction="row"
          spacing={1.25}
          sx={{
            alignItems: "center",
            px: 1.75,
            py: 1.25,
            borderTop: 1,
            borderColor: "divider",
            bgcolor: (t) => alpha(t.palette.text.primary, 0.03),
          }}
        >
          <Typography
            variant="caption"
            sx={{ fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase" }}
            color="text.secondary"
          >
            Runtime log
          </Typography>
          <Box sx={{ flex: 1 }} />
          <TextField
            select
            size="small"
            value={windowSeconds}
            onChange={(e) => setWindowSeconds(Number(e.target.value))}
            sx={{ width: 140 }}
            label="Window"
          >
            {WINDOWS.map((w) => (
              <MenuItem key={w.value} value={w.value}>
                {w.label}
              </MenuItem>
            ))}
          </TextField>
        </Stack>

        <RuntimeLog query={logs} componentName={component.name} />
      </Collapse>
    </Card>
  );
}

function RuntimeLog({
  query,
  componentName,
}: {
  query: ReturnType<typeof useRuntimeLogs>;
  componentName: string;
}) {
  if (query.isPending) {
    return (
      <LogSurface maxHeight={260}>
        <LogNote>Reading {componentName}&apos;s log…</LogNote>
      </LogSurface>
    );
  }

  if (query.isError) {
    // This endpoint lands with the BACKEND, not with this branch (#609's
    // handshake). Until it ships, every reader hits this path — so it degrades
    // to a note on the log surface rather than a red error card, which would
    // read as "your deployment is broken".
    return (
      <LogSurface maxHeight={260}>
        <LogNote>
          Runtime logs are not available yet for this component — the platform
          endpoint that serves them is still being rolled out.
        </LogNote>
      </LogSurface>
    );
  }

  const entries = query.data?.entries ?? [];
  if (entries.length === 0) {
    return (
      <LogSurface maxHeight={260}>
        <LogNote>Nothing logged in this window. Try a longer one.</LogNote>
      </LogSurface>
    );
  }

  return (
    <LogSurface maxHeight={260}>
      {query.data?.truncated && (
        <LogNote>…older lines in this window were not returned</LogNote>
      )}
      {entries.map((entry, i) => (
        <LogLine
          key={`${entry.timestamp ?? i}-${i}`}
          timestamp={
            entry.timestamp
              ? new Date(entry.timestamp).toLocaleTimeString(undefined, {
                  hour12: false,
                })
              : undefined
          }
          tone={
            entry.level === "error"
              ? "warning"
              : entry.level === "warn"
                ? "warning"
                : "default"
          }
        >
          {entry.message}
        </LogLine>
      ))}
    </LogSurface>
  );
}

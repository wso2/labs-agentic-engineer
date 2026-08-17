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
  alpha,
  Avatar,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Divider,
  LinearProgress,
  Link as MuiLink,
  Snackbar,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ArrowRight, ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { createLink, Link } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip, type StatusTone } from "../../../components/StatusChip";
import { StageRow } from "../../builds/components/StageRow";
import { useDesignDependencies } from "../../spec/api/queries";
import { useValidationEvidence } from "../../validation/api/counts";
import {
  verdictSentence,
  type ValidationCounts,
} from "../../validation/lib/verdict";
import {
  useComponentsDeployments,
  useProjectComponents,
  useProjectStatus,
} from "../api/queries";
import {
  groupDeploymentCards,
  type DeploymentCard,
} from "../lib/deploymentRows";
import {
  developmentStage,
  productionStage,
  validationStage,
} from "../lib/deploymentStory";
import { projectChip } from "../lib/projectChip";
import { validationView } from "../lib/pipeline";
import {
  canPromote,
  configuredCount,
  connectionRows,
  seedValues,
  type ConnectionRow,
  type ConnectionValues,
} from "../lib/promotion";
import { ConnectionValuesDialog } from "./ConnectionValuesDialog";
import { PromoteDialog } from "./PromoteDialog";

const LinkButton = createLink(Button);
const RouterLink = createLink(MuiLink);

// Deployments as ONE STORY with a status panel beside it (Deployments UX,
// Turn 3: option 1c + the 2c promote dialog). The two-column board this
// replaces treated Development and Production as parallel places to diff;
// they are consecutive stages of one path, so the page now reads top to
// bottom on the same numbered rail the Builds page uses — what is running,
// what the validation agent concluded about it, and the promotion that
// path leads to. The side panel is the at-a-glance answer (version, rollout,
// endpoints, production readiness) for the reader who isn't following the
// story. Data is unchanged: the components list, one list-deployments read
// per component, the status poll's deploy aggregate — plus the Spec view's
// design-dependencies read for the connections a promotion must configure.

// Chip vocabulary for a card's state (#216): the label keeps the backend's
// raw condition reason (it's the vocabulary operators see in OpenChoreo),
// only the two join-derived states get console-authored labels.
function cardChip(card: DeploymentCard): {
  label: string;
  tone: StatusTone;
  outlined?: boolean;
} {
  switch (card.kind) {
    case "notDeployed":
      return { label: "Not deployed", tone: "neutral", outlined: true };
    case "undeployed":
      return { label: "Undeployed", tone: "neutral" };
    case "success":
      return { label: card.deployment?.status ?? "Ready", tone: "success" };
    case "error":
      return { label: card.deployment?.status ?? "Failed", tone: "error" };
    case "transitional":
      return { label: card.deployment?.status ?? "In progress", tone: "info" };
    default:
      return { label: "Pending", tone: "neutral", outlined: true };
  }
}

function formatWhen(iso: string): string | null {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? null : date.toLocaleString();
}

/** One component under a stage: identity, its release, its state, its way in.
 *  Deliberately UNIFORM — every row carries the same controls (#401 review:
 *  no per-row optional actions; connection configuration lives on the side
 *  panel's Connections section). */
function ComponentRow({ card }: { card: DeploymentCard }) {
  const chip = cardChip(card);
  const d = card.deployment;
  return (
    <Stack
      direction="row"
      spacing={1.5}
      sx={{
        alignItems: "center",
        // Outlined only, like every Builds surface — the row's border does
        // the separating, no fill (#401 review: no solid backgrounds).
        border: 1,
        borderColor: "divider",
        borderRadius: 2,
        px: 1.75,
        py: 1.25,
        ...(card.kind === "notDeployed" && { opacity: 0.6, borderStyle: "dashed" }),
      }}
    >
      <Avatar
        sx={{
          width: 24,
          height: 24,
          bgcolor: "action.hover",
          color: "text.primary",
          fontSize: 12,
        }}
      >
        {(card.displayName.trim()[0] ?? "C").toUpperCase()}
      </Avatar>
      <Typography variant="subtitle2" sx={{ flexShrink: 0 }}>
        {card.displayName}
      </Typography>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{
          fontFamily: "monospace",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
          flexGrow: 1,
          minWidth: 0,
        }}
      >
        {d?.releaseName ?? ""}
      </Typography>
      <StatusChip
        label={chip.label}
        tone={chip.tone}
        {...(chip.outlined && { variant: "outlined" as const })}
      />
      {d?.endpointUrl && (
        <MuiLink
          href={d.endpointUrl}
          target="_blank"
          rel="noreferrer"
          variant="body2"
          // Every row's link reads "Open" — the accessible name carries the
          // component so a screen reader hears which app it opens (#401 review).
          aria-label={`Open ${card.displayName}`}
          sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, flexShrink: 0 }}
        >
          Open <ExternalLink size={14} />
        </MuiLink>
      )}
    </Stack>
  );
}

/**
 * The validation stage's own evidence: what the last attempt found, and the way to
 * the report.
 *
 * EVERY state takes its sentence from the SHARED copy the Validation page's tile
 * reads, word for word. Writing any of them here is what let the two come apart:
 * this banner announced a lifecycle state as a verdict ("This deployment's verdict:
 * awaiting fix.") and, on a settled failure, led with the count that PASSED — so a
 * reader moving between the two surfaces met a different voice and a different
 * headline number for one outcome.
 */
function VerdictBanner({
  projectName,
  validation,
  verdict,
  repairing,
  counts,
}: {
  projectName: string;
  /** deploy.validation — the loop's position. */
  validation: string;
  /** The run's stored verdict, which `awaiting-fix` folds away. */
  verdict: string;
  /** The attempt in flight repairs that verdict rather than re-asking it. */
  repairing: boolean;
  counts?: ValidationCounts;
}) {
  const view = validationView(validation);
  if (!view) return null;
  // WHICH verdict the sentence is about differs by state, and only that. A settled
  // deploy.validation IS the verdict, mirrored — which is also the only place the
  // three states outside COUNTABLE (`inconclusive`, `unreported`, `skipped`) can be
  // read from here, since their run row is never fetched. The two lifecycle values
  // are the ones that fold a verdict away, so those take the run's.
  const inFlight = validation === "running" || validation === "awaiting-fix";
  const sentence =
    verdictSentence(inFlight ? verdict : validation, counts, validation, repairing) ||
    // `skipped`, and any value from a newer server. The shared copy deliberately has
    // no sentence for skipped: the stage note beside this already says the version
    // authored no criteria, and two adjacent elements saying it once each is a
    // restatement. Naming the verdict complements that instead.
    `This deployment's verdict: ${view.label}.`;
  return (
    <Box
      sx={(theme) => {
        const main =
          view.tone === "ghost" || view.tone === "neutral"
            ? theme.palette.text.secondary
            : theme.palette[view.tone].main;
        return {
          border: `1px solid ${alpha(main, 0.35)}`,
          bgcolor: alpha(main, 0.06),
          borderRadius: 2,
          px: 1.75,
          py: 1.25,
          display: "flex",
          alignItems: "center",
          gap: 1.25,
        };
      }}
    >
      <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
        {sentence}
      </Typography>
      <LinkButton
        to="/projects/$projectName/validation"
        params={{ projectName }}
        size="small"
        color="inherit"
        endIcon={<ArrowRight size={14} aria-hidden />}
        // Text, not outlined — it sits inside the banner's own border. Which
        // costs it MUI's 5px text padding, half what the outlined navigation
        // buttons get, so px is said explicitly.
        sx={{ flexShrink: 0, fontWeight: 500, px: 1.25 }}
      >
        View full report
      </LinkButton>
    </Box>
  );
}

/** A side-panel section heading — MilestonePanel's exact eyebrow recipe
 *  (caption, 700, 0.08em tracking), so the two panels' type matches. */
function PanelOverline({
  children,
  color = "text.secondary",
}: {
  children: React.ReactNode;
  color?: string;
}) {
  return (
    <Typography
      variant="caption"
      sx={{
        color,
        fontWeight: 700,
        letterSpacing: "0.08em",
        textTransform: "uppercase",
        display: "block",
      }}
    >
      {children}
    </Typography>
  );
}

/** A side-panel status line: a dot, a name, a fact or link on the right. */
function PanelRow({
  dotColor,
  muted = false,
  label,
  trailing,
}: {
  dotColor: string;
  muted?: boolean;
  label: string;
  trailing?: React.ReactNode;
}) {
  return (
    <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
      <Box
        sx={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          bgcolor: dotColor,
          flexShrink: 0,
        }}
      />
      <Typography
        variant="body2"
        color={muted ? "text.secondary" : "text.primary"}
        sx={{ flexGrow: 1, minWidth: 0 }}
      >
        {label}
      </Typography>
      {trailing}
    </Stack>
  );
}

export function DeploymentsPage({ projectName }: { projectName: string }) {
  const components = useProjectComponents(projectName);
  const componentNames = (components.data?.items ?? []).map((c) => c.name);
  const deployments = useComponentsDeployments(projectName, componentNames);
  // The status poll's deploy aggregate (#184) carries the spec tag live in
  // dev ("v1") and the validation verdict; the layout already runs this query.
  const status = useProjectStatus(projectName);
  const deploy = status.data?.deploy;
  // The design's connections, for promotion readiness. Degrades to [] on
  // error (the hook's own contract), so a failed read renders the story
  // without a live-configuration line rather than blocking the page.
  const dependencies = useDesignDependencies(projectName);
  const connections = useMemo(
    () => connectionRows(dependencies.data),
    [dependencies.data],
  );
  // The rail's Validation stage (#395, decision 3): the Validation page's own
  // criteria/report join, keyed on the BUILD version (the newest run — what
  // deploy.validation describes). The VERDICT comes back with the counts because
  // `awaiting-fix` folds `failed` and `unreported` into one word and the banner's
  // sentence differs for each; counts are undefined in every failure mode, and every
  // sentence has a count-free form.
  const {
    verdict: runVerdict,
    repairing: runRepairing,
    counts,
  } = useValidationEvidence(
    projectName,
    status.data?.build.version ?? "",
    deploy?.validation ?? "",
  );

  // Production values entered through the promote dialog. Client state only:
  // the contract has no promote surface yet, so these live exactly as long as
  // the page does — seeded from the config keys' defaults.
  const [values, setValues] = useState<ConnectionValues | null>(null);
  const liveValues = values ?? seedValues(connections);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [promoteNotice, setPromoteNotice] = useState(false);
  // The connection whose dev values are being re-collected (#395: dummy
  // values at build time, real ones now), and the saved confirmation.
  const [valuesTarget, setValuesTarget] = useState<ConnectionRow | null>(null);
  const [valuesSaved, setValuesSaved] = useState(false);

  const header = (
    <PageHeader
      title="Deployments"
      {...(status.data && { status: projectChip(status.data) })}
      backTo={{
        link: <Link to="/projects/$projectName" params={{ projectName }} />,
        label: "Back to Overview",
      }}
    />
  );

  if (components.isPending || (componentNames.length > 0 && deployments.isPending)) {
    return (
      <>
        {header}
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading deployments" />
        </Box>
      </>
    );
  }

  if (components.isError) {
    return (
      <>
        {header}
        <Alert
          severity="error"
          action={
            <Button onClick={() => void components.refetch()}>Retry</Button>
          }
        >
          Failed to load deployments
          {components.error instanceof Error && components.error.message
            ? `: ${components.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  if (componentNames.length === 0) {
    return (
      <>
        {header}
        <EmptyState
          compact
          description="Nothing to deploy yet — components appear here once the published design produces them, and agents deploy to dev on merge."
        />
      </>
    );
  }

  const board = groupDeploymentCards(
    components.data?.items ?? [],
    deployments.deployments,
  );
  const devStage = deploy && developmentStage(board.development, deploy);
  const valStage = deploy && validationStage(deploy.validation, counts);
  const prodStage =
    deploy &&
    productionStage(
      board.production,
      deploy,
      dependencies.isPending ? null : connections.length,
    );
  const devDeployed = board.development.filter((c) => c.deployment);
  // Rollout numbers come from the cards (the truth the page renders), not
  // the status aggregate's components tally — see developmentStage.
  const devReady = board.development.filter((c) => c.kind === "success").length;
  const devTotal = board.development.length;
  const updatedAt = deployments.deployments
    .map((d) => d.createdAt ?? "")
    .filter(Boolean)
    .sort()
    .at(-1);
  const updated = updatedAt ? formatWhen(updatedAt) : null;
  const configured = configuredCount(connections, liveValues);
  const promotable = Boolean(
    deploy && deploy.version && board.production.length === 0,
  );

  // The card's header IS the deploy lifecycle chip (review on #401): the
  // status word does the work a "What is running" heading duplicated. Every
  // state renders one, so the header never stands empty.
  const liveChip =
    deploy?.status === "deployed"
      ? { label: "Deployed", tone: "success" as const }
      : deploy?.status === "deploying"
        ? { label: "Deploying", tone: "info" as const }
        : deploy?.status === "failed"
          ? { label: "Deploy failed", tone: "error" as const }
          : // The status poll gates nothing above, so the header must not
            // claim "nothing deployed" before the poll has answered (#401
            // review) — an unknown state says it is unknown.
            !deploy && status.isPending
            ? { label: "Loading status", tone: "neutral" as const }
            : { label: "Nothing deployed", tone: "neutral" as const };

  return (
    <>
      {header}
      {deployments.failedCount > 0 && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          Deployments for {deployments.failedCount} component
          {deployments.failedCount === 1 ? "" : "s"} could not be loaded — the
          page shows what did.
        </Alert>
      )}
      <Box
        sx={{
          // The Builds page's exact grid (BuildsPage.tsx), so the two pages'
          // main columns and side panels line up when switching between them.
          display: "grid",
          gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1fr) 320px" },
          gap: 2,
          alignItems: "start",
        }}
      >
        {/* ——— The story: what is running, one numbered rail. Same surface
            vocabulary as the Builds page's RunStory card: an outlined Card
            with a near-transparent tint (never a solid fill), state carried
            in the EDGE only when something is moving or broke. ——— */}
        <Card
          variant="outlined"
          sx={{
            bgcolor: (t) => alpha(t.palette.text.primary, 0.02),
            ...((liveChip.tone === "info" || liveChip.tone === "error") && {
              borderColor: (t) => alpha(t.palette[liveChip.tone].main, 0.35),
            }),
          }}
        >
          <CardContent sx={{ "&:last-child": { pb: 2.5 } }}>
          <Stack
            direction="row"
            spacing={1.5}
            sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 0.5 }}
          >
            <StatusChip
              label={liveChip.label}
              tone={liveChip.tone}
              appearance="soft"
              dot
            />
            {updated && (
              // body2 like RunStory's header line, not caption.
              <Typography variant="body2" color="text.secondary">
                Updated {updated}
              </Typography>
            )}
          </Stack>
          <Divider sx={{ my: 2 }} />
          {devStage && (
            <StageRow stage={devStage} step={1}>
              {devDeployed.length > 0 && (
                <Stack spacing={1}>
                  {board.development.map((card) => (
                    <ComponentRow
                      key={`${card.componentName}/${card.deployment?.environment ?? ""}`}
                      card={card}
                    />
                  ))}
                </Stack>
              )}
            </StageRow>
          )}
          {valStage && deploy && (
            <StageRow stage={valStage} step={2}>
              <VerdictBanner
                projectName={projectName}
                validation={deploy.validation}
                verdict={runVerdict}
                repairing={runRepairing}
                {...(counts && { counts })}
              />
            </StageRow>
          )}
          {prodStage && deploy && (
            <StageRow stage={prodStage} step={3} last>
              {board.production.length > 0 ? (
                <Stack spacing={1}>
                  {board.production.map((card) => (
                    <ComponentRow
                      key={`${card.componentName}/${card.deployment?.environment ?? ""}`}
                      card={card}
                    />
                  ))}
                </Stack>
              ) : promotable ? (
                <Stack
                  direction="row"
                  spacing={1.5}
                  sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 1 }}
                >
                  <span
                    {...(!canPromote(deploy) && {
                      title:
                        "Enabled once the dev deployment settles and validation has its say",
                    })}
                  >
                    <Button
                      variant="contained"
                      disabled={!canPromote(deploy)}
                      onClick={() => setPromoteOpen(true)}
                      endIcon={<ArrowRight size={16} aria-hidden />}
                    >
                      Promote {deploy.version} to production
                    </Button>
                  </span>
                  <Typography variant="caption" color="text.secondary">
                    Opens a dialog to collect live configuration.
                  </Typography>
                </Stack>
              ) : null}
            </StageRow>
          )}
          </CardContent>
        </Card>

        {/* ——— The side panel: the same facts at a glance — MilestonePanel's
            plain outlined Card, no fill of its own. ——— */}
        <Card variant="outlined">
          <CardContent>
          {/* Green only when everything is live and ready — MilestonePanel's
              delivered rule, not a standing accent. */}
          <PanelOverline
            color={
              deploy?.status === "deployed" && devTotal > 0 && devReady === devTotal
                ? "success.main"
                : "text.secondary"
            }
          >
            Environment · Dev
          </PanelOverline>
          {/* subtitle1/600 like MilestonePanel's tag title — the panels sit in
              the same grid slot and their headline type must agree. */}
          <Typography variant="subtitle1" sx={{ fontWeight: 600, mt: 0.5 }}>
            {deploy?.version || "—"}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {devDeployed.length} component{devDeployed.length === 1 ? "" : "s"} live
          </Typography>
          {/* Card-derived counts, like the rail's note — the status
              aggregate's tally has disagreed with the bindings on screen
              ("2 of 0", #401 feedback), and the panel must agree with the
              rows beside it. */}
          {devTotal > 0 && (
            <Stack
              direction="row"
              spacing={1.5}
              sx={{ alignItems: "center", mt: 1.5 }}
            >
              <LinearProgress
                variant="determinate"
                value={(devReady / devTotal) * 100}
                aria-label={`${devReady} of ${devTotal} components ready`}
                sx={{
                  flexGrow: 1,
                  height: 6,
                  borderRadius: 3,
                  // Neutral track, coloured fill only — MilestonePanel's bar.
                  bgcolor: "action.selected",
                  "& .MuiLinearProgress-bar": {
                    bgcolor: devReady === devTotal ? "success.main" : "info.main",
                    borderRadius: 3,
                  },
                }}
              />
              <Typography
                variant="caption"
                sx={{
                  color: devReady === devTotal ? "success.main" : "text.secondary",
                  fontVariantNumeric: "tabular-nums",
                  whiteSpace: "nowrap",
                }}
              >
                {devReady} / {devTotal} ready
              </Typography>
            </Stack>
          )}
          {/* No validation pill here. It said "Awaiting fix" — a label with no
              subject — under a components-ready bar in a card about the DEV
              ENVIRONMENT, so it read as the deployment awaiting a fix. Validation is
              the stage AFTER this one, which the rail already numbers as step 2 and
              names, with the actor, the counts, a sentence and its own link to the
              report. This was a strictly weaker duplicate of that row. */}
          <Divider sx={{ my: 2 }} />
          {/* The design's connections, and the way to hand the platform their
              REAL values after build-time placeholders (#395 follow-up):
              Update values re-collects an external connection's dev values
              through the same provisioning surface the build drawer used.
              Component Open links live on the rail rows — this section owns
              what the rail doesn't say. */}
          <PanelOverline>Connections</PanelOverline>
          <Stack spacing={1.25} sx={{ mt: 1 }}>
            {connections.length > 0 ? (
              connections.map((row) => (
                <PanelRow
                  key={row.id}
                  dotColor="success.main"
                  label={row.detail ? `${row.name} (${row.detail})` : row.name}
                  trailing={
                    row.kind === "external" && row.config.length > 0 ? (
                      // The console's tinted-pill recipe, in the app's accent —
                      // an ACTION among readouts must out-rank its neighbours'
                      // quiet captions, and this is the one shape for "a pill you
                      // can press".
                      <Button
                        size="small"
                        color="inherit"
                        disableElevation
                        // The row label holds the connection name; the button's
                        // accessible name must too, or every row reads
                        // "Configure" to a screen reader (#401 review).
                        aria-label={`Configure ${row.name}`}
                        onClick={() => setValuesTarget(row)}
                        sx={(theme) => ({
                          borderRadius: 999,
                          minWidth: 0,
                          px: 1.25,
                          py: 0.25,
                          flexShrink: 0,
                          fontWeight: 600,
                          fontSize: theme.typography.body2.fontSize,
                          lineHeight: 1.6,
                          color: "primary.main",
                          border: `1px solid ${alpha(theme.palette.primary.main, 0.3)}`,
                          bgcolor: alpha(theme.palette.primary.main, 0.14),
                          transition: "background-color 120ms, border-color 120ms",
                          "&:hover, &.Mui-focusVisible": {
                            bgcolor: alpha(theme.palette.primary.main, 0.24),
                            borderColor: alpha(theme.palette.primary.main, 0.5),
                          },
                          "&.Mui-focusVisible": {
                            outline: `2px solid ${alpha(theme.palette.primary.main, 0.6)}`,
                            outlineOffset: 2,
                          },
                        })}
                      >
                        Configure
                      </Button>
                    ) : row.provisioned ? (
                      <Typography variant="caption" color="success.main">
                        provisioned
                      </Typography>
                    ) : (
                      // Config-carrying but not user-updatable here (a platform
                      // resource like an identity app): the platform owns its
                      // credentials, so say that instead of inferring
                      // "provisioned" from the else-branch (#401 review).
                      <Typography variant="caption" color="text.secondary">
                        platform-managed
                      </Typography>
                    )
                  }
                />
              ))
            ) : (
              <Typography variant="body2" color="text.secondary">
                This design declares no connections.
              </Typography>
            )}
          </Stack>
          <Divider sx={{ my: 2 }} />
          <PanelOverline>Production</PanelOverline>
          <Stack spacing={1.25} sx={{ mt: 1 }}>
            {connections.length > 0 && (
              <PanelRow
                dotColor={
                  configured === connections.length
                    ? "success.main"
                    : "warning.main"
                }
                label="Live configuration"
                trailing={
                  <Typography
                    variant="body2"
                    color={
                      configured === connections.length
                        ? "success.main"
                        : "warning.main"
                    }
                  >
                    {configured} / {connections.length} set
                  </Typography>
                }
              />
            )}
            {board.production.length > 0 ? (
              <PanelRow
                dotColor="success.main"
                label={`${board.production.length} component${board.production.length === 1 ? "" : "s"} live`}
              />
            ) : (
              <PanelRow
                dotColor="text.disabled"
                muted
                label="Nothing deployed yet"
              />
            )}
          </Stack>
          <Divider sx={{ my: 2 }} />
          <RouterLink
            to="/projects/$projectName/builds"
            params={{ projectName }}
            variant="body2"
            sx={{ fontWeight: 500 }}
          >
            View the build that shipped this →
          </RouterLink>
          </CardContent>
        </Card>
      </Box>

      {valuesTarget && (
        <ConnectionValuesDialog
          open
          onClose={() => setValuesTarget(null)}
          onSaved={() => {
            setValuesTarget(null);
            setValuesSaved(true);
          }}
          projectName={projectName}
          connection={valuesTarget}
          environment="development"
        />
      )}
      {deploy && (
        <PromoteDialog
          open={promoteOpen}
          onClose={() => setPromoteOpen(false)}
          projectName={projectName}
          version={deploy.version}
          validation={deploy.validation}
          rows={connections}
          values={liveValues}
          onValueChange={(rowId, key, value) =>
            setValues({
              ...liveValues,
              [rowId]: { ...liveValues[rowId], [key]: value },
            })
          }
          onPromote={() => {
            setPromoteOpen(false);
            setPromoteNotice(true);
          }}
        />
      )}
      {/* Promotion has no platform surface yet (no promote endpoint in the
          contract) — an enabled Promote is honest about that instead of
          pretending a deploy happened. */}
      <Snackbar
        open={promoteNotice}
        autoHideDuration={6000}
        onClose={() => setPromoteNotice(false)}
      >
        <Alert severity="info" onClose={() => setPromoteNotice(false)}>
          Production promotion isn't wired to the platform yet — your live
          configuration is kept for this session.
        </Alert>
      </Snackbar>
      <Snackbar
        open={valuesSaved}
        autoHideDuration={6000}
        onClose={() => setValuesSaved(false)}
      >
        <Alert severity="success" onClose={() => setValuesSaved(false)}>
          Values saved — the connection re-provisions with them.
        </Alert>
      </Snackbar>
    </>
  );
}

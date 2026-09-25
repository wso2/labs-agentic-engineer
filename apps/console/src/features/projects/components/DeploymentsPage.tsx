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
import { Alert, Button, Snackbar } from "@wso2/oxygen-ui";
import { Link, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { useBuildRuns, useBuilds } from "../../builds/api/queries";
import { runStamp } from "../../builds/lib/format";
import { mergedCycle } from "../../builds/lib/runView";
import { useDesignDependencies } from "../../spec/api/queries";
import { useExternalResources } from "../../settings/api/queries";
import { isRegisteredExternal } from "../../marketplace/kind";
import { useValidationEvidence } from "../../validation/api/counts";
import {
  useComponentsDeployments,
  useEnvironments,
  useProjectComponents,
  useProjectDependencyReadiness,
  useProjectStatus,
} from "../api/queries";
import {
  deployHold,
  deployedBehindBuild,
  deployedValidationState,
  promoteUnavailable,
  developmentConnections,
  promoteStep,
} from "../lib/deploymentFlow";
import {
  buildFor,
  commitUrl,
  environmentRows,
  milestoneFor,
  milestoneUrl,
} from "../lib/deploymentLedger";
import { groupDeploymentCards } from "../lib/deploymentRows";
import {
  connectionRows,
  seedValues,
  type ConnectionRow,
  type ConnectionValues,
} from "../lib/promotion";
import { ConnectionValuesDialog } from "./ConnectionValuesDialog";
import { EnvironmentFlow, EnvironmentFlowSkeleton } from "./EnvironmentFlow";
import { PromoteDialog } from "./PromoteDialog";

/**
 * Deployments as the PIPELINE it is: one full-detail card per environment the
 * platform names, left to right in promotion order (ADR-0027/0032), each card
 * its own flow — deployed → validated → promoted — and each card opening that
 * environment's own page. However many environments there are, one or six:
 * nothing on this page counts them, and no sentence here names an environment
 * the served list did not.
 *
 * The version ledger that used to sit under the board has left the page; past
 * deployments belong to the environment they happened in.
 *
 * Data is the board's, plus two reads the Builds page already makes: the
 * newest run's story (for a run parked at the deploy gate — the "on hold"
 * state) and the dependency readiness read (whether the platform holds a
 * value for each external in development). No new contract surface.
 */
export function DeploymentsPage({ projectName }: { projectName: string }) {
  const navigate = useNavigate();
  const components = useProjectComponents(projectName);
  const componentNames = (components.data?.items ?? []).map((c) => c.name);
  const deployments = useComponentsDeployments(projectName, componentNames);
  // The status poll's deploy aggregate (#184) carries the spec tag live in
  // dev ("v1") and the validation verdict; the layout already runs this query.
  const status = useProjectStatus(projectName);
  const deploy = status.data?.deploy;
  // The version ledger, for "Milestone #N" beside the running version. DB-only
  // and already cached by the Builds surfaces.
  const builds = useBuilds(projectName);
  // The newest run's story — the BUILD version is the newest run's tag. A run
  // parked at the deploy gate is the one read that says "on hold"; the
  // validation evidence hook makes the same read, so it is served from cache.
  const runs = useBuildRuns(projectName, status.data?.build.version || undefined);
  // The design's connections, for promotion readiness. A failed read surfaces
  // as `isError` at the hook; this page degrades it here — `connectionRows`
  // maps an absent payload to [], so the board renders without a
  // connections group rather than blocking the page.
  const dependencies = useDesignDependencies(projectName);
  const connections = useMemo(
    () => connectionRows(dependencies.data),
    [dependencies.data],
  );
  const connectionsKnown = !dependencies.isPending && !dependencies.isError;
  // The pipeline's environments, in promotion order — the board's own order.
  const environments = useEnvironments();
  const environmentList = environments.data ?? [];
  // Values are collected where they are first needed: the environment a build
  // lands in. An empty name keeps the read idle until the list arrives.
  const entryEnvironment = environmentList[0]?.name ?? "";
  // Whether the platform holds values for each external THERE — the deploy
  // gate's own read, so "Set" here means what the gate means.
  const readiness = useProjectDependencyReadiness(projectName, entryEnvironment);
  // Org catalog: Registered Externals (non-empty envCells) already hold
  // values on the org plane — Connections must not offer Configure / the
  // project values dialog for those names. While the catalog query is
  // pending or failed, registeredNames is empty, so hide Configure for
  // every external until the query has settled successfully.
  const externalCatalog = useExternalResources();
  const catalogUnknown = externalCatalog.isPending || externalCatalog.isError;
  const registeredNames = useMemo(() => {
    const names = new Set<string>();
    for (const resource of externalCatalog.data ?? []) {
      if (isRegisteredExternal(resource)) names.add(resource.name);
    }
    return names;
  }, [externalCatalog.data]);
  // The version the Development card is about: what runs there, or — while
  // nothing does yet — the version being built. The aggregate's validation
  // names the BUILD version, so a deployed version older than it answers for
  // its own (`deployedValidation`) — one more tag-scoped run read, served from
  // cache whenever the Builds page made it, and not made at all while the two
  // versions agree.
  const buildVersion = status.data?.build.version ?? "";
  const version = deploy?.version || buildVersion;
  const behind = deployedBehindBuild(deploy, buildVersion);
  const deployedRuns = useBuildRuns(projectName, behind ? deploy?.version : undefined);
  const deployedState = deployedValidationState(deploy, buildVersion, deployedRuns);
  // While the verdict is unknown the aggregate carries `none` — PENDING, which
  // withholds promotion and paints nothing on its own; the availability flags
  // below are what every surface reads first, so the word is never shown.
  const cardDeploy = deploy
    ? { ...deploy, validation: deployedState.validation ?? "none" }
    : undefined;
  // The Validation page's own criteria/report join, keyed on the card's
  // version. The VERDICT comes back with the counts because `awaiting-fix`
  // folds `failed` and `unreported` into one word and the banner's sentence
  // differs for each.
  const validation = useValidationEvidence(projectName, version, cardDeploy?.validation ?? "");

  // Production values entered through the promote dialog. Client state only:
  // the contract has no promote surface yet, so these live exactly as long as
  // the page does — seeded from the config keys' defaults.
  const [values, setValues] = useState<ConnectionValues | null>(null);
  const liveValues = values ?? seedValues(connections);
  const [promoteOpen, setPromoteOpen] = useState(false);
  // The connection a production Configure asked for — the dialog opens on it.
  const [promoteFocus, setPromoteFocus] = useState<string | null>(null);
  const [promoteNotice, setPromoteNotice] = useState(false);
  // The connection whose dev values are being re-collected (#395: dummy
  // values at build time, real ones now), and the saved confirmation.
  const [valuesTarget, setValuesTarget] = useState<ConnectionRow | null>(null);
  const [valuesSaved, setValuesSaved] = useState(false);

  const header = (
    <PageHeader
      title="Deployments"
      backTo={{
        link: <Link to="/projects/$projectName" params={{ projectName }} />,
        label: "Back to Overview",
      }}
    />
  );

  // The board is one row per environment the pipeline names, so it cannot be
  // drawn — or honestly called empty — until that list is in. Without this the
  // page would assert "Nothing deployed yet" over a deployed project for as
  // long as the environments read takes.
  if (
    components.isPending ||
    environments.isPending ||
    (componentNames.length > 0 && deployments.isPending)
  ) {
    return (
      <>
        {header}
        <EnvironmentFlowSkeleton />
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

  if (environments.isError) {
    return (
      <>
        {header}
        <Alert
          severity="error"
          action={<Button onClick={() => void environments.refetch()}>Retry</Button>}
        >
          The deployment pipeline could not be loaded
          {environments.error instanceof Error && environments.error.message
            ? `: ${environments.error.message}`
            : ""}
          {" — the flow has no environments to draw until they load."}
        </Alert>
      </>
    );
  }

  // Settled and empty: the environments read came back, and named nothing. The
  // flow is one card per environment, so it has no cards to draw — and it
  // cannot say so itself, because `rows.length === 0` is equally the shape of a
  // read still out or one that came back empty, which is why the flow answers
  // both with the same shimmer. The page holds the query and knows which: not
  // pending (checked above), not errored (checked above), and empty. So the
  // page says it, and the reader stops waiting for a board that is never
  // coming. Only the environments list is named here — nothing on this page is
  // in a position to say WHY the platform has none.
  if (environmentList.length === 0) {
    return (
      <>
        {header}
        <EmptyState
          compact
          description="This organization has no deployment environments yet. Components deploy into environments, so there is no pipeline to draw until the platform has one."
        />
      </>
    );
  }

  if (componentNames.length === 0) {
    return (
      <>
        {header}
        <EmptyState
          compact
          description="Nothing deployed yet. Your components run here once they are built — each environment shows what is live and where to reach it."
        />
      </>
    );
  }

  const board = groupDeploymentCards(
    components.data?.items ?? [],
    deployments.deployments,
    entryEnvironment,
  );
  const rows = environmentRows(board, environmentList, deploy);
  // The promotion TARGET, as the ENTRY environment's own `promotesTo` names
  // it — never the next row along. A single-environment pipeline names none,
  // and a step invented for it drew an empty title over an environment the
  // platform never named. The flow card derives its target the same way, so
  // the page and the card can never disagree about which one it is.
  const promotesTo = environmentList[0]?.promotesTo;
  const promoteTarget = promotesTo
    ? rows.find((r) => r.environment === promotesTo)
    : undefined;
  const componentTypes = new Map<string, string>();
  for (const c of components.data?.items ?? []) {
    if (c.type) componentTypes.set(c.name, c.type);
  }
  // A hold is a fact about the BUILD version, so it is the card's only while
  // that is the card's version: an older version serving under a newer parked
  // build stays "Deployed" here and the Builds page names the park.
  const parked = deployHold(runs.data?.runs, dependencies.data);
  const hold = parked && !behind ? parked : null;
  // An unknown verdict withholds promotion outright: `canPromote` would wave
  // an empty validation through.
  const promoteIfKnown = promoteTarget
    ? promoteStep(cardDeploy, promoteTarget, connections, liveValues, hold, version)
    : null;
  const promote =
    promoteIfKnown && deployedState.failed ? promoteUnavailable(version) : promoteIfKnown;
  // Pending and failed alike: neither supports a claim about the deploy
  // aggregate, and every step that reads it must withhold rather than guess.
  const statusUnsettled = status.isPending || status.isError;
  // What the card's VERSION block states, all off reads the page already
  // makes. The run story keyed on the card's own version — the newest run's
  // when the deployed version IS the build version, the tag-scoped read
  // otherwise — so the sha under "Version v1" can never be v2's merge.
  const versionRuns = behind ? deployedRuns : runs;
  const versionBuild = buildFor(version || undefined, builds.data);
  const merged = mergedCycle(versionRuns.data?.runs);
  const mergeSha = merged?.mergeSha ?? "";
  const commitHref = commitUrl(status.data?.repoUrl, mergeSha);
  // A run read still out is not "no commit": the block says nothing about it
  // until it settles, rather than printing a placeholder.
  const commit: { sha: string; href?: string } | "loading" | undefined = versionRuns.isPending
    ? "loading"
    : mergeSha
      ? { sha: mergeSha, ...(commitHref ? { href: commitHref } : {}) }
      : undefined;
  const builtAt = runStamp(versionBuild?.completedAt) || undefined;
  const milestoneHref = milestoneUrl(status.data?.repoUrl, versionBuild?.milestoneNumber);
  const devLines = connectionsKnown
    ? developmentConnections(connections, readiness.data, hold, registeredNames, catalogUnknown)
    : null;

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
      {externalCatalog.isError && (
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={<Button onClick={() => void externalCatalog.refetch()}>Retry</Button>}
        >
          Failed to load org catalog
          {externalCatalog.error instanceof Error && externalCatalog.error.message
            ? `: ${externalCatalog.error.message}`
            : ""}
          {" — Configure is hidden on connections until it loads."}
        </Alert>
      )}
      {(runs.isError || deployedRuns.isError) && (
        // Without the run story the board cannot tell a parked deployment
        // from one still pending, nor read an older deployed version's
        // verdict — so it says so rather than drawing the ordinary state.
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={
            <Button
              onClick={() => {
                if (runs.isError) void runs.refetch();
                if (deployedRuns.isError) void deployedRuns.refetch();
              }}
            >
              Retry
            </Button>
          }
        >
          The version's run story could not be loaded
          {(runs.error ?? deployedRuns.error) instanceof Error &&
          (runs.error ?? deployedRuns.error)?.message
            ? `: ${(runs.error ?? deployedRuns.error)?.message}`
            : ""}
          {" — a deployment on hold, or the deployed version's validation, cannot be read until it is."}
        </Alert>
      )}
      <EnvironmentFlow
        projectName={projectName}
        environments={environmentList}
        rows={rows}
        deploy={cardDeploy}
        validation={validation}
        version={version}
        milestone={milestoneFor(version || undefined, builds.data)}
        {...(milestoneHref ? { milestoneHref } : {})}
        {...(builtAt ? { builtAt } : {})}
        {...(commit ? { commit } : {})}
        hold={hold}
        componentTypes={componentTypes}
        connections={devLines}
        promote={promote}
        pending={{
          // The status poll names `version`, and `promote` is null without
          // one: an unsettled poll must not be read as "nothing is deployed".
          deploy: statusUnsettled,
          connections: dependencies.isPending || (readiness.isPending && !readiness.isError),
          // The VERDICT is `status.data.deploy.validation`, so the status poll
          // gates step 2 as much as it gates step 3. Neither flag below is
          // true while the poll is out — `version` is "" so the evidence read
          // never starts — and without the poll folded in, `validationStep`
          // read an absent aggregate off a green step 1 and said "Starts
          // automatically now that the deployment is live." over a project
          // whose validation had already settled.
          validation: validation.pending || deployedState.pending || statusUnsettled,
          hold: Boolean(status.data?.build.version) && runs.isPending,
        }}
        validationUnavailable={deployedState.failed}
        onTryOut={(environment) =>
          void navigate({
            to: "/projects/$projectName/deployments/$environment",
            params: { projectName, environment },
          })
        }
        onPromote={() => {
          setPromoteFocus(null);
          setPromoteOpen(true);
        }}
        onConfigureConnection={setValuesTarget}
        onConfigurePromoteTarget={(row) => {
          setPromoteFocus(row.id);
          setPromoteOpen(true);
        }}
      />

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
          environment={entryEnvironment}
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
          {...(promoteFocus ? { focusRowId: promoteFocus } : {})}
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

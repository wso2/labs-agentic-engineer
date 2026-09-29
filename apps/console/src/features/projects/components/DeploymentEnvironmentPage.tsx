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
  Button,
  Skeleton,
  Snackbar,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { Compass } from "@wso2/oxygen-ui-icons-react";
import { createLink, Link, useNavigate } from "@tanstack/react-router";
import { useHasPermission } from "../../../auth/permissions";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { PermissionRestrictedPage } from "../../../components/PermissionRestrictedPage";
import { useBuildRuns, useBuilds } from "../../builds/api/queries";
import { runStamp } from "../../builds/lib/format";
import { mergedCycle } from "../../builds/lib/runView";
import { isRegisteredExternal } from "../../marketplace/kind";
import { useExternalResources } from "../../settings/api/queries";
import { useDesignDependencies } from "../../spec/api/queries";
import { useValidationEvidence } from "../../validation/api/counts";
import {
  useComponentsDeployments,
  useEnvironments,
  useProjectComponents,
  useProjectDependencyReadiness,
  useProjectStatus,
} from "../api/queries";
import { connectionTable, talksTo } from "../lib/deploymentDetail";
import { deployedValidationState, } from "../lib/deploymentFlow";
import {
  buildFor,
  commitUrl,
  environmentLabel,
  environmentRows,
  milestoneUrl,
  validationCell,
} from "../lib/deploymentLedger";
import { historyFor } from "../lib/environmentHistory";
import { findEnvironment } from "../lib/environments";
import { groupDeploymentCards } from "../lib/deploymentRows";
import { connectionRows, type ConnectionRow } from "../lib/promotion";
import { ComponentOpenApiDialog } from "./ComponentOpenApiDialog";
import { ConnectionValuesDialog } from "./ConnectionValuesDialog";
import { DependenciesTable } from "./DependenciesTable";
import { EnvironmentDeploymentSummary } from "./EnvironmentDeploymentSummary";
import { PageSection } from "./PageSection";
import { PastDeployments } from "./PastDeployments";
import { TryItOutCard, useTestUsers } from "./TryItOut";

const LinkButton = createLink(Button);

/**
 * THE ENVIRONMENT PAGE (the multi-environment design, §6) — the one page under
 * Deployments, in four sections and this order:
 *
 *   1. Deployment — what runs here now, and where it came from.
 *   2. Try it out — the live components as things a person can act on: a web
 *      application is visited and carries the test users that sign in to it, a
 *      service names its URL and opens its contract in the viewer.
 *   3. Dependencies — what the design depends on, and whether this
 *      environment holds values for it.
 *   4. Past deployments — what has run here. There is no per-version page any
 *      more; a superseded version is a row.
 *
 * Keyed by ENVIRONMENT because a release binding is current state: there is
 * exactly one deployment per environment.
 *
 * Only the pipeline's ENTRY environment can resolve a version, a milestone, a
 * commit, a verdict, dependency readiness or a past. A later environment
 * omits each of them rather than showing the entry environment's under its own
 * name, and every section says when a read is still out or failed rather than
 * drawing an empty state that would be a claim.
 */
export function DeploymentEnvironmentPage({
  projectName,
  environment: segment,
}: {
  projectName: string;
  environment: string;
}) {
  const navigate = useNavigate();
  const canViewDeployments = useHasPermission("ae:build-view");
  // The environment is whatever the pipeline calls it; the list is the only
  // authority on which names exist. While it is still loading the segment is
  // taken at its word — a page that flashed "no such environment" on every
  // load would be lying about what it knows.
  const environments = useEnvironments();
  const environmentList = environments.data ?? [];
  const envInfo = findEnvironment(environmentList, segment);
  const environment = envInfo || environments.isPending ? segment : null;
  const components = useProjectComponents(projectName);
  const componentNames = (components.data?.items ?? []).map((c) => c.name);
  const deployments = useComponentsDeployments(projectName, componentNames);
  const status = useProjectStatus(projectName);
  const deploy = status.data?.deploy;

  // The version this environment runs — the aggregate names only the one a
  // build lands in, the first of the pipeline.
  const version =
    envInfo?.position === 0 && deploy?.version ? deploy.version : undefined;
  // The version's run story, for the commit that shipped it. Tag-scoped and
  // DB-only; the Builds surfaces make the same read, so it is served from cache
  // whenever the reader came from there.
  const runs = useBuildRuns(projectName, version);
  // The aggregate's validation names the BUILD version; this page names the
  // deployed one, which answers for itself off the run story just read when
  // the two differ (`deployedValidation`).
  const deployedState = deployedValidationState(deploy, status.data?.build.version ?? "", runs);
  const pageDeploy = deploy ? { ...deploy, validation: deployedState.validation } : undefined;
  const validationAvailability = deployedState.pending
    ? ("pending" as const)
    : deployedState.failed
      ? ("failed" as const)
      : undefined;
  const validation = useValidationEvidence(projectName, version ?? "", pageDeploy?.validation ?? "");
  // The design's graph — who talks to whom. Its connections are the version
  // page's business (#779 review).
  const dependencies = useDesignDependencies(projectName);
  const connections = useMemo(() => connectionRows(dependencies.data), [dependencies.data]);
  // Values are collected where they are FIRST needed — the environment a
  // build lands in. Anywhere else the read stays idle, and the table offers
  // no Edit.
  const readiness = useProjectDependencyReadiness(
    projectName,
    envInfo?.position === 0 ? segment : "",
  );
  const externalCatalog = useExternalResources();
  const catalogUnknown = externalCatalog.isPending || externalCatalog.isError;
  const registeredNames = useMemo(() => {
    const names = new Set<string>();
    for (const resource of externalCatalog.data ?? []) {
      if (isRegisteredExternal(resource)) names.add(resource.name);
    }
    return names;
  }, [externalCatalog.data]);

  const [contractComponent, setContractComponent] = useState<string | null>(null);
  const [valuesTarget, setValuesTarget] = useState<ConnectionRow | null>(null);
  const [valuesSaved, setValuesSaved] = useState(false);

  const envLabel = environmentLabel(envInfo, segment);
  // "Staging Environment" — the environment's own name is the page's title,
  // the word Environment small beside it (the approved design, §6).
  const title = (
    <Stack direction="row" spacing={1} sx={{ alignItems: "baseline" }}>
      <span>{envLabel}</span>
      <Typography variant="body2" color="text.secondary">
        Environment
      </Typography>
    </Stack>
  );
  const backTo = {
    link: <Link to="/projects/$projectName/deployments" params={{ projectName }} />,
    label: "Back to Deployments",
  };

  // Everything below needs the board; these are computed before the early
  // returns so the test-users read can be mounted unconditionally (a hook).
  const board = groupDeploymentCards(
    components.data?.items ?? [],
    deployments.deployments,
    environmentList[0]?.name ?? "",
  );
  const row = environment
    ? environmentRows(board, environmentList, deploy).find((r) => r.environment === environment)
    : undefined;
  const bound = row?.cards.some((c) => c.deployment) ?? false;
  // The version ledger — the milestone this version's work lived in, and the
  // stamps its build recorded. It speaks for the environment a build LANDS
  // in and no other, so a later environment reads none of it.
  const builds = useBuilds(projectName);
  const build = buildFor(version, builds.data);
  const mergeSha = mergedCycle(runs.data?.runs)?.mergeSha ?? "";
  const commitHref = commitUrl(status.data?.repoUrl, mergeSha);
  const commit: { sha: string; href?: string } | "loading" | undefined = !version
    ? undefined
    : runs.isPending
      ? "loading"
      : mergeSha
        ? { sha: mergeSha, ...(commitHref ? { href: commitHref } : {}) }
        : undefined;
  const milestoneHref = milestoneUrl(status.data?.repoUrl, build?.milestoneNumber);
  // The status poll is what names the version; the version ledger is what
  // names its milestone and its build stamp. Section 1 OMITS a cell it cannot
  // resolve, and omission on this page means "this environment has no such
  // fact" — so an entry environment whose ledger has not answered must not
  // render as one that has none. Both reads gate the section, exactly as the
  // ledger read gates section 4.
  const entry = envInfo?.position === 0;
  const summaryPending = Boolean(entry && (status.isPending || builds.isPending));
  // A FAILED read is not a pending one. Both queries do keep polling while
  // they hold no data (their refetchInterval treats an errored read as "no
  // data yet"), so an answer may well arrive — but a skeleton PROMISES an
  // imminent one, and a read that just failed cannot promise that. So a
  // failure says what happened and offers a Retry that asks again now,
  // rather than shimmering indefinitely with nothing on screen to act on.
  const statusFailed = Boolean(entry && status.isError);
  const buildsFailed = Boolean(entry && builds.isError);
  const deployedStamp = runStamp(row?.deployedAt);
  const builtAt = runStamp(build?.completedAt);
  const subtitle = environment
    ? [
        projectName,
        version && deployedStamp
          ? `running ${version} since ${deployedStamp}`
          : version
            ? `running ${version}`
            : deployedStamp
              ? `running since ${deployedStamp}`
              : "",
      ]
        .filter(Boolean)
        .join(" · ")
    : projectName;
  // Test users live with the app they sign in to. Read only for a green first
  // environment — the roles read stays idle until there is something to sign
  // in to, as it did on the board. Green is the row's own word, which folds
  // live bindings under a `none` aggregate to Deployed (deploymentLedger):
  // an app that is serving is one a test user can sign in to, whatever
  // rollout the aggregate is tracking.
  // Behaviour preserved: the roles read stays where it has always been, the
  // first environment of the pipeline. Whether that is because the test users
  // belong to the deployment a build lands in, or because credentials are
  // deliberately not offered downstream of it, the code does not say — both
  // read as position 0 today (see task 6 report).
  const green =
    envInfo?.position === 0 &&
    row?.status.label === "Deployed" &&
    (row?.total ?? 0) > 0 &&
    row?.live === row?.total;
  const testUsers = useTestUsers(projectName, Boolean(green));

  // Every hook above must run first — React's rule against conditional hooks
  // — so the gate sits here, after all of them, rather than before any.
  if (!canViewDeployments) {
    return (
      <PermissionRestrictedPage
        title="You don't have access to this project's deployments"
        description="Environment status, connections, and promotion are restricted for your role. Ask a project admin to grant access."
        backLabel="Back to project overview"
        onBack={() =>
          void navigate({ to: "/projects/$projectName", params: { projectName } })
        }
      />
    );
  }

  // A failed read is not a verdict on the segment. Without this the page would
  // tell the user there is no such environment — a permanent-sounding fact —
  // when all that happened is that a request failed.
  if (environments.isError) {
    return (
      <>
        <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
        <Alert
          severity="warning"
          action={<Button onClick={() => void environments.refetch()}>Retry</Button>}
        >
          The platform's environments could not be read
          {environments.error instanceof Error && environments.error.message
            ? `: ${environments.error.message}`
            : ""}
          {" — this page cannot say what runs in "}
          {segment} until they load.
        </Alert>
      </>
    );
  }

  if (!environment) {
    // An unknown segment is a dead end with a way out, not a blank page with a
    // title on it.
    return (
      <>
        <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
        <EmptyState
          icon={<Compass size={48} />}
          title={`No environment called ${segment}`}
          description={
            environmentList.length > 0
              ? `Deployments live in ${environmentList.map((e) => e.displayName || e.name).join(", ")}.`
              : "That environment is not one this platform deploys to."
          }
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
      </>
    );
  }

  // The board is one row per environment the pipeline names, so a row for
  // THIS one cannot be drawn — or honestly called empty — until that list is
  // in. Without this the page would assert "Nothing deployed here yet" over
  // a deployed environment for as long as the environments read takes.
  if (
    components.isPending ||
    environments.isPending ||
    (componentNames.length > 0 && deployments.isPending)
  ) {
    return (
      <>
        <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
        <Stack spacing={2} sx={{ mt: 2 }} aria-label="Loading deployments">
          <Skeleton variant="rounded" height={140} />
          <Skeleton variant="rounded" height={220} />
        </Stack>
      </>
    );
  }

  if (components.isError) {
    return (
      <>
        <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
        <Alert
          severity="error"
          action={<Button onClick={() => void components.refetch()}>Retry</Button>}
        >
          Failed to load deployments
          {components.error instanceof Error && components.error.message
            ? `: ${components.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  if (!row || !bound) {
    // "Nothing is deployed here" is a claim about every component, and a read
    // that FAILED supports no claim at all — so a page that lost some of them
    // says THAT instead (#714 review). The two must not render together: an
    // empty state beside a load warning tells the reader both that the
    // environment is empty and that the page could not find out.
    //
    // The queries keep polling on failure (their interval is the active one
    // while they hold no data), so this state resolves itself and needs no
    // Retry of its own.
    return (
      <>
        <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
        {deployments.failedCount > 0 ? (
          <Alert severity="warning">
            Deployments for {deployments.failedCount} component
            {deployments.failedCount === 1 ? "" : "s"} could not be loaded, so
            there is nothing this page can say about {envLabel}{" "}
            yet. It keeps retrying.
          </Alert>
        ) : (
          <EmptyState
            compact
            description={
              envInfo?.position === 0
                ? `Nothing deployed here yet — agents deploy to ${envLabel} when a build merges.`
                : "Nothing deployed here yet — promote a validated version from the environment before this one."
            }
          />
        )}
      </>
    );
  }

  const types = new Map<string, string>();
  for (const c of components.data?.items ?? []) if (c.type) types.set(c.name, c.type);
  // The WORD, not the counts: section 1 shows the verdict and its counts as
  // two things (the approved design), so the cell is asked for its label
  // alone and the counts ride beside it.
  const validationView = validationCell(
    envInfo,
    pageDeploy?.validation,
    undefined,
    validationAvailability,
  );

  // What has run here. `undefined` builds is a read that has not answered —
  // `historyFor` reports that as pending rather than as an empty past, so the
  // section never says "nothing ran here" over a slow or failed ledger.
  const history = historyFor(
    envInfo ?? { name: segment, displayName: envLabel, isProduction: false, validation: "off", position: -1 },
    row,
    builds.isError ? [] : builds.data,
  );
  const readinessOut = envInfo?.position === 0 && readiness.isPending && !readiness.isError;
  const dependencyRows = connectionTable(
    connections,
    dependencies.data,
    readiness.data,
    envInfo,
    registeredNames,
    catalogUnknown,
  );

  return (
    <>
      {/* No status chip beside the title — section 1 below carries the
          verdict, and two of one fact in one screenful is one too many
          (review round). */}
      <PageHeader title={title} subtitle={subtitle} backTo={backTo} />
      {deployments.failedCount > 0 && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          Deployments for {deployments.failedCount} component
          {deployments.failedCount === 1 ? "" : "s"} could not be loaded — the
          page shows what did.
        </Alert>
      )}
      {statusFailed && (
        // Without the status poll the page cannot name the version running
        // here — and "Version unknown" is a claim about a settled read, which
        // this is not.
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={<Button onClick={() => void status.refetch()}>Retry</Button>}
        >
          The project's status could not be read
          {status.error instanceof Error && status.error.message
            ? `: ${status.error.message}`
            : ""}
          {" — the version running in "}
          {envLabel} cannot be named until it is.
        </Alert>
      )}
      {runs.isError && (
        // When this version is behind the build, its verdict is its own run
        // story's. Without it the chip says so rather than settling on
        // "Not run".
        <Alert
          severity="warning"
          sx={{ mb: 2 }}
          action={<Button onClick={() => void runs.refetch()}>Retry</Button>}
        >
          The version's run story could not be loaded
          {runs.error instanceof Error && runs.error.message ? `: ${runs.error.message}` : ""}
        </Alert>
      )}
      <Stack spacing={2}>
        <PageSection title="Deployment">
          <EnvironmentDeploymentSummary
            {...(version ? { version } : {})}
            bound={bound}
            pending={summaryPending}
            versionUnavailable={statusFailed}
            ledgerUnavailable={buildsFailed}
            {...(build?.milestoneNumber ? { milestoneNumber: build.milestoneNumber } : {})}
            {...(milestoneHref ? { milestoneHref } : {})}
            {...(commit ? { commit } : {})}
            validation={validationView}
            {...(validation.counts ? { counts: validation.counts } : {})}
            {...(builtAt ? { builtAt } : {})}
            {...(deployedStamp ? { deployedAt: deployedStamp } : {})}
            live={row.live}
            total={row.total}
          />
        </PageSection>

        <TryItOutCard
          cards={row.cards}
          types={types}
          talksTo={(name) => talksTo(dependencies.data, name)}
          testUsers={green ? testUsers : null}
          onTryApi={setContractComponent}
        />

        {/* Section 3. A failed design read says so rather than claiming the
            design declares nothing; the readiness read holds the table too —
            drawn before it answers, every external would read Unknown as if
            that were settled — and a failed one says so over the table, since
            Unknown is then the honest word. */}
        {dependencies.isError ? (
          <Alert
            severity="warning"
            action={<Button onClick={() => void dependencies.refetch()}>Retry</Button>}
          >
            The design's dependencies could not be loaded
            {dependencies.error instanceof Error && dependencies.error.message
              ? `: ${dependencies.error.message}`
              : ""}
          </Alert>
        ) : dependencies.isPending || readinessOut ? (
          <Skeleton variant="rounded" height={160} data-testid="dependencies-skeleton" />
        ) : (
          <>
            {readiness.isError && (
              <Alert
                severity="warning"
                action={<Button onClick={() => void readiness.refetch()}>Retry</Button>}
              >
                Whether {envLabel} holds values for these dependencies could not be read
                {readiness.error instanceof Error && readiness.error.message
                  ? `: ${readiness.error.message}`
                  : ""}
                {" — each reads Unknown until it is."}
              </Alert>
            )}
            <DependenciesTable
              environmentLabel={envLabel}
              rows={dependencyRows}
              onEdit={setValuesTarget}
            />
          </>
        )}

        <PastDeployments
          environmentLabel={envLabel}
          view={history}
          {...(status.data?.repoUrl ? { repoUrl: status.data.repoUrl } : {})}
          validation={validationView}
          {...(buildsFailed
            ? {
                failed:
                  builds.error instanceof Error && builds.error.message
                    ? builds.error.message
                    : "the read failed",
                onRetry: () => void builds.refetch(),
              }
            : {})}
        />
      </Stack>

      <ComponentOpenApiDialog
        projectName={projectName}
        componentName={contractComponent}
        onClose={() => setContractComponent(null)}
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
          environment={environmentList[0]?.name ?? ""}
        />
      )}
      <Snackbar
        open={valuesSaved}
        autoHideDuration={6000}
        onClose={() => setValuesSaved(false)}
      >
        <Alert severity="success" onClose={() => setValuesSaved(false)}>
          Values saved — the dependency re-provisions with them.
        </Alert>
      </Snackbar>
    </>
  );
}

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
  CircularProgress,
  Snackbar,
  Stack,
} from "@wso2/oxygen-ui";
import { Link, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { useBuilds } from "../../builds/api/queries";
import { useDesignDependencies } from "../../spec/api/queries";
import { useExternalResources } from "../../settings/api/queries";
import { isRegisteredExternal } from "../../marketplace/kind";
import { useValidationEvidence } from "../../validation/api/counts";
import {
  useComponentsDeployments,
  useProjectComponents,
  useProjectStatus,
} from "../api/queries";
import { environmentRows, ledgerRows } from "../lib/deploymentLedger";
import { groupDeploymentCards } from "../lib/deploymentRows";
import {
  configuredCount,
  connectionRows,
  seedValues,
  type ConnectionRow,
  type ConnectionValues,
} from "../lib/promotion";
import { ConnectionsCard } from "./ConnectionsCard";
import { ConnectionValuesDialog } from "./ConnectionValuesDialog";
import { DeploymentsLedger } from "./DeploymentsLedger";
import { EnvironmentCards } from "./EnvironmentCards";
import { PromoteDialog } from "./PromoteDialog";

/**
 * Deployments as an ENVIRONMENT BOARD (ADR-0027, artboard 1c): a card per
 * environment — what runs there, how much of it is up, the verdict on it and
 * the promotion it leads to — then a ledger with one row per environment that
 * runs something, each opening the environment's own page.
 *
 * Data is unchanged from the story rail this replaces: the components list,
 * one list-deployments read per component, the status poll's deploy aggregate,
 * the Spec view's design-dependencies read for the connections a promotion
 * must configure — plus the version ledger the layout already holds, for the
 * Milestone cell. No new contract surface.
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
  // The design's connections, for promotion readiness. A failed read surfaces
  // as `isError` at the hook; this page degrades it here — `connectionRows`
  // maps an absent payload to [], so the board renders without a
  // live-configuration line rather than blocking the page.
  const dependencies = useDesignDependencies(projectName);
  const connections = useMemo(
    () => connectionRows(dependencies.data),
    [dependencies.data],
  );
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
  // The Validation page's own criteria/report join, keyed on the BUILD version
  // (the newest run — what deploy.validation describes). The VERDICT comes back
  // with the counts because `awaiting-fix` folds `failed` and `unreported` into
  // one word and the banner's sentence differs for each.
  const validation = useValidationEvidence(
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
          description="Nothing deployed yet. Your components run here once they are built — each environment shows what is live and where to reach it."
        />
      </>
    );
  }

  const board = groupDeploymentCards(
    components.data?.items ?? [],
    deployments.deployments,
  );
  const rows = environmentRows(board, deploy);
  const development = rows[0];
  const production = rows[1] ?? {
    environment: "production" as const,
    label: "Production",
    cards: [],
    status: { label: "Nothing deployed", tone: "neutral" as const, live: false },
    live: 0,
    total: 0,
  };
  const configured = configuredCount(connections, liveValues);

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
      <Stack spacing={2}>
        {development && (
          <EnvironmentCards
            projectName={projectName}
            development={development}
            production={production}
            deploy={deploy}
            validation={validation}
            connectionCount={dependencies.isPending ? null : connections.length}
            configured={configured}
            onPromote={() => setPromoteOpen(true)}
          />
        )}

        <DeploymentsLedger
          rows={ledgerRows(rows)}
          builds={builds.data}
          validation={deploy?.validation}
          counts={validation.counts}
          onOpen={(row) =>
            void navigate({
              to: "/projects/$projectName/deployments/$environment",
              params: { projectName, environment: row.environment },
            })
          }
        />

        <ConnectionsCard
          connections={connections}
          registeredNames={registeredNames}
          catalogUnknown={catalogUnknown}
          {...(externalCatalog.isError
            ? {
                catalogError: {
                  message:
                    externalCatalog.error instanceof Error
                      ? externalCatalog.error.message
                      : "",
                  retry: () => void externalCatalog.refetch(),
                },
              }
            : {})}
          onConfigure={setValuesTarget}
        />
      </Stack>

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

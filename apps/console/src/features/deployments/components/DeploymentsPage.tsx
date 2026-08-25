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
import { Alert, Snackbar } from "@wso2/oxygen-ui";
import { useDesignDependencies } from "../../spec/api/queries";
import { useProjectStatus } from "../../projects/api/queries";
import {
  connectionRows,
  seedValues,
  type ConnectionValues,
} from "../../projects/lib/promotion";
import { PromoteDialog } from "../../projects/components/PromoteDialog";
import { DeploymentsBoard } from "./DeploymentsBoard";

/**
 * The Deployments page (ADR-0020 §5).
 *
 * The BOARD is the new part. Promotion is deliberately unchanged — the same
 * `PromoteDialog`, the same connection-values flow, the same
 * not-wired-yet notice — because #609 scoped this feature to where the promote
 * button lives, not to what it does.
 */
export function DeploymentsPage({ projectName }: { projectName: string }) {
  const status = useProjectStatus(projectName);
  const dependencies = useDesignDependencies(projectName);
  const connections = connectionRows(dependencies.data ?? []);

  // Production values entered through the promote dialog. Client state only:
  // the contract has no promote surface yet, so these live exactly as long as
  // the page does — seeded from the config keys' defaults.
  const [values, setValues] = useState<ConnectionValues | null>(null);
  const liveValues = values ?? seedValues(connections);
  const [promoteVersion, setPromoteVersion] = useState<string | null>(null);
  const [promoteNotice, setPromoteNotice] = useState(false);
  const [redeployNotice, setRedeployNotice] = useState(false);

  return (
    <>
      <DeploymentsBoard
        projectName={projectName}
        onPromote={(deployment) => setPromoteVersion(deployment.tag)}
        onRedeploy={() => setRedeployNotice(true)}
      />

      {promoteVersion && (
        <PromoteDialog
          open
          onClose={() => setPromoteVersion(null)}
          projectName={projectName}
          version={promoteVersion}
          validation={status.data?.deploy.validation ?? ""}
          rows={connections}
          values={liveValues}
          onValueChange={(rowId, key, value) =>
            setValues({
              ...liveValues,
              [rowId]: { ...liveValues[rowId], [key]: value },
            })
          }
          onPromote={() => {
            setPromoteVersion(null);
            setPromoteNotice(true);
          }}
        />
      )}

      {/* Redeploy is drawn in the design but has no endpoint behind it. Wiring
          it to a refetch would look like it worked and change nothing in the
          cluster, so it says what it actually is. */}
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

      {/* Promotion has no platform surface yet (no promote endpoint in the
          contract) — saying so is honest about what just happened instead of
          pretending a deploy started. */}
      <Snackbar
        open={promoteNotice}
        autoHideDuration={6000}
        onClose={() => setPromoteNotice(false)}
      >
        <Alert severity="info" onClose={() => setPromoteNotice(false)}>
          Production promotion isn&apos;t wired to the platform yet — your live
          configuration is kept for this session.
        </Alert>
      </Snackbar>
    </>
  );
}

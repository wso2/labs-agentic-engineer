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
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, Link, Skeleton, Typography } from "@wso2/oxygen-ui";
import { useProjectComponents } from "../../deploy/api/deploy";
import { TryItPanel } from "../../deploy/components/TryItPanel";
import { componentKindLabel } from "../../deploy/model/pipeline";
import { componentTry, type ComponentTry } from "../../deploy/model/tryIt";
import { useEnvironmentColumns } from "../../deploy/useDeployBoard";

// The overview's components, in the design's order, each with Try it: the
// Deploy Page's Try it Panel, for that one component, opened on the
// environment that runs its newest version, with a switch to the others it
// serves in. Whether its dependencies have their values is the Deploy Page's
// to say, not this list's.

const DeployLink = createLink(Link);

const STATE_NOTE: Record<Exclude<ComponentTry["kind"], "serving" | "not-deployed">, string> = {
  deploying: "Deploying in",
  failed: "Deploy failed in",
  running: "Running in",
};

function NotServing({ projectName, state }: { projectName: string; state: Exclude<ComponentTry, { kind: "serving" }> }) {
  const note = state.kind === "not-deployed" ? "Not deployed yet" : `${STATE_NOTE[state.kind]} ${state.environment.label}`;
  return (
    <Typography variant="caption" color={state.kind === "failed" ? "error.main" : "text.secondary"} sx={{ textAlign: "end" }}>
      {note} ·{" "}
      <DeployLink to="/projects/$projectName/deploy" params={{ projectName }} underline="hover">
        Deploy
      </DeployLink>
    </Typography>
  );
}

export function OverviewComponents({ projectName }: { projectName: string }) {
  const components = useProjectComponents(projectName);
  const board = useEnvironmentColumns(projectName);
  // By name, so the panel follows the board as it refreshes.
  const [trying, setTrying] = useState<{ component: string; environment: string } | null>(null);

  if (components.isError) {
    return (
      <Alert severity="error" action={<Button onClick={() => void components.refetch()}>Retry</Button>}>
        {components.error.message}
      </Alert>
    );
  }
  if (!components.data) return <Skeleton variant="rounded" height={96} />;
  if (components.data.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        No components yet. The design names them, and the first build creates them.
      </Typography>
    );
  }

  const columns = board.columns;
  const tryColumn = trying ? columns?.find((c) => c.name === trying.environment) : undefined;
  const tryState = trying && columns ? componentTry(columns, trying.component) : null;

  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
      {components.data.map((c) => {
        const kind = componentKindLabel(c.type);
        const state = columns ? componentTry(columns, c.name) : null;
        return (
          <Box
            key={c.name}
            sx={{
              display: "flex",
              alignItems: "center",
              gap: 2,
              px: 1.75,
              py: 1.25,
              "& + &": { borderTop: 1, borderColor: "divider" },
            }}
          >
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography variant="body2" sx={{ fontWeight: 600 }}>
                {c.displayName || c.name}
                {kind && (
                  <Typography component="span" variant="caption" color="text.secondary" sx={{ fontWeight: 400 }}>
                    {" "}
                    · {kind}
                  </Typography>
                )}
              </Typography>
              {c.description && (
                <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
                  {c.description}
                </Typography>
              )}
            </Box>
            {board.error ? (
              <Typography variant="caption" color="text.secondary">
                Where it runs could not be read.
              </Typography>
            ) : !state ? (
              <Skeleton width={72} />
            ) : state.kind === "serving" ? (
              <Button
                size="small"
                variant="outlined"
                onClick={() => setTrying({ component: c.name, environment: state.environments[0]!.name })}
              >
                Try it
              </Button>
            ) : (
              <NotServing projectName={projectName} state={state} />
            )}
          </Box>
        );
      })}
      {trying && tryColumn && tryState?.kind === "serving" && (
        <TryItPanel
          projectName={projectName}
          column={tryColumn}
          entryLabel={columns?.[0]?.label ?? ""}
          focus={{
            componentName: trying.component,
            environments: tryState.environments,
            onEnvironment: (environment) => setTrying({ ...trying, environment }),
          }}
          onClose={() => setTrying(null)}
        />
      )}
    </Box>
  );
}

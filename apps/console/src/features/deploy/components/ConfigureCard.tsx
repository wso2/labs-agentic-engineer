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
import { Alert, Box, Button, Chip, Skeleton, TextField, Typography } from "@wso2/oxygen-ui";
import { EmptyState } from "../../../components/EmptyState";
import { CardOverlay } from "../../projects/components/CardOverlay";
import { projectLabel, useProject } from "../../projects/api/queries";
import { useDesignDependencies, useEnvironments, useReadiness, useSaveDependencyValues } from "../api/deploy";
import {
  configureDependencies,
  nameList,
  orgHeldDependencies,
  valuesComplete,
  type ConfigureDependency,
} from "../model/dependencies";

// An environment's Configure card, over the Deploy Page: the values each
// dependency reads in that environment (Xero's client id, secret, tenant).
// It sets no Turn scope: no agent can change an environment yet, so the chat
// stays on the whole product.
//
// Values are write-only. Secrets go to the platform's secret manager and
// nothing stored is echoed back, so each form opens empty and saving replaces
// all of a dependency's values; the platform re-provisions it with them.

function DependencyForm({
  projectName,
  environment,
  dependency,
}: {
  projectName: string;
  environment: { name: string; label: string };
  dependency: ConfigureDependency;
}) {
  const save = useSaveDependencyValues(projectName);
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const ready = dependency.state === "configured";
  const complete = valuesComplete(dependency.keys, values);

  return (
    <Box component="section" aria-label={dependency.name} sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography component="h3" sx={{ fontWeight: 600 }}>
          {dependency.name}
        </Typography>
        <Chip
          size="small"
          variant="outlined"
          color={ready ? "success" : "warning"}
          label={ready ? "Has its values" : "Needs values"}
        />
      </Box>
      {dependency.keys.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          The design declares no values for {dependency.name}.
        </Typography>
      ) : (
        <>
          <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" }, gap: 1.25 }}>
            {dependency.keys.map((key) => (
              <TextField
                key={key.key}
                size="small"
                fullWidth
                label={key.key}
                {...(key.description ? { helperText: key.description } : {})}
                type={key.secret ? "password" : "text"}
                autoComplete="off"
                value={values[key.key] ?? ""}
                onChange={(e) => {
                  setSaved(false);
                  setValues((v) => ({ ...v, [key.key]: e.target.value }));
                }}
                sx={{ "& input": { fontFamily: "monospace" } }}
              />
            ))}
          </Box>
          {save.isError && <Alert severity="error">{save.error.message}</Alert>}
          {saved && (
            <Alert severity="success">
              Saved. {dependency.name} is set up again in {environment.label} with these values.
            </Alert>
          )}
          <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
            <Button
              size="small"
              variant="contained"
              disabled={!complete || save.isPending}
              onClick={() =>
                save.mutate(
                  { name: dependency.name, environment: environment.name, values },
                  {
                    onSuccess: () => {
                      setValues({});
                      setSaved(true);
                    },
                  },
                )
              }
            >
              {save.isPending ? "Saving…" : "Save values"}
            </Button>
            <Typography variant="caption" color="text.secondary">
              {ready ? "Saving replaces the values it has." : "Every value is needed."}
            </Typography>
          </Box>
        </>
      )}
    </Box>
  );
}

function ConfigureBody({ projectName, environment }: { projectName: string; environment: { name: string; label: string } }) {
  const project = useProject(projectName);
  const readiness = useReadiness(projectName, environment.name);
  const design = useDesignDependencies(projectName);
  const productName = project.data ? projectLabel(project.data) : projectName;

  if (readiness.isError || design.isError) {
    const failed = readiness.isError ? readiness : design;
    return (
      <Alert
        severity="error"
        action={
          <Button color="inherit" size="small" onClick={() => void failed.refetch()}>
            Try again
          </Button>
        }
      >
        {failed.error?.message}
      </Alert>
    );
  }
  if (!readiness.data || !design.data) {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Skeleton width="70%" />
        <Skeleton variant="rounded" height={120} />
      </Box>
    );
  }

  const dependencies = configureDependencies(readiness.data, design.data);
  const orgHeld = orgHeldDependencies(design.data);
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, maxWidth: 640 }}>
      <Typography color="text.secondary">
        What {environment.label} needs before {productName} runs there: the values each dependency reads in this
        environment. Values you saved never show here; secrets go to the platform&apos;s secret manager.
      </Typography>
      {dependencies.length === 0 ? (
        <EmptyState compact bordered description={`Nothing in ${productName} needs values in ${environment.label}.`} />
      ) : (
        dependencies.map((d) => (
          <DependencyForm key={d.name} projectName={projectName} environment={environment} dependency={d} />
        ))
      )}
      {orgHeld.length > 0 && (
        <Typography variant="body2" color="text.secondary">
          {nameList(orgHeld)} {orgHeld.length === 1 ? "takes its" : "take their"} values from the organization&apos;s
          resources, so {orgHeld.length === 1 ? "it needs" : "they need"} none here.
        </Typography>
      )}
    </Box>
  );
}

/** The Configure card of one environment, as its route draws it. */
export function ConfigureCard({ projectName, env }: { projectName: string; env: string }) {
  const environments = useEnvironments();
  const found = environments.data?.find((e) => e.name === env);
  const environment = found ? { name: found.name, label: found.displayName || found.name } : null;
  return (
    <CardOverlay card="configure" title={`Configure ${environment?.label ?? env}`}>
      {environments.isError ? (
        <Alert severity="error">{environments.error.message}</Alert>
      ) : environments.isPending ? (
        <Skeleton width="60%" />
      ) : environment ? (
        <ConfigureBody projectName={projectName} environment={environment} />
      ) : (
        <EmptyState compact description={`This organization has no environment called ${env}.`} />
      )}
    </CardOverlay>
  );
}

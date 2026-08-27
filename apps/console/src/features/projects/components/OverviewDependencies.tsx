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
  ButtonBase,
  Chip,
  Skeleton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { SectionTitle } from "../../../components/SectionTitle";
import { EmptyState } from "../../../components/EmptyState";
import { CatalogTypeDrawer } from "../../marketplace/components/CatalogTypeDrawer";
import {
  useExternalResources,
  usePlatformResourceTypes,
} from "../../settings/api/queries";
import { useWorkloadDependencies } from "../api/queries";
import { ComponentOpenApiDialog } from "./ComponentOpenApiDialog";
import type { components } from "../../../generated/aep-api";

type WorkloadDependencyDTO = components["schemas"]["WorkloadDependencyDTO"];
type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

type ResourceSelection =
  | { kind: "platform"; resource: PlatformResourceTypeDTO }
  | { kind: "external"; resource: ExternalResourceDTO }
  | { kind: null; resource: null };

function rowLabel(row: WorkloadDependencyDTO): string {
  if (row.kind === "org-service") {
    return row.name ?? row.component ?? "org-service";
  }
  return row.name ?? row.ref ?? "resource";
}

function rowChipLabel(row: WorkloadDependencyDTO): string {
  if (row.kind === "org-service") return "Org-service";
  if (row.tag === "platform") return "Platform";
  return "External";
}

export function OverviewDependencies({ projectName }: { projectName: string }) {
  const deps = useWorkloadDependencies(projectName);
  const platform = usePlatformResourceTypes();
  const external = useExternalResources();
  const [selection, setSelection] = useState<ResourceSelection>({
    kind: null,
    resource: null,
  });
  const [openapi, setOpenapi] = useState<{
    project: string;
    component: string;
  } | null>(null);

  const rows = deps.data ?? [];
  const empty = useMemo(
    () => !deps.isPending && !deps.isError && rows.length === 0,
    [deps.isPending, deps.isError, rows.length],
  );

  const onRow = (row: WorkloadDependencyDTO) => {
    if (row.kind === "org-service") {
      if (row.project && row.component) {
        setOpenapi({ project: row.project, component: row.component });
      }
      return;
    }
    const key = row.ref ?? row.name;
    if (!key) return;
    if (row.tag === "platform") {
      const resource =
        platform.data?.find((r) => r.name === key) ?? { name: key };
      setSelection({ kind: "platform", resource });
      return;
    }
    const resource = external.data?.find((r) => r.name === key) ?? {
      name: key,
      config: [],
      consumers: [],
    };
    setSelection({ kind: "external", resource });
  };

  return (
    <Box sx={{ mt: 3 }}>
      <SectionTitle>Dependencies</SectionTitle>
      {deps.isError ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void deps.refetch()}>Retry</Button>}
        >
          Failed to load workload dependencies
          {deps.error instanceof Error ? `: ${deps.error.message}` : ""}
        </Alert>
      ) : deps.isPending ? (
        <Skeleton
          variant="rounded"
          height={72}
          aria-label="Loading dependencies"
        />
      ) : empty ? (
        <EmptyState
          compact
          bordered
          title="No deployed dependencies"
          description="Rows appear after a component has deployed. Unresolved design declarations stay off this list."
        />
      ) : (
        <Stack spacing={0.5}>
          {rows.map((row) => (
            <ButtonBase
              key={`${row.kind}-${row.ref ?? row.name}-${row.project ?? ""}-${row.component ?? ""}`}
              onClick={() => onRow(row)}
              sx={{
                display: "flex",
                alignItems: "center",
                justifyContent: "flex-start",
                gap: 1,
                px: 1,
                py: 0.75,
                borderRadius: 1,
                width: "100%",
                textAlign: "left",
                "&:hover": { bgcolor: "action.hover" },
              }}
            >
              <Chip
                size="small"
                variant="outlined"
                label={rowChipLabel(row)}
              />
              <Typography variant="body2" sx={{ fontWeight: 600 }}>
                {rowLabel(row)}
              </Typography>
              {row.kind === "org-service" && row.project && (
                <Typography variant="caption" color="text.secondary">
                  {row.project}
                  {row.component ? ` / ${row.component}` : ""}
                </Typography>
              )}
            </ButtonBase>
          ))}
        </Stack>
      )}
      <CatalogTypeDrawer
        {...selection}
        open={selection.kind !== null}
        onClose={() => setSelection({ kind: null, resource: null })}
      />
      <ComponentOpenApiDialog
        projectName={openapi?.project ?? projectName}
        componentName={openapi?.component ?? null}
        onClose={() => setOpenapi(null)}
      />
    </Box>
  );
}

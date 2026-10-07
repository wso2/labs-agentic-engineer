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

import type { ReactNode } from "react";
import { Alert, Box, Button, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { Tag } from "../../spec/components/Tag";
import { useProjectUsageList } from "../api/queries";
import { formatTokens, formatUsd, totalTokens, unpricedHostLine } from "../format";
import { UsageBreakdown } from "./UsageBreakdown";

type ProjectUsageCard = components["schemas"]["ProjectUsageCard"];

const cell = { py: 1.25, px: 1.5, borderBottom: 1, borderColor: "divider", verticalAlign: "top" } as const;
const numeric = { ...cell, textAlign: "right", fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap" } as const;

/**
 * Settings, Usage (#291): what the agents have spent on each project, in USD
 * and in tokens. Every live project has a row, an idle one at $0; a deleted
 * project keeps its row, greyed, since its spend was real. The dollar figure
 * opens the breakdown (token kinds, model, phases) on hover or focus.
 */
export function UsageSection() {
  const usage = useProjectUsageList();

  if (usage.isPending) return <Skeleton variant="rounded" height={160} aria-label="Loading usage" />;

  if (usage.isError) {
    return (
      <Alert severity="error" action={<Button onClick={() => void usage.refetch()}>Retry</Button>}>
        Failed to load usage{usage.error.message ? `: ${usage.error.message}` : ""}
      </Alert>
    );
  }

  const projects = usage.data.projects;
  if (projects.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        No agent usage yet. Spend shows here as the spec, design and coding agents run.
      </Typography>
    );
  }

  return (
    <Box component="table" sx={{ width: "100%", borderCollapse: "collapse", fontSize: "0.875rem" }}>
      <thead>
        <tr>
          <Head>Project</Head>
          <Head align="right">Spend</Head>
          <Head align="right">Tokens</Head>
        </tr>
      </thead>
      <tbody>
        {/* The name alone is not a key: a project deleted and recreated under
            the same one is two rows, and the live one does not inherit the
            spend of the one before. */}
        {projects.map((p) => (
          <ProjectRow key={`${p.projectName}:${p.deleted}`} project={p} />
        ))}
      </tbody>
    </Box>
  );
}

function Head({ align = "left", children }: { align?: "left" | "right"; children: ReactNode }) {
  return (
    <Box
      component="th"
      scope="col"
      sx={{
        ...cell,
        pt: 0,
        textAlign: align,
        fontSize: "0.6875rem",
        letterSpacing: "0.08em",
        textTransform: "uppercase",
        fontWeight: 600,
        color: "text.secondary",
      }}
    >
      {children}
    </Box>
  );
}

function ProjectRow({ project }: { project: ProjectUsageCard }) {
  const { usage } = project;
  const billedBy = unpricedHostLine(usage);
  return (
    <Box component="tr" sx={{ opacity: project.deleted ? 0.6 : 1 }}>
      <Box component="td" sx={cell}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>
            {project.displayName}
          </Typography>
          {project.deleted && <Tag tone={null}>Deleted project</Tag>}
        </Box>
        <Typography variant="caption" color="text.secondary">
          {project.projectName}
          {usage.model && ` · ${usage.model}`}
          {usage.costUsd === null && usage.host && ` via ${usage.host}`}
        </Typography>
      </Box>
      <Box component="td" sx={numeric}>
        <Tooltip
          title={<UsageBreakdown usage={usage} phases={project.phases} context={`Agent spend, ${project.displayName}`} />}
          slotProps={{ tooltip: { sx: { maxWidth: 320 } } }}
        >
          {/* Focusable, so the breakdown is not for pointers only: the
              tooltip opens on focus as well as hover. */}
          <Typography component="span" variant="body2" tabIndex={0} sx={{ fontVariantNumeric: "tabular-nums" }}>
            {usage.costUsd !== null ? formatUsd(usage.costUsd) : "Not priced"}
          </Typography>
        </Tooltip>
        {billedBy && (
          <Typography variant="caption" color="text.secondary" component="div">
            {billedBy}
          </Typography>
        )}
      </Box>
      <Box component="td" sx={{ ...numeric, color: "text.secondary" }}>
        {formatTokens(totalTokens(usage))}
      </Box>
    </Box>
  );
}

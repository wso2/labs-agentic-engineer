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

import { useState, type ReactNode } from "react";
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, ButtonBase, SearchBar, Skeleton, Typography } from "@wso2/oxygen-ui";
import { Boxes, Plus } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { Segmented } from "../../design/components/Segmented";
import { projectLabel, useProjects } from "../../projects/api/queries";
import { Tag } from "../../spec/components/Tag";
import { useExternalResources, useOrgEndpoints, usePlatformResourceTypes } from "../api/resources";
import { filterResources, resourceAddress, resourceEntries, type ResourceEntry, type ResourceFilter } from "../model/resources";

// The Resources Page: one list of everything a project can depend on, the
// platform's resource types, the organization's Registered External
// resources, the External resources projects hold for themselves, and what
// other projects offer; searched, and filtered by kind. Each opens its
// Resource card over the page; New resource opens an empty one.

const RowLink = createLink(ButtonBase);
const ButtonLink = createLink(Button);

const FILTERS: { value: ResourceFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "platform", label: "Platform" },
  { value: "external", label: "External" },
  { value: "endpoints", label: "From projects" },
];

/** A project's name as the console shows it: its display name when the first page of projects has it. */
export function useProjectLabel(): (slug: string) => string {
  const projects = useProjects();
  return (slug) => {
    const project = projects.data?.find((p) => p.name === slug);
    return project ? projectLabel(project) : slug;
  };
}

/** What a resource is, in a word, and whose. */
export function ResourceTags({ entry, label }: { entry: ResourceEntry; label: (slug: string) => string }) {
  switch (entry.kind) {
    case "platform":
      return <Tag tone={null}>Platform</Tag>;
    case "registered":
      return <Tag tone="primary">External</Tag>;
    case "project":
      return (
        <>
          <Tag tone={null}>In {label(entry.project)}</Tag>
          <Tag tone="primary">External</Tag>
        </>
      );
    case "endpoint":
      return <Tag tone={null}>From {label(entry.project)}</Tag>;
  }
}

function summaryOf(entry: ResourceEntry): string {
  switch (entry.kind) {
    case "platform":
      return entry.resource.description ?? "";
    case "registered":
    case "project":
      return [entry.resource.provider, entry.resource.description].filter(Boolean).join(" · ");
    case "endpoint":
      return `${entry.resource.type} endpoint ${entry.resource.endpoint}`;
  }
}

function Row({ entry, label }: { entry: ResourceEntry; label: (slug: string) => string }) {
  const { name, search } = resourceAddress(entry);
  return (
    <RowLink
      to="/resources/$name"
      params={{ name }}
      search={search}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1.5,
        justifyContent: "stretch",
        textAlign: "start",
        px: 1.75,
        py: 1.25,
        "& + &": { borderTop: 1, borderColor: "divider" },
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <Box component="span" sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
          {entry.name}
        </Typography>
        <Typography component="span" variant="caption" color="text.secondary" noWrap>
          {summaryOf(entry)}
        </Typography>
      </Box>
      <Box component="span" sx={{ display: "flex", flexWrap: "wrap", justifyContent: "flex-end", gap: 0.75, flexShrink: 0 }}>
        <ResourceTags entry={entry} label={label} />
      </Box>
    </RowLink>
  );
}

export function ResourcesPage() {
  const platform = usePlatformResourceTypes();
  const external = useExternalResources();
  const endpoints = useOrgEndpoints();
  const label = useProjectLabel();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ResourceFilter>("all");

  const failed = [platform, external, endpoints].filter((q) => q.isError);
  let body: ReactNode;
  if (failed.length > 0) {
    body = (
      <Alert severity="error" action={<Button onClick={() => failed.forEach((q) => void q.refetch())}>Retry</Button>}>
        {failed.map((q) => q.error?.message).join(" ")}
      </Alert>
    );
  } else if (!platform.data || !external.data || !endpoints.data) {
    body = <Skeleton variant="rounded" height={240} />;
  } else {
    const all = resourceEntries({ platform: platform.data, external: external.data, endpoints: endpoints.data });
    const rows = filterResources(all, { query, filter });
    body =
      all.length === 0 ? (
        <EmptyState
          icon={<Boxes size={48} />}
          title="Nothing to depend on yet"
          description="The platform's resource types, the services your organization registers, and what projects offer show here."
        />
      ) : rows.length === 0 ? (
        <Typography variant="body2" color="text.secondary" sx={{ py: 2 }}>
          No resources match.
        </Typography>
      ) : (
        <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
          {rows.map((entry) => {
            const { name, search } = resourceAddress(entry);
            return <Row key={`${entry.kind}:${search.project ?? ""}:${search.endpoint ?? ""}:${name}`} entry={entry} label={label} />;
          })}
        </Box>
      );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: 960 }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
            Resources
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Everything a project can depend on: what the platform provisions, the external services your organization
            registered or a project defined, and what other projects offer.
          </Typography>
        </Box>
        <ButtonLink to="/resources/new" variant="contained" startIcon={<Plus size={16} />}>
          New resource
        </ButtonLink>
      </Box>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 220, maxWidth: 420 }}>
          <SearchBar
            size="small"
            fullWidth
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search resources"
            slotProps={{ htmlInput: { "aria-label": "Search resources" } }}
          />
        </Box>
        <Segmented value={filter} options={FILTERS} onChange={setFilter} label="Kind" />
      </Box>
      {body}
    </Box>
  );
}

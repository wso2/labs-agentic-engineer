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

import { useCallback, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Alert, Box, Button, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import { CardFrame } from "../../shell/components/CardFrame";
import { useExternalResources, useOrgEndpoints, usePlatformResourceTypes } from "../api/resources";
import {
  addressedKind,
  findResource,
  promoteBlocker,
  resourceEntries,
  type ResourceEntry,
  type ResourceSearch,
} from "../model/resources";
import { PromotePanel } from "./PromotePanel";
import { RegisteredResourceCard } from "./RegisteredResourceCard";
import { ContractLine, KeysList, Quiet, Section, Text, UsedBy } from "./ResourceSections";
import { ResourceTags, useProjectLabel } from "./ResourcesPage";

// A Resource card, over the Resources Page (`/resources/$name`, the kind and
// project in its search where the name alone could collide; a new one at
// `/resources/new`). A Registered External resource is the organization's to
// change, here; a platform resource type and another project's endpoint are
// read-only, their information to read; a project's own External resource is
// read-only too, with Promote to organization, after which it is a
// registered, editable one.
//
// No Turn scope: there is no resource agent yet, so the org's chat stays as
// it is beside the card.

/** Every Resource card's frame: its name, what it is, its actions; closing goes back to Resources. */
export function ResourceFrame({
  title,
  entry,
  actions,
  children,
}: {
  title: string;
  entry: ResourceEntry | null;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const close = useCallback(() => void navigate({ to: "/resources" }), [navigate]);
  const label = useProjectLabel();
  return (
    <CardFrame
      name={entry ? `Resource ${title}` : title}
      heading={
        <Box sx={{ flex: 1, minWidth: 0, display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
          <Typography component="h2" noWrap sx={{ fontSize: "1rem", fontWeight: 600, minWidth: 0 }}>
            {title}
          </Typography>
          {entry && <ResourceTags entry={entry} label={label} />}
        </Box>
      }
      actions={actions}
      closeHint="Back to Resources"
      onClose={close}
      openKey={title}
    >
      {children}
    </CardFrame>
  );
}

export function ResourceCard({ name, search }: { name: string | null; search: ResourceSearch }) {
  const kind = name === null ? "registered" : addressedKind(search);
  const platform = usePlatformResourceTypes(kind === "platform");
  const external = useExternalResources(kind === "registered" || kind === "project");
  const endpoints = useOrgEndpoints(kind === "endpoint");

  if (name === null) return <RegisteredResourceCard key="new" saved={null} />;
  const list = kind === "platform" ? platform : kind === "endpoint" ? endpoints : external;
  if (!list.data) {
    return (
      <ResourceFrame title={name} entry={null}>
        {list.isError ? (
          <Alert severity="error" action={<Button onClick={() => void list.refetch()}>Retry</Button>}>
            {list.error?.message}
          </Alert>
        ) : (
          <Skeleton variant="rounded" height={320} />
        )}
      </ResourceFrame>
    );
  }
  const entries = resourceEntries({ platform: platform.data ?? [], external: external.data ?? [], endpoints: endpoints.data ?? [] });
  const entry = findResource(entries, name, search);
  if (!entry) {
    return (
      <ResourceFrame title={name} entry={null}>
        <Alert severity="info">
          {search.project
            ? `There is no resource ${name} in ${search.project}.`
            : `The organization has no resource ${name}.`}
        </Alert>
      </ResourceFrame>
    );
  }
  switch (entry.kind) {
    case "registered":
      // A saved version is a fresh card: its draft starts clean.
      return <RegisteredResourceCard key={JSON.stringify(entry.resource)} saved={entry.resource} />;
    case "project":
      return <ProjectResourceCard entry={entry} registeredNames={new Set(entries.filter((e) => e.kind === "registered").map((e) => e.name))} />;
    case "platform":
      return <PlatformResourceCard entry={entry} />;
    case "endpoint":
      return <EndpointCard entry={entry} />;
  }
}

function ProjectResourceCard({
  entry,
  registeredNames,
}: {
  entry: Extract<ResourceEntry, { kind: "project" }>;
  registeredNames: ReadonlySet<string>;
}) {
  const label = useProjectLabel();
  const [promoting, setPromoting] = useState(false);
  const r = entry.resource;
  const blocked = promoteBlocker(r, registeredNames);
  return (
    <ResourceFrame
      title={entry.name}
      entry={entry}
      actions={
        <Tooltip title={blocked ?? ""}>
          <span>
            <Button variant="contained" disabled={blocked !== null} onClick={() => setPromoting(true)}>
              Promote to organization
            </Button>
          </span>
        </Tooltip>
      }
    >
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: "72ch" }}>
        <Quiet>
          {label(entry.project)} defined it for itself, and holds its values. Promote it to hold them for the organization,
          so other projects can reuse it.
        </Quiet>
        {blocked && <Alert severity="info">{blocked}</Alert>}
        <Section title="Provider">{r.provider ? <Text>{r.provider}</Text> : <Quiet>Not chosen yet.</Quiet>}</Section>
        <Section title="Description">{r.description ? <Text>{r.description}</Text> : <Quiet>None.</Quiet>}</Section>
        <Section title="Contract document">
          <ContractLine contract={r.contract} />
        </Section>
        <Section title="Keys">
          <KeysList keys={r.config ?? []} />
        </Section>
        <Section title="Used by">
          <UsedBy consumers={r.consumers ?? []} label={label} />
        </Section>
      </Box>
      {promoting && <PromotePanel project={entry.project} resource={r} onClose={() => setPromoting(false)} />}
    </ResourceFrame>
  );
}

function PlatformResourceCard({ entry }: { entry: Extract<ResourceEntry, { kind: "platform" }> }) {
  const label = useProjectLabel();
  const r = entry.resource;
  const parameters = Object.entries(r.parameters ?? {});
  return (
    <ResourceFrame title={entry.name} entry={entry}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: "72ch" }}>
        <Quiet>The platform provisions it for a project that depends on it, once a person approves.</Quiet>
        {r.description && <Text>{r.description}</Text>}
        <Section title="What a project sets">
          {parameters.length === 0 ? (
            <Quiet>Nothing.</Quiet>
          ) : (
            <Box component="ul" sx={{ m: 0, pl: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 0.75 }}>
              {parameters.map(([key, schema]) => {
                const info = schema && typeof schema === "object" ? (schema as { type?: unknown; description?: unknown }) : {};
                return (
                  <Box component="li" key={key} sx={{ display: "flex", alignItems: "baseline", gap: 1, flexWrap: "wrap" }}>
                    <Typography component="code" variant="body2" sx={{ fontFamily: "monospace" }}>
                      {key}
                    </Typography>
                    {typeof info.type === "string" && (
                      <Typography variant="caption" color="text.secondary">
                        {info.type}
                      </Typography>
                    )}
                    {typeof info.description === "string" && (
                      <Typography variant="caption" color="text.secondary">
                        {info.description}
                      </Typography>
                    )}
                  </Box>
                );
              })}
            </Box>
          )}
        </Section>
        <Section title="What a component reads">
          {(r.outputs ?? []).length === 0 ? (
            <Quiet>Nothing.</Quiet>
          ) : (
            <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
              {(r.outputs ?? []).join(", ")}
            </Typography>
          )}
        </Section>
        <Section title="Used by">
          <UsedBy consumers={r.consumers ?? []} label={label} />
        </Section>
      </Box>
    </ResourceFrame>
  );
}

function EndpointCard({ entry }: { entry: Extract<ResourceEntry, { kind: "endpoint" }> }) {
  const label = useProjectLabel();
  const r = entry.resource;
  return (
    <ResourceFrame title={entry.name} entry={entry}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: "72ch" }}>
        <Quiet>
          {label(entry.project)} offers it to the organization. A project depends on it by its name; {label(entry.project)} changes it.
        </Quiet>
        <Section title="Endpoint">
          <Text>
            {r.endpoint} · {r.type}
          </Text>
        </Section>
        <Section title="Offered by">
          <UsedBy consumers={[{ projectId: entry.project, componentName: entry.name }]} label={label} />
        </Section>
      </Box>
    </ResourceFrame>
  );
}

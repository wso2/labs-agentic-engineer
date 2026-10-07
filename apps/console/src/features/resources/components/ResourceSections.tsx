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
import { createLink } from "@tanstack/react-router";
import { Box, Link, Typography } from "@wso2/oxygen-ui";
import { Lock } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { Tag } from "../../spec/components/Tag";

// The read-only parts of a Resource card, shared by its kinds: a titled
// section, a resource's keys, its contract document, its docs, and who uses
// it. After the old console's resource-inspect-sections, restyled.

type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type ConsumerDTO = components["schemas"]["ConsumerDTO"];
type ResourceContract = components["schemas"]["ResourceContract"];
type ResourceDocPointerDTO = components["schemas"]["ResourceDocPointerDTO"];

const ProjectLink = createLink(Link);

export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography component="h3" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
        {title}
      </Typography>
      {children}
    </Box>
  );
}

export function Quiet({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" color="text.secondary">
      {children}
    </Typography>
  );
}

export function Text({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
      {children}
    </Typography>
  );
}

export function KeysList({ keys }: { keys: readonly ConfigKeyDTO[] }) {
  if (keys.length === 0) return <Quiet>None.</Quiet>;
  return (
    <Box component="ul" sx={{ m: 0, pl: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 0.75 }}>
      {keys.map((k) => (
        <Box component="li" key={k.key} sx={{ display: "flex", alignItems: "baseline", gap: 1 }}>
          <Typography component="code" variant="body2" sx={{ fontFamily: "monospace", display: "inline-flex", alignItems: "center", gap: 0.5 }}>
            {k.secret && <Lock size={12} aria-label="Secret" />}
            {k.key}
          </Typography>
          {k.description && (
            <Typography variant="caption" color="text.secondary">
              {k.description}
            </Typography>
          )}
        </Box>
      ))}
    </Box>
  );
}

const CONTRACT_LABEL: Record<ResourceContract["type"], string> = {
  openapi: "OpenAPI",
  graphql: "GraphQL",
  sdk: "SDK",
  asyncapi: "AsyncAPI",
  protobuf: "Protobuf",
  documentation: "Documentation",
};

export function contractLabel(type: ResourceContract["type"]): string {
  return CONTRACT_LABEL[type];
}

/** The contract document on file: a path in the organization's (or the project's) store, a fact rather than a link. */
export function ContractLine({ contract }: { contract: ResourceContract | undefined }) {
  if (!contract) return <Quiet>No contract document yet.</Quiet>;
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
      <Tag tone={null}>{CONTRACT_LABEL[contract.type]}</Tag>
      <Typography component="code" variant="body2" sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>
        {contract.path}
      </Typography>
      {contract.origin && (
        <Typography variant="caption" color="text.secondary">
          {contract.origin === "assumed" ? "assumed by the design agent" : contract.origin === "derived" ? "derived from the provider's reference" : `from the ${contract.origin}`}
        </Typography>
      )}
    </Box>
  );
}

export function DocsList({ docs }: { docs: readonly ResourceDocPointerDTO[] }) {
  return (
    <Box component="ul" sx={{ m: 0, pl: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 0.5 }}>
      {docs.map((d) => (
        <Box component="li" key={`${d.type}:${d.url ?? d.path ?? ""}`} sx={{ display: "flex", alignItems: "center", gap: 1, minWidth: 0 }}>
          <Tag tone={null}>{d.type}</Tag>
          {d.url ? (
            <Link href={d.url} target="_blank" rel="noreferrer" variant="body2" sx={{ wordBreak: "break-all" }}>
              {d.url}
            </Link>
          ) : (
            <Typography component="code" variant="body2" sx={{ fontFamily: "monospace" }}>
              {d.path}
            </Typography>
          )}
        </Box>
      ))}
    </Box>
  );
}

/** The components that use it, each linking to its project. */
export function UsedBy({ consumers, label }: { consumers: readonly ConsumerDTO[]; label: (slug: string) => string }) {
  if (consumers.length === 0) return <Quiet>No component uses it yet.</Quiet>;
  return (
    <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
      {consumers.map((c) => (
        <li key={`${c.projectId}/${c.componentName}`}>
          <Typography variant="body2">
            {c.componentName} in{" "}
            <ProjectLink to="/projects/$projectName" params={{ projectName: c.projectId }}>
              {label(c.projectId)}
            </ProjectLink>
          </Typography>
        </li>
      ))}
    </Box>
  );
}

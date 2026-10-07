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

import type { components } from "../../../generated/aep-api";

// Everything a project can depend on, as the Resources Page lists it and a
// Resource card finds it: the platform's resource types, the organization's
// Registered External resources, the External resources projects defined for
// themselves (each held by its project), and what other projects offer
// (their endpoints). Names collide across these (two projects may each hold
// a resource of one name, and an endpoint is named for its component), so a
// card's address carries the kind and the project where the name alone is
// not enough (`resourceAddress`).

type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type OrgEndpointDTO = components["schemas"]["OrgEndpointDTO"];

export type ResourceEntry =
  | { kind: "platform"; name: string; resource: PlatformResourceTypeDTO }
  | { kind: "registered"; name: string; resource: ExternalResourceDTO }
  | { kind: "project"; name: string; project: string; resource: ExternalResourceDTO }
  | { kind: "endpoint"; name: string; project: string; resource: OrgEndpointDTO };

export type ResourceKind = ResourceEntry["kind"];

/** The Page's filter: Platform, External (registered and project-held), From projects (endpoints). */
export type ResourceFilter = "all" | "platform" | "external" | "endpoints";

const FILTER_KINDS: Record<Exclude<ResourceFilter, "all">, readonly ResourceKind[]> = {
  platform: ["platform"],
  external: ["registered", "project"],
  endpoints: ["endpoint"],
};

/**
 * Whether an external resource is the organization's record. `scope` says so;
 * a response that predates it is told by its org value plane (envCells),
 * which only a registered resource has.
 */
export function isRegistered(resource: ExternalResourceDTO): boolean {
  if (resource.scope) return resource.scope === "org";
  return (resource.envCells ?? []).length > 0;
}

/** One list of everything, by name. */
export function resourceEntries(lists: {
  platform: readonly PlatformResourceTypeDTO[];
  external: readonly ExternalResourceDTO[];
  endpoints: readonly OrgEndpointDTO[];
}): ResourceEntry[] {
  const entries: ResourceEntry[] = [
    ...lists.platform.map((resource) => ({ kind: "platform" as const, name: resource.name, resource })),
    ...lists.external.map((resource): ResourceEntry =>
      isRegistered(resource) || !resource.project
        ? { kind: "registered", name: resource.name, resource }
        : { kind: "project", name: resource.name, project: resource.project, resource },
    ),
    ...lists.endpoints.map((resource) => ({ kind: "endpoint" as const, name: resource.name, project: resource.project, resource })),
  ];
  return entries.sort((a, b) => a.name.localeCompare(b.name) || projectOf(a).localeCompare(projectOf(b)));
}

function projectOf(entry: ResourceEntry): string {
  return entry.kind === "project" || entry.kind === "endpoint" ? entry.project : "";
}

function searchable(entry: ResourceEntry): string[] {
  switch (entry.kind) {
    case "platform":
      return [entry.name, entry.resource.description ?? ""];
    case "registered":
      return [entry.name, entry.resource.provider ?? "", entry.resource.description ?? ""];
    case "project":
      return [entry.name, entry.project, entry.resource.provider ?? "", entry.resource.description ?? ""];
    case "endpoint":
      return [entry.name, entry.project, entry.resource.endpoint, entry.resource.type];
  }
}

/** The entries of the chosen kind whose name, provider, description or project holds the search. */
export function filterResources(
  entries: readonly ResourceEntry[],
  { query, filter }: { query: string; filter: ResourceFilter },
): ResourceEntry[] {
  const q = query.trim().toLowerCase();
  return entries
    .filter((e) => filter === "all" || FILTER_KINDS[filter].includes(e.kind))
    .filter((e) => !q || searchable(e).some((field) => field.toLowerCase().includes(q)));
}

/**
 * The part of a Resource card's address after its name. A registered
 * resource needs none (the organization's names are unique); a project's own
 * resource names its project; a platform type and an endpoint name their
 * kind, and an endpoint its project and endpoint.
 */
export interface ResourceSearch {
  kind?: "platform" | "endpoint";
  project?: string;
  endpoint?: string;
}

export function resourceAddress(entry: ResourceEntry): { name: string; search: ResourceSearch } {
  switch (entry.kind) {
    case "platform":
      return { name: entry.name, search: { kind: "platform" } };
    case "registered":
      return { name: entry.name, search: {} };
    case "project":
      return { name: entry.name, search: { project: entry.project } };
    case "endpoint":
      return { name: entry.name, search: { kind: "endpoint", project: entry.project, endpoint: entry.resource.endpoint } };
  }
}

/** The address's search as the route reads it: unknown values are dropped. */
export function resourceSearch(raw: Record<string, unknown>): ResourceSearch {
  const text = (v: unknown) => (typeof v === "string" && v !== "" ? v : undefined);
  const kind = raw.kind === "platform" || raw.kind === "endpoint" ? raw.kind : undefined;
  const project = text(raw.project);
  const endpoint = text(raw.endpoint);
  return { ...(kind ? { kind } : {}), ...(project ? { project } : {}), ...(endpoint ? { endpoint } : {}) };
}

/** Which kind of resource an address names, before any list is read. */
export function addressedKind(search: ResourceSearch): ResourceKind {
  if (search.kind) return search.kind;
  return search.project ? "project" : "registered";
}

/** The entry an address names, among the entries of its kind. */
export function findResource(
  entries: readonly ResourceEntry[],
  name: string,
  search: ResourceSearch,
): ResourceEntry | undefined {
  const kind = addressedKind(search);
  return entries.find((e) => {
    if (e.kind !== kind || e.name !== name) return false;
    if (e.kind === "project") return e.project === search.project;
    if (e.kind === "endpoint") return e.project === search.project && (!search.endpoint || e.resource.endpoint === search.endpoint);
    return true;
  });
}

/**
 * Why a project's own resource cannot be promoted to the organization, or
 * null when it can. The platform takes the record from the project's copy,
 * so the copy must name its provider and its keys; and the organization's
 * names are unique, so one it already registered cannot be promoted again
 * (the project reuses that one instead).
 */
export function promoteBlocker(resource: ExternalResourceDTO, registeredNames: ReadonlySet<string>): string | null {
  if (registeredNames.has(resource.name)) {
    return "The organization already registered a resource of this name; the project can reuse it instead.";
  }
  if (!resource.provider?.trim()) return "The project has not chosen its provider yet.";
  if ((resource.config ?? []).length === 0) return "It declares no keys, so the organization has no values to hold.";
  return null;
}

/** Why a registered resource cannot be deleted, or null when it can: one a component uses stays. */
export function deleteBlocker(resource: ExternalResourceDTO): string | null {
  const used = (resource.consumers ?? []).length;
  if (used === 0) return null;
  return `${used === 1 ? "A component uses" : `${used} components use`} it. Remove those dependencies first.`;
}

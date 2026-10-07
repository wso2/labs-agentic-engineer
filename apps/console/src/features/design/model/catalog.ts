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

import type { ScopeRoles } from "@aep/ui-openapi-view";
import type { DesignArtifact } from "../api/designModel";

// What a design yields, worked out from its files in the room (E4): one
// catalog, each artifact tagged business or technical, covering the features
// whose stories it serves. Pure, so an agent's write shows on the next render.
//
//   Business   the prototype of each web app (its wireframes), each key flow,
//              who can do what (security.json's roles), the data model, and
//              each feature's acceptance criteria;
//   Technical  the architecture (design.cell), each component and its
//              contract (with the organization's resources it reuses), and
//              the permissions it guards (security.json).
//
// An API-only product has no web app, so no prototype. The files are the
// design skill's (skills/design): flows and the domain model are markdown
// around one mermaid diagram, drawn as they are.

const COMPONENT_RE = /^specs\/design\/components\/([^/]+)\/design\.json$/;
const FLOW_RE = /^specs\/design\/flows\/([^/]+)\.md$/;
const ACCEPTANCE_RE = /^specs\/validation\/acceptance\/[^/]+\.feature$/;
const FEATURE_LINE = /^\s*Feature:\s*(F\d+)\s+(.+)$/m;
const STORY_RE = /\bF(\d+)\.\d+\b/g;
const HEADING = /^#\s+(.+)$/m;
const CELL_TITLE = /^\s*title\s+(.+)$/m;

interface ComponentFacts {
  name: string;
  type: string;
  features: string[];
}

interface SecurityDoc {
  permissions?: { resource: string; component?: string; description?: string; actions?: { handle: string; description?: string }[] }[];
  roles?: { name: string; description?: string; grants?: string[]; assignTo?: string[]; stories?: string[] }[];
}

function featuresOfStories(stories: readonly string[]): string[] {
  return [...new Set(stories.flatMap((s) => /^(F\d+)\./.exec(s)?.[1] ?? []))].sort(byId);
}

function byId(a: string, b: string): number {
  return Number(a.slice(1)) - Number(b.slice(1));
}

function parseJson<T>(text: string | undefined): T | null {
  if (!text) return null;
  try {
    return JSON.parse(text) as T;
  } catch {
    return null;
  }
}

/**
 * The Registered External resources a component reuses: each `external`
 * dependency is defined once, in its own dependency.json, and one whose
 * resource carries a `ref` is a copy of the organization's record of that
 * name (CONTEXT.md, "Registered External resource").
 */
export function registeredResourcesOf(files: Readonly<Record<string, string>>, design: string | undefined): string[] {
  const deps = parseJson<{ dependencies?: { kind?: string; name?: string }[] }>(design)?.dependencies ?? [];
  const refs = deps.flatMap((d) => {
    if (d.kind !== "external" || !d.name) return [];
    const definition = parseJson<{ resource?: { ref?: string } }>(files[`specs/design/dependencies/${d.name}/dependency.json`]);
    return definition?.resource?.ref ? [definition.resource.ref] : [];
  });
  return [...new Set(refs)].sort();
}

function componentsOf(files: Readonly<Record<string, string>>): ComponentFacts[] {
  return Object.keys(files)
    .flatMap((path) => {
      const m = COMPONENT_RE.exec(path);
      const design = parseJson<{ name?: string; type?: string; stories?: string[] }>(files[path]);
      if (!m || !design) return [];
      return [{ name: design.name ?? m[1]!, type: design.type ?? "", features: featuresOfStories(design.stories ?? []) }];
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** Who can do what: each role's grants, one row per action a resource offers. */
function rolesOf(security: SecurityDoc): { roles: string[]; rows: { action: string; grants: boolean[] }[]; note: string } {
  const roles = security.roles ?? [];
  const rows = (security.permissions ?? []).flatMap((p) =>
    (p.actions ?? []).map((a) => ({
      action: a.description ?? `${p.resource}:${a.handle}`,
      grants: roles.map((r) => (r.grants ?? []).includes(`${p.resource}:${a.handle}`)),
    })),
  );
  const assigned = roles.filter((r) => r.assignTo?.length).map((r) => `${r.name} → ${r.assignTo!.join(", ")}`);
  return { roles: roles.map((r) => r.name), rows, note: assigned.length ? `Assigned through groups: ${assigned.join("; ")}.` : "" };
}

/** Which roles hold each scope ("claims:submit"), for the contract's view of who may call what. */
function scopeRolesOf(security: SecurityDoc): ScopeRoles {
  const out: Record<string, string[]> = {};
  for (const role of security.roles ?? []) {
    for (const grant of role.grants ?? []) (out[grant] ??= []).push(role.name);
  }
  return out;
}

export function designCatalog(files: Readonly<Record<string, string>>): DesignArtifact[] {
  const components = componentsOf(files);
  const all = [...new Set(components.flatMap((c) => c.features))].sort(byId);
  const security = parseJson<SecurityDoc>(files["specs/design/security.json"]);
  const at = { addedIn: 0, changedIn: 0 };
  const out: DesignArtifact[] = [];

  for (const c of components) {
    const path = `specs/design/components/${c.name}/wireframes.dsl`;
    const dsl = files[path];
    if (c.type === "web-application" && dsl) {
      out.push({ id: `prototype-${c.name}`, title: `Wireframes: ${c.name}`, depth: "business", features: c.features, ...at, source: { kind: "prototype", path, dsl } });
    }
  }
  for (const path of Object.keys(files).sort()) {
    const m = FLOW_RE.exec(path);
    if (!m) continue;
    const markdown = files[path]!;
    const named = featuresOfStories([...markdown.matchAll(STORY_RE)].map((s) => s[0]));
    out.push({
      id: `flow-${m[1]}`,
      title: `Flow: ${HEADING.exec(markdown)?.[1]?.trim() ?? m[1]}`,
      depth: "business",
      features: named.length ? named : all,
      ...at,
      source: { kind: "document", path, markdown },
    });
  }
  if (security?.roles?.length) {
    out.push({ id: "roles", title: "Roles: who can do what", depth: "business", features: all, ...at, source: { kind: "roles", ...rolesOf(security) } });
  }
  const domain = files["specs/design/domain-model.md"];
  if (domain) {
    out.push({ id: "data", title: "Data model", depth: "business", features: all, ...at, source: { kind: "document", path: "specs/design/domain-model.md", markdown: domain } });
  }
  const cell = files["specs/design/design.cell"];
  if (cell) {
    const title = CELL_TITLE.exec(cell)?.[1]?.trim();
    out.push({ id: "architecture", title: title ? `Architecture: ${title}` : "Architecture", depth: "technical", features: all, ...at, source: { kind: "architecture", cell } });
  }
  const scopeRoles = security ? scopeRolesOf(security) : undefined;
  for (const c of components) {
    out.push({
      id: c.name,
      title: `Component and contract: ${c.name}`,
      depth: "technical",
      features: c.features.length ? c.features : all,
      ...at,
      source: {
        kind: "contract",
        design: files[`specs/design/components/${c.name}/design.json`]!,
        openapi: files[`specs/design/components/${c.name}/openapi.yaml`] ?? null,
        ...(scopeRoles ? { roles: scopeRoles } : {}),
        resources: registeredResourcesOf(files, files[`specs/design/components/${c.name}/design.json`]),
      },
    });
  }
  if (security?.permissions?.length) {
    const rows = security.permissions.map((p) => ({
      subject: p.component ? `${p.resource} (${p.component})` : p.resource,
      rule: [p.description, (p.actions ?? []).map((a) => a.handle).join(", ")].filter(Boolean).join(" — "),
    }));
    out.push({ id: "security", title: "Security: sign-in and grants", depth: "technical", features: all, ...at, source: { kind: "security", rows } });
  }
  for (const path of Object.keys(files).sort()) {
    if (!ACCEPTANCE_RE.test(path)) continue;
    const content = files[path]!;
    const feature = FEATURE_LINE.exec(content);
    out.push({
      id: `acceptance-${feature?.[1] ?? path}`,
      title: feature ? `Acceptance: ${feature[1]} ${feature[2]!.trim()}` : `Acceptance: ${path.split("/").pop()}`,
      depth: "business",
      features: feature ? [feature[1]!] : all,
      ...at,
      source: { kind: "acceptance", path, content },
    });
  }
  return out;
}

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

import { prototypeHash } from "@wso2/prototype-kit/feedback";
import { sha256Text } from "../../../lib/sha256";
import type { ArtifactSource } from "../api/designModel";
import {
  componentsOf,
  parseJson,
  registeredResourcesOf,
  rolesOf,
  scopeRolesOf,
  securityRowsOf,
  type SecurityDoc,
} from "./catalog";
import { keptMarkdown } from "../decisionsRoom";
import { DECISIONS_PATH } from "./decisions";

// The Design card's rail (L21, D12): the files Design generates, read from the
// room, one item per file and in the order Design makes them:
//
//   Architecture · Data model · Roles and permissions · each component, its
//   artifact under it (a web app's Prototype, a service's Contract, an
//   agent's definition) · Dependencies · Flows · Technical decisions ·
//   Acceptance tests, one per feature.
//
// An item shows only once its file exists. Each names the path review.json
// keys it by (the prototype by its manifest), what its Checked hash is taken
// of (the technical decisions' as they read once every one is kept, ADR-0006), and
// which Overview list reviews it: a business reviewer's (prototypes,
// agents, roles and permissions) or a technical one's (architecture, data
// model, contracts, flows, technical decisions). Only those are checkable;
// components, dependencies and acceptance tests are for reference.
// Pure, so an agent's write shows on the next render.

const DESIGN = "specs/design/";
const COMPONENTS = `${DESIGN}components/`;
const DEPENDENCY_RE = /^specs\/design\/dependencies\/([^/]+)\/dependency\.json$/;
const FLOW_RE = /^specs\/design\/flows\/([^/]+)\.md$/;
const ACCEPTANCE_RE = /^specs\/validation\/acceptance\/[^/]+\.feature$/;
const FEATURE_LINE = /^\s*Feature:\s*(F\d+)\s+(.+)$/m;
const HEADING = /^#\s+(.+)$/m;

/** The room's prefixes the rail reads. */
export const DESIGN_FILE_PREFIXES = [DESIGN, "specs/validation/acceptance/"] as const;


export type ReviewList = "business" | "technical";

type Source<K extends ArtifactSource["kind"]> = Extract<ArtifactSource, { kind: K }>;

/** What an item's pane draws, with the console's existing renderers. */
export type ItemBody =
  | { kind: "architecture"; source: Source<"architecture"> }
  | { kind: "document"; markdown: string }
  /** The technical decisions: drawn from the room's lines, which Keep edits. */
  | { kind: "decisions" }
  | { kind: "roles"; roles: Source<"roles">; security: Source<"security"> }
  /** A component's design.json, or a service's with its contract. */
  | { kind: "contract"; source: Source<"contract"> }
  | { kind: "prototype"; component: string }
  /** An ai-agent's agent.afm.md, as written. */
  | { kind: "agent"; afm: string }
  /** An external dependency: what it is, then its assumptions. */
  | { kind: "dependency"; name: string; markdown: string }
  | { kind: "acceptance"; path: string; content: string };

/** What an item's Checked hash is taken of: the file's text, or the prototype's two files. */
export type HashInput = { text: string } | { manifest: string; source: string };

export interface DesignItem {
  /** Its key in the card's URL (`?art=`). */
  key: string;
  /** The path review.json keys it by. */
  path: string;
  /** Its name on its own, as the rail lists it. */
  label: string;
  /** Its full name, on its title line and in the Overview. */
  title: string;
  /** The rail's section heading it sits under, if any. */
  section: "Dependencies" | "Flows" | "Acceptance tests" | null;
  /** Indented under the component row before it. */
  nested: boolean;
  /** A component name, set in monospace. */
  mono: boolean;
  list: ReviewList | null;
  checkable: boolean;
  hashOf: HashInput;
  body: ItemBody;
}

/** A web application first, then services, then agents, then any other kind; by name within each. */
const KIND_ORDER = ["web-application", "service", "ai-agent"];

function kindRank(type: string): number {
  const i = KIND_ORDER.indexOf(type);
  return i < 0 ? KIND_ORDER.length : i;
}

function item(fields: Omit<DesignItem, "section" | "nested" | "mono" | "list" | "checkable"> & Partial<DesignItem>): DesignItem {
  const list = fields.list ?? null;
  return { section: null, nested: false, mono: false, checkable: list !== null, ...fields, list };
}

function documentItem(key: string, path: string, label: string, text: string, list: ReviewList | null, extra: Partial<DesignItem> = {}): DesignItem {
  return item({ key, path, label, title: label, list, hashOf: { text }, body: { kind: "document", markdown: text }, ...extra });
}

/** A dependency as the rail shows it: its name and description, its provider and config keys, then its assumptions. */
function dependencyMarkdown(name: string, json: string, assumptions: string | undefined): string {
  const definition = parseJson<{
    resource?: { name?: string; description?: string; provider?: string; ref?: string; config?: { key: string; secret?: boolean; description?: string }[] };
  }>(json);
  const r = definition?.resource;
  const lines = [`# ${r?.name ?? name}`, ""];
  if (r?.description) lines.push(r.description, "");
  lines.push(
    r?.ref
      ? `- Uses the organization's \`${r.ref}\``
      : r?.provider
        ? `- Provider: ${r.provider}`
        : "- Provider: not chosen yet",
  );
  for (const k of r?.config ?? []) lines.push(`- Config \`${k.key}\`${k.secret ? " (secret)" : ""}${k.description ? `: ${k.description}` : ""}`);
  return assumptions ? `${lines.join("\n")}\n\n${assumptions}` : lines.join("\n");
}

/** The rail's items, in generation order, for the room's files under DESIGN_FILE_PREFIXES. */
export function designItems(files: Readonly<Record<string, string>>): DesignItem[] {
  const out: DesignItem[] = [];
  const security = parseJson<SecurityDoc>(files[`${DESIGN}security.json`]);

  const cell = files[`${DESIGN}design.cell`];
  if (cell !== undefined) {
    out.push(item({ key: "architecture", path: `${DESIGN}design.cell`, label: "Architecture", title: "Architecture", list: "technical", hashOf: { text: cell }, body: { kind: "architecture", source: { kind: "architecture", cell } } }));
  }
  const domain = files[`${DESIGN}domain-model.md`];
  if (domain !== undefined) out.push(documentItem("data-model", `${DESIGN}domain-model.md`, "Data model", domain, "technical"));
  const securityText = files[`${DESIGN}security.json`];
  if (security && securityText !== undefined) {
    out.push(
      item({
        key: "roles",
        path: `${DESIGN}security.json`,
        label: "Roles and permissions",
        title: "Roles and permissions",
        list: "business",
        hashOf: { text: securityText },
        body: { kind: "roles", roles: { kind: "roles", ...rolesOf(security) }, security: { kind: "security", rows: securityRowsOf(security) } },
      }),
    );
  }

  const scopeRoles = security ? scopeRolesOf(security) : undefined;
  const components = [...componentsOf(files)].sort((a, b) => kindRank(a.type) - kindRank(b.type) || a.name.localeCompare(b.name));
  for (const c of components) {
    const dir = `${COMPONENTS}${c.name}/`;
    const design = files[`${dir}design.json`]!;
    const resources = registeredResourcesOf(files, design);
    out.push(
      item({
        key: `component-${c.name}`,
        path: `${dir}design.json`,
        label: c.name,
        title: c.name,
        mono: true,
        hashOf: { text: design },
        body: { kind: "contract", source: { kind: "contract", design, openapi: null, resources } },
      }),
    );
    const manifest = files[`${dir}prototype.json`];
    const openapi = files[`${dir}openapi.yaml`];
    const agent = files[`${dir}agent.afm.md`];
    if (c.type === "web-application" && manifest !== undefined) {
      out.push(
        item({
          key: `prototype-${c.name}`,
          path: `${dir}prototype.json`,
          label: "Prototype",
          title: `${c.name} › Prototype`,
          nested: true,
          list: "business",
          hashOf: { manifest, source: files[`${dir}prototype.tsx`] ?? "" },
          body: { kind: "prototype", component: c.name },
        }),
      );
    } else if (openapi !== undefined) {
      out.push(
        item({
          key: `contract-${c.name}`,
          path: `${dir}openapi.yaml`,
          label: "Contract",
          title: `${c.name} › Contract`,
          nested: true,
          list: "technical",
          hashOf: { text: openapi },
          body: { kind: "contract", source: { kind: "contract", design, openapi, ...(scopeRoles ? { roles: scopeRoles } : {}), resources } },
        }),
      );
    } else if (agent !== undefined) {
      out.push(
        item({
          key: `agent-${c.name}`,
          path: `${dir}agent.afm.md`,
          label: "Agent",
          title: `${c.name} › Agent`,
          nested: true,
          list: "business",
          hashOf: { text: agent },
          body: { kind: "agent", afm: agent },
        }),
      );
    }
  }

  for (const path of Object.keys(files).sort()) {
    const m = DEPENDENCY_RE.exec(path);
    if (!m) continue;
    const name = m[1]!;
    const json = files[path]!;
    out.push(
      item({
        key: `dependency-${name}`,
        path,
        label: name,
        title: `Dependency: ${name}`,
        section: "Dependencies",
        nested: true,
        hashOf: { text: json },
        body: { kind: "dependency", name, markdown: dependencyMarkdown(name, json, files[`${DESIGN}dependencies/${name}/assumptions.md`]) },
      }),
    );
  }
  for (const path of Object.keys(files).sort()) {
    const m = FLOW_RE.exec(path);
    if (!m) continue;
    const markdown = files[path]!;
    const name = HEADING.exec(markdown)?.[1]?.trim() ?? m[1]!;
    out.push(documentItem(`flow-${m[1]}`, path, name, markdown, "technical", { title: `Flow: ${name}`, section: "Flows", nested: true }));
  }
  const decisions = files[DECISIONS_PATH];
  if (decisions !== undefined) {
    out.push(
      item({
        key: "decisions",
        path: DECISIONS_PATH,
        label: "Technical decisions",
        title: "Technical decisions",
        list: "technical",
        hashOf: { text: keptMarkdown(decisions) },
        body: { kind: "decisions" },
      }),
    );
  }

  for (const path of Object.keys(files).sort()) {
    if (!ACCEPTANCE_RE.test(path)) continue;
    const content = files[path]!;
    const feature = FEATURE_LINE.exec(content);
    const label = feature ? `${feature[1]} ${feature[2]!.trim()}` : path.split("/").pop()!;
    out.push(
      item({
        key: `acceptance-${feature?.[1] ?? path.split("/").pop()}`,
        path,
        label,
        title: `Acceptance tests: ${label}`,
        section: "Acceptance tests",
        nested: true,
        hashOf: { text: content },
        body: { kind: "acceptance", path, content },
      }),
    );
  }
  return out;
}

/** Each Overview review list, in its own order: the business one (prototypes, agents, then roles), the technical one (as generated). */
export function reviewLists(items: readonly DesignItem[]): Record<ReviewList, DesignItem[]> {
  const business = items.filter((i) => i.list === "business");
  const rank = (i: DesignItem) => (i.body.kind === "prototype" ? 0 : i.body.kind === "agent" ? 1 : 2);
  return {
    business: [...business].sort((a, b) => rank(a) - rank(b)),
    technical: items.filter((i) => i.list === "technical"),
  };
}

/**
 * Each item's hash as review.json's Checked compares it, by path: the
 * lowercase hex SHA-256 of the UTF-8 text its `hashOf` names, or for a
 * prototype the kit's revision hash over its two files.
 */
export function artifactHashes(items: readonly DesignItem[]): Record<string, string> {
  return Object.fromEntries(
    items.map((i) => [i.path, "text" in i.hashOf ? sha256Text(i.hashOf.text) : prototypeHash(i.hashOf.manifest, i.hashOf.source)]),
  );
}

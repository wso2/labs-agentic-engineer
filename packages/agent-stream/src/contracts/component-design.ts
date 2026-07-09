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

/**
 * ComponentDesign — the AUTHORED per-component design file at
 * `specs/design/components/<name>/design.json`. This replaces the
 * component-level design.md: the spec agent writes it (whole-file rewrites,
 * schema-validated by the FileBundle on every write), downstream consumers
 * (design projection, coding-agent dispatch, task generation) read it
 * directly. The TOP-LEVEL design.md (prose + skillsApplied frontmatter) is
 * unchanged. The Zod validator (`componentDesignSchema` in
 * `../component-design-schema.ts`) is drift-guarded against this type.
 */

export interface ComponentDesign {
  /** Must equal the component's directory name (kebab-case). */
  name: string;
  /**
   * Component kind. "service" and "web-application" carry full platform
   * conventions (openapi.yaml / wireframes.dsl and deployment support) and
   * mirror OpenChoreo's own terms (deployment/service,
   * deployment/web-application — the same words minus the prefix); any other kind
   * the requirements imply (e.g. "scheduled-task", "worker") is CAPTURED at
   * design time — support-gating happens in later phases, not here.
   */
  type: string;
  /** Semantic version; "0.1.0" for a new component. */
  version: string;
  /** Implementation language, e.g. "Go", "TypeScript". */
  language: string;
  /** Always "docker" today. */
  buildpack: string;
  /** Repo-relative source dir — the component name. */
  appPath: string;
  /** Deploy entry, e.g. "deployment/service". */
  entrypoint: string;
  /** Gateway exposure of the component's endpoint. */
  exposure: "internet" | "intranet";
  /**
   * Unified, kind-discriminated dependency edges — the successor to the
   * legacy `connections[]`. Mirrors the aep-api Go `models.Dependency` MINUS
   * `status`/`reason` (those are PLATFORM-COMPUTED at read time against the
   * live catalog — never authored, presence of either is a schema violation).
   */
  dependencies: Dependency[];
  /** The single-responsibility paragraph (what it does / does NOT do). */
  description: string;
  /**
   * PLATFORM-OWNED (optional). Managed-API exposure policy for a service, set
   * by the platform — the agent must NOT invent it. Round-trips through the
   * file untouched. Mirrors Go `models.ExposesAPI`.
   */
  exposesAPI?: ExposesAPI;
  /**
   * PLATFORM-OWNED (optional). Extra instructions the platform injects for the
   * downstream coding agent. Passthrough — the design agent must not author it.
   */
  componentAgentInstructions?: string;
}

/** The closed set of dependency kinds (mirrors Go `models.DependencyKind`). */
export type DependencyKind = "component" | "org-service" | "external" | "platform-resource";

/**
 * One unified dependency edge. A single flat shape carries every kind's
 * fields; `kind` selects which are meaningful — mirroring the Go codec, which
 * uses one struct and is LENIENT about kind-specific fields (it does not
 * reject, e.g., `resourceType` on an `external` dep). Only `kind` (closed set)
 * and `name` are required; every other field is optional. `status`/`reason`
 * are deliberately ABSENT — they are read-time computed, never authored.
 */
export interface Dependency {
  kind: DependencyKind;
  /** Sibling component / org-service provider / external system / resource name. */
  name: string;
  description?: string;
  /** external (REST/GraphQL): the agent must call specific endpoints ⇒ a spec is needed. */
  needsSpec?: boolean;
  /** external: stored contract path, component-relative (dependencies/<name>.openapi.yaml). */
  specPath?: string;
  /** external: transient published-OpenAPI hint auto-fetched at save then cleared. */
  specUrl?: string;
  /** external: the config-key schema the consuming component codes against. */
  config?: ConfigKey[];
  /** platform-resource: the registered (Cluster)ResourceType. */
  resourceType?: string;
  /** platform-resource: provisioning parameters. */
  parameters?: Record<string, string>;
  /** resolution UI: options attached when the platform marks a dep ambiguous. */
  candidates?: DependencyCandidate[];
}

/** One env-var key a component reads at runtime. Mirrors Go `models.ConfigKey`. */
export interface ConfigKey {
  key: string;
  /** Secret keys route through the secret path. Default: false. */
  secret?: boolean;
  /** publishable | secret. */
  credentialClass?: string;
}

/** One option attached to an ambiguous dependency. Mirrors Go `models.DependencyCandidate`. */
export interface DependencyCandidate {
  label: string;
  description?: string;
  url?: string;
}

/** Managed-API exposure policy (platform-owned). Mirrors Go `models.ExposesAPI`. */
export interface ExposesAPI {
  managed?: boolean;
  /** "end-user-required" | "service-required" | "none". */
  auth?: string;
  /** injected header name, e.g. "X-User-Id". */
  userContext?: string;
  /** endpoint consumable by OTHER projects in the org. */
  orgPublished?: boolean;
}

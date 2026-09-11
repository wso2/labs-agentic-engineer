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
 * directly. `skillsPinned` is a key HERE (per-component), NOT in design.md
 * frontmatter; the root design.cell carries an optional `sourceSpec`
 * frontmatter only. The Zod validator (`componentDesignSchema` in
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
  /**
   * The build buildpack — always "docker" (the platform's single build path).
   * The agent write-gate (checkComponentDesign) pins this to "docker" as a
   * post-parse check, so the type stays `string` and the shared JSON Schema /
   * BFF save-gate stay permissive.
   */
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
   * Optional. The single network endpoint the component exposes. Its `name`
   * is the SINGLE SOURCE OF TRUTH for the endpoint name: the coding agent
   * writes the same name into `workload.yaml` (`spec.endpoints[].name`) and
   * the platform's managed-API (`api-configuration`) trait binds to it. When
   * omitted, both sides default to the conventional name `"http"`. The port
   * is NOT declared here — it stays in `workload.yaml`, chosen to match the
   * app's actual listen port. Mirrors Go `models.ComponentEndpoint`.
   */
  endpoint?: Endpoint;
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
  /**
   * Skills the coding agent PRELOADS for this component's build. Deliberately
   * NOT an exhaustive list of what the build may consult — the rest of the
   * copied skill library stays loadable on demand. Per-component; the
   * coding runner materializes exactly these for a build of this component.
   */
  /** PRD story numbers this component serves — AGENT-AUTHORED during
   *  enrichment (#369); the build gate's coverage check reads it. */
  stories?: number[];
  skillsPinned?: string[];
}

/**
 * The single network endpoint a component exposes. Only the `name` is
 * declared — it is the shared key the coding agent's `workload.yaml` and the
 * platform's `api-configuration` trait both reference. Mirrors Go
 * `models.ComponentEndpoint`.
 */
export interface Endpoint {
  /** Workload endpoint name (the `spec.endpoints[].name` key). Defaults to "http" when the component declares no endpoint. */
  name: string;
}

/** The closed set of dependency kinds (mirrors Go `models.DependencyKind`). */
export type DependencyKind = "component" | "org-service" | "external" | "platform-resource";

// The external dependency's own definition lives in its directory
// (`./dependency-design.ts`); these re-exports keep the older import paths
// working for readers that only need the shared leaf types.
export type { DependencyStyle, DependencySuggestion, ConfigKey } from "./dependency-design.js";

/**
 * One dependency edge as a component declares it. A single flat shape carries
 * every kind's fields; `kind` selects which are meaningful — mirroring the Go
 * codec, which uses one struct and is LENIENT about kind-specific fields. Only
 * `kind` (closed set) and `name` are required. `status`/`reason` are
 * deliberately ABSENT — they are read-time computed, never authored.
 *
 * An `external` dependency is a REFERENCE: its definition (provider, style,
 * contract, config keys, candidates) lives once, in
 * `specs/design/dependencies/<name>/dependency.json` (`DependencyDesign`),
 * and every component that uses it points at that one file by `name`. The
 * fields that used to sit here (`style`, `package`, `specPath`, `candidates`,
 * `config`) are rejected on a component now — the write-gate names the file
 * they moved to. The platform hydrates the reference from the directory when
 * it reads the design, so downstream readers still see one flat edge.
 */
export interface Dependency {
  kind: DependencyKind;
  /** Sibling component / org-service provider / external system / resource name. */
  name: string;
  /** Why THIS component uses it — the dependency's own description lives in its file. */
  description?: string;
  /** platform-resource: the registered (Cluster)ResourceType. */
  resourceType?: string;
  /**
   * platform-resource: provisioning parameters. Values are mixed scalar types
   * per the target (Cluster)ResourceType schema (e.g. postgres-cnpg: `instances`
   * is an integer, `storage`/`version` are strings), marshalled verbatim into
   * the OpenChoreo Resource spec.parameters.
   */
  parameters?: Record<string, string | number | boolean>;
  /**
   * component / platform-resource / external: the consumer-side wiring,
   * PLATFORM-STAMPED at design save and re-derived on every save — never
   * authored. See `DependencyWiring`.
   */
  wiring?: DependencyWiring;
}

/**
 * The resolved consumer-side wiring for a dependency — everything the coding
 * agent needs to reach it, and the part of the `workload.yaml` `dependencies:`
 * block that is knowable without asking the cluster.
 *
 * ONE VARIANT PER `dependencies:` SUB-BLOCK, and each variant is byte-identical
 * to one entry of its block, so the agent COPIES the object rather than
 * transforming it:
 *   - `{ ref, envBindings }`  → one `dependencies.resources[]` entry
 *     (`platform-resource` / `external`)
 *   - `{ endpoint }`          → one `dependencies.endpoints[]` entry
 *     (`component` — a sibling in the same project)
 *
 * The variants are exclusive: a dependency resolves to a resource OR to an
 * endpoint, never both, and the `kind` already says which. Keeping them as
 * separate variants rather than one object of optional fields is what preserves
 * each block's own all-or-nothing rule — a half-stamped entry renders a
 * workload.yaml OpenChoreo silently ignores.
 *
 * PLATFORM-STAMPED, never authored: the platform derives it at design save and
 * OVERWRITES it on every save. An agent-authored value is therefore corrected
 * rather than rejected — the design agent reads the design, edits and writes it
 * back, so a rejection rule would reject its own echo.
 *
 * Absent means "not derivable yet" — the resource type is unknown to the
 * cluster, or (for an external dep) no config keys are declared. It never means
 * "this dependency needs no wiring": a declared dependency with no `wiring` is a
 * platform fault the coding agent reports rather than works around.
 */
export type DependencyWiring = ResourceWiring | EndpointWiringVariant;

/** The `platform-resource` / `external` variant — one `resources[]` entry. */
export interface ResourceWiring {
  /** The OpenChoreo Resource name — the `dependencies.resources[].ref` value. */
  ref: string;
  /**
   * Resource output name → the env var OpenChoreo injects it as. A
   * platform-resource's outputs are generic (host, port, …) so the keys are
   * prefixed with the dependency name (`orders-db` + `host` → `ORDERS_DB_HOST`);
   * an external resource's are already namespaced by its own config schema, so
   * the env var IS the key.
   */
  envBindings: Record<string, string>;
}

/** The `component` variant — wraps one `endpoints[]` entry. */
export interface EndpointWiringVariant {
  endpoint: EndpointWiring;
}

/**
 * One `dependencies.endpoints[]` entry pointing at a sibling component in the
 * same project.
 *
 * Every field is a pure function of the design, which is why this is stamped here
 * instead of resolved from the live cluster at dispatch time. That distinction is
 * the entire point: the live endpoint catalog is only populated once a component
 * has DEPLOYED, so on a first delivery — where siblings are coded in one cycle,
 * before anything is running — the cluster could answer nothing and the agent was
 * left to invent `component`. It invented the friendly name, OpenChoreo resolves
 * endpoint dependencies by SCOPED name, and the consumer's ReleaseBinding sat at
 * `Ready=False / ConnectionsPending` indefinitely while the platform reported
 * "deploying".
 */
export interface EndpointWiring {
  /**
   * The provider's SCOPED OpenChoreo component name (`<project>-<component>`) —
   * the key OpenChoreo resolves the binding by. NOT the friendly name the design
   * declares as the dependency's `name`.
   */
  component: string;
  /** The provider's workload endpoint name — its design's `endpoint.name`, default "http". */
  name: string;
  /** Reachability of the target endpoint; "project" for a same-project sibling. */
  visibility: string;
  /**
   * Endpoint output → the env var OpenChoreo injects it as. One key, `address`,
   * bound to the provider's base-URL variable (`todo-api` → `TODO_API_URL`) —
   * the same name a browser app already reads from `window._env_`.
   */
  envBindings: Record<string, string>;
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

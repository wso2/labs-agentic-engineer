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
 * DependencyDesign — the AUTHORED definition of one external dependency at
 * `specs/design/dependencies/<name>/dependency.json`: ONE PROJECT'S USE OF A
 * RESOURCE. The org registry holds resources; a resource becomes a dependency
 * only when a project uses it. One dependency, one definition: a component's
 * `design.json` references it by `name` only (`Dependency` in
 * `./component-design.ts`), so two components using the same provider share
 * one file, one contract document and one set of config keys.
 *
 * The definition holds a full `resource` block in the ONE SHAPE a resource has
 * everywhere (`ResourceDefinition`): the org registry record and a project's
 * copy of it (`ref` set) — or a resource the project defined itself — are the
 * same object, so the coding agent reads everything it needs from this file
 * and follows no link. What "resolved" means is on disk here: a chosen
 * provider, a contract document beside the file, the config key names — or a
 * `ref` to a registered resource. Resolution state itself is never written —
 * the platform derives it at read time (ADR-0003 unchanged), so a stale flag
 * cannot contradict the files. Style (REST client / GraphQL client / vendor
 * library) is computed from the contract's type and never stored.
 *
 * The Zod validator (`dependencyDesignSchema` in
 * `../dependency-design-schema.ts`) is drift-guarded against this type.
 */

/** How the consuming component talks to the system — COMPUTED from the contract type, kept as a vocabulary for suggestions. */
export type DependencyStyle = "rest-api" | "graphql" | "sdk";

/**
 * A service commonly used for this capability, named from the design agent's
 * knowledge while no provider is chosen — a starting point for the user's
 * choice, not a researched fit. The definition view offers each as a one-click
 * answer to "which service do you want to use?"; the resolve flow does the
 * research once one is picked. Mirrors Go `contracts.DependencySuggestion`.
 */
export interface DependencySuggestion {
  name: string;
  style?: DependencyStyle;
  description?: string;
}

/**
 * One config key the consuming component codes against. Mirrors Go
 * `contracts.ConfigKey`. Names only — values are collected on the build and
 * enforced at deploy (ADR-0023).
 */
export interface ConfigKey {
  key: string;
  secret?: boolean;
  description?: string;
  /** Never set on a secret. */
  defaultValue?: string;
}

/** The kind of document a contract is. A project contract is one of the first three. */
export type ResourceContractType = "openapi" | "graphql" | "sdk" | "asyncapi" | "protobuf" | "documentation";

/** Where a project's contract file came from. */
export type ContractOrigin = "registry" | "provider" | "derived" | "assumed";

/**
 * The user's permission to build against a contract the agent wrote from
 * research rather than from the provider's document. USER-WRITTEN: the platform
 * records it when the user accepts the agent's proposal on the dependency
 * page; the agent may echo it on a later edit but never introduce or change
 * it (the write-gate compares against the file already in the bundle).
 */
export interface DependencyAssumption {
  /** Who accepted — the signed-in user's login. */
  by: string;
  /** RFC 3339 instant of the acceptance. */
  at: string;
  /** The agent's own statement of what it was unsure about, as shown when accepting. */
  note?: string;
}

/**
 * The contract as held at ONE level: `{ type, path }`, the path relative to that
 * level's store — the org docs repo for a registry record, the dependency's own
 * directory for a project copy (a bare file name: `openapi.yaml`,
 * `schema.graphql`, `sdk.json`). There is deliberately NO URL form: an internet
 * address is provenance, never a contract, and the coding agent must find
 * nothing in the repo it could follow off it. Mirrors Go `contracts.ResourceContract`.
 */
export interface ResourceContract {
  type: ResourceContractType;
  path: string;
  /**
   * Project copy only: `registry` (copied byte for byte from the org record's
   * document), `provider` (the provider's own document, fetched or uploaded
   * whole), `derived` (written by the design agent from the provider's
   * developer reference), `assumed` (written from less than that; counts only
   * once `accepted`).
   */
  origin?: ContractOrigin;
  /** Platform-written; see `DependencyAssumption`. */
  accepted?: DependencyAssumption;
}

/**
 * Where a copy of a contract document came from, at either level, with the
 * whole document's hash at the time so a changed source is detectable and a
 * refresh is a re-copy. Mirrors Go `contracts.ResourceProvenance`.
 */
export interface ResourceProvenance {
  /** The internet address a copy was fetched from. */
  sourceUrl?: string;
  /** The org-resource-docs path (`<name>/<file>`) a project copy was taken from. Never a URL. */
  registry?: string;
  /** SHA-256 (hex) of the whole document at the source. */
  sha256?: string;
  /** RFC 3339 instant the source was read. */
  readOn?: string;
}

/**
 * An External resource — what the thing is — in the one shape it has at both
 * levels. Mirrors Go `contracts.ResourceDefinition`. Values are never here.
 */
export interface ResourceDefinition {
  /**
   * Project copy only: the registry name this block was copied from. Present
   * ⇒ a Registered External resource is reused here; it must equal the
   * dependency's `name`. The agent writes it as a STUB — `{ ref, name }` and
   * nothing else — and the platform fills the block at save. Absent ⇒ a
   * Project External resource the design defined.
   */
  ref?: string;
  /** Must equal the dependency's `name`. */
  name: string;
  description?: string;
  /**
   * The concrete system ("Stripe", "Open Exchange Rates"). Absent on a project
   * block only while the user has not chosen one (`suggestions` open on the
   * dependency). Never beside `suggestions`.
   */
  provider?: string;
  /** The env-var keys every consumer codes against; written only once `provider` is set. */
  config?: ConfigKey[];
  contract?: ResourceContract;
  /**
   * How the organization wants the resource used. Written by an organization
   * at register and copied by the platform; never authored on a resource the
   * project defined itself.
   */
  consumptionInstructions?: string;
  /** Registry record only — where the org copy of the document came from. */
  provenance?: ResourceProvenance;
}

export interface DependencyDesign {
  /** Must equal the dependency's directory name (kebab-case). */
  name: string;
  /** The full resource block — a copy from the registry (`ref` set) or one this project defined. */
  resource: ResourceDefinition;
  /** Where this project's contract document came from (registry file, or provider address) and its hash. */
  provenance?: ResourceProvenance;
  /**
   * Services the user might choose, while no provider is chosen. Any length;
   * choosing one REMOVES the field and sets `resource.provider`. The agent
   * never turns a suggestion into a provider on its own.
   */
  suggestions?: DependencySuggestion[];
}

/**
 * The SDK manifest at `specs/design/dependencies/<name>/sdk.json` — the
 * contract document of an `sdk`-type contract: where the consuming
 * component's implementation language finds the SDK, and the calls the design
 * relies on.
 */
export interface SdkManifest {
  /**
   * Ecosystem-prefixed package identifier per implementation language, keyed by
   * the language as a component's `design.json` names it (lower-case):
   * `{ "typescript": "npm:stripe@^14", "go": "go:github.com/stripe/stripe-go/v79" }`.
   * The consuming component's language must have an entry.
   */
  packages: Record<string, string>;
  /** The provider's SDK documentation. */
  docsUrl?: string;
  /** The SDK calls the design relies on, in the SDK's own naming. */
  calls?: string[];
  /** Retained for manifests written before `contract.origin` existed; origin is the source of truth now. */
  derived?: boolean;
  /** Retained for manifests written before `contract.origin` existed; origin is the source of truth now. */
  assumed?: boolean;
}

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
 * `specs/design/dependencies/<name>/dependency.json`, beside the contract it
 * points at. One dependency, one definition: a component's `design.json`
 * references it by `name` only (`Dependency` in `./component-design.ts`), so
 * two components using the same provider share one file, one contract and one
 * set of config keys. The directory name, this `name`, the cell's south node
 * and the org registry key are the same identifier.
 *
 * What "resolved" means is on disk here: a chosen `provider`, a committed
 * `contract` file in the same directory, and the `config` key names. Resolution
 * state itself is never written — the platform derives it at read time from
 * which of these are present (ADR-0003 unchanged), so a stale flag cannot
 * contradict the files.
 *
 * The Zod validator (`dependencyDesignSchema` in
 * `../dependency-design-schema.ts`) is drift-guarded against this type.
 */

/** How the consuming component talks to the system. */
export type DependencyStyle = "rest-api" | "graphql" | "sdk";

/**
 * Who owns the definition. `project` (the default) is agent-authored in this
 * repo. `org` marks a copy of a Registered External resource: the platform
 * stamps provider, contract and config from the org record at every design
 * save (the way `wiring` is derived), the agent does not edit those fields, and
 * the build collects no values for it — they live on the org record.
 */
export type DependencySource = "project" | "org";

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

/**
 * Where the committed contract came from, so a reader can tell a slice of the
 * provider's published document from something typed by hand, and re-derive
 * the slice when the source moves.
 */
export interface DependencyProvenance {
  /** The document the contract was taken from — a URL, or the name of an uploaded file. */
  sourceUrl?: string;
  /** SHA-256 (hex) of the FULL source document, not of the slice. */
  sha256?: string;
  /** RFC 3339 instant the source was read. */
  fetchedAt?: string;
  /** True when the committed contract is a slice of the source; false when it is the whole document. */
  sliced?: boolean;
}

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

export interface DependencyDesign {
  /** Must equal the dependency's directory name (kebab-case). */
  name: string;
  /** What the system does for this product, in the product's words. */
  description?: string;
  /** Defaults to `project`. See `DependencySource`. */
  source?: DependencySource;
  /**
   * The concrete system chosen ("Stripe", "SendGrid"). Absent until the user
   * chose one — in requirements, or on the definition, or in the resolve
   * flow; present on every resolved or assumed dependency. Never beside
   * `suggestions`.
   */
  provider?: string;
  /** Set once a provider is chosen. */
  style?: DependencyStyle;
  /**
   * The contract file IN THIS DIRECTORY: `openapi.yaml` (or `.yml` / `.json`)
   * for `rest-api`, `schema.graphql` for `graphql`. An `sdk` dependency carries
   * it too when the provider has an API behind the SDK; without one the
   * dependency reads as SDK-only. Absent ⇒ the dependency still needs its
   * contract and the build gate blocks.
   */
  contract?: string;
  /** `sdk` style only: the manifest file in this directory (`sdk.json`). */
  sdk?: string;
  provenance?: DependencyProvenance;
  /**
   * Services the user might choose, while no provider is chosen. Any length;
   * choosing one REMOVES the field and sets `provider` + `style`. The agent
   * never turns a suggestion into a provider on its own.
   */
  suggestions?: DependencySuggestion[];
  /**
   * The config-key schema every consuming component codes against. Derived
   * from the chosen service, so it is written only once `provider` is set.
   */
  config?: ConfigKey[];
  /** See `DependencyAssumption`. */
  assumed?: DependencyAssumption;
}

/**
 * The SDK manifest at `specs/design/dependencies/<name>/sdk.json` — where the
 * consuming component's implementation language finds the SDK. The API slice
 * beside it (when the provider has one) is what the coding agent falls back to
 * when the SDK lacks a call, and what validation checks against.
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
  /**
   * The agent wrote this manifest from the provider's SDK reference — the
   * sdk.json twin of a contract's `x-aep-derived: true`. Resolved, flagged.
   */
  derived?: boolean;
  /**
   * The agent wrote this manifest without a published source — the sdk.json
   * twin of a contract's `x-aep-assumed: true`. Counts only once a user accepts
   * the assumption (`DependencyDesign.assumed`).
   */
  assumed?: boolean;
}

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
import type { ConnectionRow } from "../../projects/lib/promotion";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ConfigKey = components["schemas"]["ConfigKey"];
type Dependency = components["schemas"]["Dependency"];
type ProjectDependencyReadiness =
  components["schemas"]["ProjectDependencyReadiness"];
type ValueState = components["schemas"]["ExternalDependencyValueState"];

// EXTERNAL RESOURCES — the values a person still owes this version, joined from
// the two reads that each hold half of the answer.
//
// THE READINESS READ DECIDES WHICH ROWS EXIST; the design decides what each one
// says. Readiness enumerates the externals THIS PROJECT CAN SUPPLY — it is
// derived from the same design the console reads, so it lists every suppliable
// one whether or not a binding exists yet, and it deliberately omits a
// Registered External, whose values live on the org catalog record and whose
// project-scoped save answers 409 `values live on the org record` (ADR-0023).
// The deploy gate omits it for the same reason, so rendering one here would
// offer a Configure button that 409s under a headline contradicting a gate that
// is not blocked.
//
// The design is still the only source of a dependency's description and of the
// config-key schema the dialog collects, so the join stands.
//
// There is deliberately NO "the readiness read never mentioned it" fallback: a
// dependency readiness omits is one the project cannot supply, not one nobody
// has supplied yet. That is only safe because both sides enumerate from the
// same design — and because an ABSENT readiness response (pending, or failed)
// is never mistaken for an empty one: this function renders nothing at all
// without one, and the section distinguishes the three cases before it decides
// what to say.
//
// EXTERNALS ONLY. A platform resource has no values for anyone to supply — the
// platform authors its credentials itself — and its progress is already
// reported by the run's own provisioning gates. Listing one here would offer a
// button that opens a dialog with nothing to type in.

/** How a row reads to a person: is there anything left to do about it? */
export type ExternalResourceDisplay = "configured" | "needs-values";

/** One external dependency, and whether its values have arrived. */
export interface ExternalResourceRow {
  /** The dependency's name — unique per project, so also the React key and the
   *  path segment the save posts to. */
  name: string;
  /** The design's own sentence about the dependency, when it carries one. */
  description?: string;
  /** The keys the dialog collects, straight off the design's config schema. */
  config: ConfigKey[];
  /** The readiness read's own word, kept verbatim so a row can be traced back
   *  to what the platform actually said. */
  state: ValueState;
  display: ExternalResourceDisplay;
  /** How many of its keys the platform is still missing. */
  missingCount: number;
}

/**
 * The readiness state, as an ACTIONABLE / DONE decision.
 *
 * `unset` is the state this section is normally looking at: the build authored
 * the dependency's binding with every declared key present and empty (ADR-0023's
 * "names at build time, values later"), so the resource exists and is waiting on
 * a person.
 *
 * `not-provisioned` — no binding at all — is the same ask, and must NOT read as
 * "the platform is still working on it". Two reasons it is actionable rather
 * than a wait:
 *
 *   - For an EXTERNAL dependency there is no platform work to wait FOR. Nothing
 *     provisions it in the background; the only thing that fills it is a person
 *     typing values. `provisioning.SaveValues` authors the OpenChoreo resource
 *     from scratch and needs no prior binding.
 *   - It is reachable. A project whose design declares a dependency that no
 *     build has authored yet — a dependency added since the last build, or a
 *     first visit before any build has run — reports exactly this. A
 *     "provisioning, come back later" row would never resolve, because nothing
 *     is coming.
 *
 * So both collapse to one actionable state.
 */
export function displayState(state: ValueState): ExternalResourceDisplay {
  return state === "configured" ? "configured" : "needs-values";
}

/** Dependency names are matched case-insensitively — the platform slugs them
 *  on the way to OpenChoreo, so the readiness read can answer in lower case
 *  for a design that declared `Stripe`. */
function key(name: string): string {
  return name.toLowerCase();
}

/**
 * Every external the DESIGN declares that could be asked of a person, merged
 * across the components that declare it (a shared dependency is declared on
 * every consumer's design.json and its values are supplied once, not once per
 * consumer) and keyed by slug.
 *
 * The UNION of each declaration's config keys, not the first one seen. Two
 * components may declare the same dependency with different keys, and readiness
 * is computed against the union (`spec.UnionExternalConfigKeys`) — so keeping
 * only the first schema would render a dialog that cannot submit the keys the
 * other component declared, and the dependency would stay `unset` however many
 * times a person saved it. That is a deploy gate nobody can clear from this
 * page, which is the failure this section exists to prevent.
 *
 * The merge mirrors that Go helper deliberately: first-seen spelling wins for
 * the name, keys dedupe by `key`, and SECRET WINS on conflict — a key any
 * component marks secret must never be collected as a plain value.
 *
 * Only dependencies that declare config keys are collectable: a row exists to
 * collect values, and one with an empty schema has none to collect.
 */
function declaredExternals(
  design: ComponentDependencies[] | null | undefined,
): Map<string, Dependency> {
  const byName = new Map<string, Dependency>();
  for (const comp of design ?? []) {
    for (const dep of comp.dependencies ?? []) {
      if (dep.kind !== "external") continue;
      const config = dep.config ?? [];
      if (config.length === 0) continue;
      const slug = key(dep.name);
      const seen = byName.get(slug);
      if (!seen) {
        byName.set(slug, { ...dep, config: [...config] });
        continue;
      }
      const merged = [...(seen.config ?? [])];
      for (const k of config) {
        const at = merged.findIndex((m) => m.key === k.key);
        if (at === -1) {
          merged.push(k);
          continue;
        }
        if (k.secret) merged[at] = { ...merged[at]!, secret: true };
      }
      byName.set(slug, {
        ...seen,
        // A description on any declaration beats none — the first component to
        // declare the dependency need not be the one that explained it.
        ...(seen.description ? {} : dep.description ? { description: dep.description } : {}),
        config: merged,
      });
    }
  }
  return byName;
}

/**
 * How many external dependencies the design declares that somebody could be
 * asked to supply.
 *
 * This is the ONLY thing the design read alone genuinely supports, and it is
 * what the section falls back to when the readiness read is unknown: the design
 * proves the ask exists; only readiness knows which of them this project can
 * supply, and which are still outstanding.
 */
export function declaredExternalCount(
  design: ComponentDependencies[] | null | undefined,
): number {
  return declaredExternals(design).size;
}

/**
 * One row per external dependency the READINESS READ names, described by the
 * design.
 *
 * Without a readiness response there are no rows — see the module comment: an
 * absent response means "not known yet", never "nothing to configure", and the
 * caller is responsible for saying which.
 */
export function externalResourceRows(
  design: ComponentDependencies[] | null | undefined,
  readiness: ProjectDependencyReadiness | undefined,
): ExternalResourceRow[] {
  if (!readiness) return [];
  const declared = declaredExternals(design);
  const rows: ExternalResourceRow[] = [];
  for (const reported of readiness.dependencies) {
    const dep = declared.get(key(reported.name));
    // Readiness enumerates from the design, so this is the design lagging the
    // platform (a dependency just removed) — not a row to invent a schema for.
    if (!dep) continue;
    rows.push({
      // The DESIGN's spelling is what a person reads; the platform slugs it.
      name: dep.name,
      ...(dep.description && { description: dep.description }),
      config: dep.config ?? [],
      state: reported.state,
      display: displayState(reported.state),
      // Missing keys are the platform's own tally, against the same design.
      missingCount: reported.missingKeys.length,
    });
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name));
}

/** The section's headline: what is still wanted, in one line. */
export function externalResourceHeadline(rows: ExternalResourceRow[]): string {
  const outstanding = rows.filter((row) => row.display === "needs-values").length;
  return outstanding === 0
    ? `${rows.length} of ${rows.length} configured`
    : `${outstanding} of ${rows.length} need configuration`;
}

/**
 * A row as the value-entry dialog wants it. The dialog is the Deployments
 * page's, unchanged, and it speaks `ConnectionRow` — the design-derived shape
 * both pages collect values through. `provisioned` is false by construction:
 * every row here carries config keys, which is what "there is something to
 * supply" means to that shape.
 */
export function asConnectionRow(row: ExternalResourceRow): ConnectionRow {
  return {
    id: row.name,
    name: row.name,
    kind: "external",
    config: row.config,
    provisioned: false,
  };
}

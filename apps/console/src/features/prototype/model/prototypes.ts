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

import { parsePrototypeCommand } from "@aep/contracts/commands";
import { parseManifestJson, type PrototypeManifest } from "@wso2/prototype-kit/manifest";
import type { DesignArtifact } from "../../design/api/designModel";

// The prototypes a design has, worked out from the room (spec #860). Each
// web-application component the design names may have one: a `prototype.json`
// manifest and a `prototype.tsx` source beside its design.json. A component's
// prototype is
//
//   none      neither file is there yet;
//   ready     the manifest parses and the source is there: it can be reviewed;
//   invalid   one file is missing, or the manifest does not parse (the reason
//             says which, so the review shows it instead of a blank frame);
//   revising  a `/prototype` turn for it is running now.
//
// Pure, so an agent's write shows on the next render. The checks here are the
// ones the review needs to open; the kit's full rules ran at the agent's write
// gate and the save gate.

/** Where the room keeps the design's components; the prototype files sit under it. */
export const COMPONENTS_PREFIX = "specs/design/components/";

export type PrototypeStatus = "none" | "ready" | "invalid" | "revising";

/** A prototype that can be shown: both files, the manifest parsed. */
export interface PrototypeFiles {
  manifestText: string;
  manifest: PrototypeManifest;
  source: string;
}

export interface AppPrototype {
  /** The web-application component, as named under `specs/design/components/`. */
  component: string;
  status: PrototypeStatus;
  /** Why it cannot be shown; null unless a file is missing or the manifest is invalid. */
  problem: string | null;
  /** What the review shows; null until both files are there and the manifest parses. */
  files: PrototypeFiles | null;
  /** Some prototype file exists: making one again updates it. */
  exists: boolean;
}

export function manifestPath(component: string): string {
  return `${COMPONENTS_PREFIX}${component}/prototype.json`;
}

export function sourcePath(component: string): string {
  return `${COMPONENTS_PREFIX}${component}/prototype.tsx`;
}

/**
 * The design's web applications: each component whose design.json says
 * `"type": "web-application"`, as the design card's catalog carries it (a
 * component-and-contract artifact). An API-only product has none.
 */
export function webApplications(artifacts: readonly DesignArtifact[]): string[] {
  const names = artifacts.flatMap((a) => {
    if (a.source.kind !== "contract") return [];
    try {
      const design = JSON.parse(a.source.design) as { name?: unknown; type?: unknown };
      if (design.type !== "web-application") return [];
      return [typeof design.name === "string" && design.name ? design.name : a.id];
    } catch {
      return [];
    }
  });
  return [...new Set(names)].sort();
}

/**
 * Which prototypes a running turn is revising, from its instruction: one
 * component, every one (a bare `/prototype`), or none (not a prototype turn).
 */
export function revisingIn(instruction: string | undefined): { component: string | null } | null {
  return instruction ? parsePrototypeCommand(instruction) : null;
}

function problemOf(manifestText: string | undefined, source: string | undefined): { problem: string } | { files: PrototypeFiles } {
  if (manifestText === undefined) return { problem: "prototype.json is missing: ask the agent to make the prototype again." };
  const parsed = parseManifestJson(manifestText);
  if (!parsed.ok) {
    const [first, ...rest] = parsed.findings;
    const more = rest.length > 0 ? ` (and ${rest.length} more)` : "";
    return { problem: `prototype.json is invalid: ${first?.message ?? "it does not match the kit's schema"}${more}` };
  }
  if (source === undefined || source.trim() === "") return { problem: "prototype.tsx is missing: ask the agent to make the prototype again." };
  return { files: { manifestText, manifest: parsed.manifest, source } };
}

/** Each web application's prototype, in the order given, from the room's files and the running turn. */
export function appPrototypes(
  components: readonly string[],
  files: Readonly<Record<string, string>>,
  revising: { component: string | null } | null,
): AppPrototype[] {
  return components.map((component) => {
    const manifestText = files[manifestPath(component)];
    const source = files[sourcePath(component)];
    const exists = manifestText !== undefined || source !== undefined;
    const outcome = exists ? problemOf(manifestText, source) : null;
    const running = revising !== null && (revising.component === null || revising.component === component);
    const status: PrototypeStatus = running ? "revising" : !outcome ? "none" : "files" in outcome ? "ready" : "invalid";
    return {
      component,
      status,
      problem: outcome && "problem" in outcome ? outcome.problem : null,
      files: outcome && "files" in outcome ? outcome.files : null,
      exists,
    };
  });
}

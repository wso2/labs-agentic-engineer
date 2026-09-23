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
 * design.json projection — the per-component design.json bundle, folded
 * into the derived machine view (`ProjectDesign` in types.ts) that the
 * cell diagram, coding-agent dispatch, and task generation consume. Pure:
 * files in, JSON out; nobody authors this artifact and the agent never sees
 * it (like the compiled .excalidraw, it is excluded from snapshots). In
 * production the BFF runs the equivalent projection; the playground writes
 * `design.gen.json` post-turn so the shape is inspectable while testing.
 */

import type { ProjectDesign, ProjectDesignComponent, ProjectDesignConnection } from "./types.js";

const COMPONENT_RE = /^specs\/design\/components\/([^/]+)\/design\.json$/;

const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

// How each unified dependency `kind` renders as a cell-diagram edge. The
// authored `dependencies[]` (unified, kind-discriminated — the successor to the
// legacy `connections[]`) drives the diagram:
//   - component / org-service are on-platform HTTP hops (a sibling component, or
//     another project's service);
//   - external is an off-platform HTTP call (SaaS / legacy);
//   - platform-resource is an on-platform datastore/backing-service.
// The downstream cell-diagram split (in-project vs east/external) keys off the
// edge target name + onPlatform, so this table is the only kind-awareness needed.
const DEP_KIND_EDGE: Record<string, { type: string; onPlatform: boolean }> = {
  component: { type: "http", onPlatform: true },
  "org-service": { type: "http", onPlatform: true },
  external: { type: "http", onPlatform: false },
  "platform-resource": { type: "datastore", onPlatform: true },
};

function dependencyEdges(v: unknown): ProjectDesignConnection[] {
  if (!Array.isArray(v)) return [];
  const out: ProjectDesignConnection[] = [];
  for (const d of v) {
    if (!d || typeof d !== "object") continue;
    const rec = d as Record<string, unknown>;
    const name = str(rec.name);
    const kind = str(rec.kind);
    if (!name || !kind) continue;
    const edge = DEP_KIND_EDGE[kind];
    if (!edge) continue; // unknown kind: not a diagram edge (validated elsewhere)
    out.push({ id: `${edge.type}://${name}`, type: edge.type, onPlatform: edge.onPlatform });
  }
  return out;
}

/**
 * STAGE 1 — the base unit: ONE component's design.md (+ sibling artifact
 * presence) → its ProjectDesignComponent. Everything here comes from the
 * component's own directory; no cross-component knowledge.
 */
export function projectComponent(id: string, files: Record<string, string>): ProjectDesignComponent {
  const dir = `specs/design/components/${id}`;
  // The authored file is schema-validated at write time (FileBundle); parse
  // defensively anyway so a hand-edited bundle degrades to defaults, not a throw.
  let dj: Record<string, unknown> = {};
  try {
    const parsed: unknown = JSON.parse(files[`${dir}/design.json`] ?? "");
    if (parsed && typeof parsed === "object") dj = parsed as Record<string, unknown>;
  } catch {
    /* defaults below */
  }
  // The design phase accepts ANY component kind the requirements imply; the
  // authored type passes through untouched (support-gating is a later phase).
  const type = str(dj.type) ?? "service";
  const exposure = str(dj.exposure) ?? "intranet";
  // Non-string entries are filtered rather than rejected: the projection is a
  // read-side view, so a malformed authored value degrades to fewer pins here
  // and is caught by the write-gate, not by breaking the diagram.
  const skillsSource = Array.isArray(dj.skillsPinned) ? dj.skillsPinned : [];
  const skillsPinned = skillsSource.filter((s): s is string => typeof s === "string");

  const artifacts: Record<string, string> = { design: `${dir}/design.json` };
  if (files[`${dir}/openapi.yaml`] !== undefined) artifacts.openapi = `${dir}/openapi.yaml`;
  if (files[`${dir}/wireframes.dsl`] !== undefined) artifacts.wireframes = `${dir}/wireframes.dsl`;
  // A web-application may also carry its reviewed prototype (the /prototype
  // flow's output); only a web-application has one.
  if (files[`${dir}/prototype.json`] !== undefined) artifacts.prototype = `${dir}/prototype.json`;

  // exactOptionalPropertyTypes: omit absent keys instead of `key: undefined`.
  const build: ProjectDesignComponent["build"] = {};
  for (const key of ["language", "buildpack", "appPath", "entrypoint"] as const) {
    const v = str(dj[key]);
    if (v !== undefined) build[key] = v;
  }

  return {
    id,
    type,
    version: str(dj.version) ?? "0.1.0",
    skillsPinned,
    build,
    ...(type === "service"
      ? {
          services: {
            [id]: {
              id,
              label: id,
              type: "http",
              deploymentMetadata: {
                gateways: {
                  internet: { isExposed: exposure === "internet" },
                  intranet: { isExposed: exposure === "intranet" },
                },
              },
            },
          },
        }
      : {}),
    connections: dependencyEdges(dj.dependencies),
    artifacts,
  };
}

/**
 * STAGE 2 — aggregation: every component projection folded into the
 * project-wide ProjectDesign. Downstream views (cell diagram, task planning)
 * build on THIS, because their subject is the relationships BETWEEN
 * components.
 */
export function buildProjectDesign(projectName: string, files: Record<string, string>): ProjectDesign {
  const ids = Object.keys(files)
    .map((p) => COMPONENT_RE.exec(p)?.[1])
    .filter((id): id is string => id !== undefined)
    .sort();
  const components = ids.map((id) => projectComponent(id, files));

  return { modelVersion: "0.4.0", id: projectName, name: projectName, components };
}

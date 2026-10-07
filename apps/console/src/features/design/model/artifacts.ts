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

import type { ArtifactDepth, DesignArtifact, DesignDependency } from "../api/designModel";

// The design card's artifact list, worked out from the design model and the
// live spec: one list, filtered by depth, each artifact with the features it
// covers and a marker for what happened to it lately. Pure, so a keystroke
// that puts a feature out of date marks its artifacts on the next render.

export type DepthFilter = "all" | ArtifactDepth;

export const DEPTH_FILTERS: { value: DepthFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "business", label: "Business" },
  { value: "technical", label: "Technical" },
];

/** The artifacts a filter shows, in the list's order, and how many it hides. */
export function filterArtifacts(
  artifacts: DesignArtifact[],
  filter: DepthFilter,
): { shown: DesignArtifact[]; hidden: number } {
  const shown = filter === "all" ? artifacts : artifacts.filter((a) => a.depth === filter);
  return { shown, hidden: artifacts.length - shown.length };
}

/**
 * What happened to an artifact lately. "out of date": a feature it covers
 * changed in the spec since it was designed, and wins over the rest.
 * Otherwise "new" when the latest design turn added it and "changed" when the
 * latest turn changed it; nothing once a later turn has passed it by.
 */
export type ArtifactMarker = "out of date" | "new" | "changed";

export function artifactMarker(
  artifact: Pick<DesignArtifact, "features" | "addedIn" | "changedIn">,
  revision: number,
  outOfDate: readonly string[],
): ArtifactMarker | null {
  if (artifact.features.some((f) => outOfDate.includes(f))) return "out of date";
  if (artifact.addedIn === revision) return "new";
  if (artifact.changedIn === revision) return "changed";
  return null;
}

/** Dependencies still waiting on an answer: each blocks its own feature's build, nothing else. */
export function blockingDependencies(dependencies: DesignDependency[]): DesignDependency[] {
  return dependencies.filter((d) => d.answer === null);
}

/** The key the design card's URL names a dependency by (`?art=dep-F3`), beside the artifacts' own IDs. */
export function dependencyKey(featureId: string): string {
  return `dep-${featureId}`;
}

/** What the card shows for `?art=`: that artifact or dependency, else the first there is. */
export function openArtifact(
  artifacts: DesignArtifact[],
  dependencies: DesignDependency[],
  art: string | undefined,
):
  | { kind: "artifact"; artifact: DesignArtifact }
  | { kind: "dependency"; dependency: DesignDependency }
  | null {
  const dependency = dependencies.find((d) => dependencyKey(d.featureId) === art);
  if (dependency) return { kind: "dependency", dependency };
  const artifact = artifacts.find((a) => a.id === art) ?? artifacts[0];
  return artifact ? { kind: "artifact", artifact } : null;
}

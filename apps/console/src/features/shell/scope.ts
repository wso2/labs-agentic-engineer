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

// Where the reader is, in the shell's terms: at org level (the Projects grid,
// New project), or in a project with at most one card open over its overview
// (and, on the spec card, the file open in it).
// The rail's active item, whether the chat panel exists, its breadcrumb and
// its scope line all follow from this, so it is worked out once, from the
// router's leaf match, here.

/** The cards drawn over a project's overview, each a route of its own. */
export type ProjectCard = "spec" | "design" | "prototype" | "builds";

export type ShellScope =
  | { kind: "org"; page: "projects" | "new" | "other" }
  | {
      kind: "project";
      projectName: string;
      card: ProjectCard | null;
      /** The spec card's open file (`?file=`), by its key; null on the product page and off the card. */
      specFile: string | null;
    };

const CARD_ROUTES: Record<string, ProjectCard> = {
  "/projects/$projectName/spec": "spec",
  "/projects/$projectName/design": "design",
  "/projects/$projectName/prototype": "prototype",
  "/projects/$projectName/builds/": "builds",
  "/projects/$projectName/builds/$version": "builds",
};

/** The scope of the deepest matched route. */
export function shellScope(leaf: {
  routeId: string;
  params: { projectName?: string };
  search?: { file?: unknown };
}): ShellScope {
  const { routeId, params, search } = leaf;
  if (routeId === "/") return { kind: "org", page: "projects" };
  if (routeId === "/projects/new") return { kind: "org", page: "new" };
  if (params.projectName && routeId.startsWith("/projects/$projectName")) {
    const card = CARD_ROUTES[routeId] ?? null;
    const file = search?.file;
    return {
      kind: "project",
      projectName: params.projectName,
      card,
      specFile: card === "spec" && typeof file === "string" && file ? file : null,
    };
  }
  return { kind: "org", page: "other" };
}

const CARD_TITLE: Record<ProjectCard, string> = {
  spec: "Spec",
  design: "Design",
  prototype: "Prototype",
  builds: "Builds",
};

export function cardTitle(card: ProjectCard): string {
  return CARD_TITLE[card];
}

/**
 * What a message sent from here would be about, for the line above the
 * composer: the design card talks about the design review; a feature open in
 * the spec card narrows it to that feature, and a change reaching past it is
 * made there too; everywhere else in a project, the whole product.
 */
export function chatTopic(
  card: ProjectCard | null,
  openFeature: string | null,
): { topic: string; note: string | null } {
  if (card === "design" || card === "prototype") return { topic: "the design review", note: null };
  if (card === "spec" && openFeature) {
    return { topic: openFeature, note: "A change that reaches other features is made there too." };
  }
  return { topic: "the whole product", note: null };
}

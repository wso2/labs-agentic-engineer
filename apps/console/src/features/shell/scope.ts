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

// Where the reader is, in the shell's terms: at org level (the Dashboard, the
// Projects grid, New project, Skills, Resources), possibly with one of the
// org's cards open over its Page (Settings over the Dashboard, a Skill over
// Skills, a Resource over Resources), or on one of a project's Pages with at most one of its Cards
// open over it (and, on the spec card, the file open in it).
// The rail's active item, whether the chat panel exists, its breadcrumb and
// its scope line all follow from this, so it is worked out once, from the
// router's leaf match, here. Which routes are Pages and which are Cards, and
// the Page each Card is over, are this module's tables
// (design/pages-and-cards.md).

/** The org's Pages; "other" is an org-level address that is none of them. */
export type OrgPage = "dashboard" | "projects" | "new" | "skills" | "resources" | "other";

/** The Cards drawn over the org's Pages: Settings over the Dashboard, a Skill over Skills, a Resource over Resources. */
export type OrgCard = "settings" | "skill" | "resource";

/** A project's Pages: each is a layout route that draws itself under its Cards. */
export type ProjectPage = "overview" | "builds" | "validations" | "deploy" | "issues";

/** The Cards drawn over a project's Pages, each a route of its own. */
export type ProjectCard =
  | "spec"
  | "design"
  | "prototype"
  | "questions"
  | "build"
  | "validation"
  | "configure"
  | "issue";

export type ShellScope =
  | { kind: "org"; page: OrgPage; card: OrgCard | null }
  | {
      kind: "project";
      projectName: string;
      page: ProjectPage;
      card: ProjectCard | null;
      /** The spec card's open file (`?file=`), by its key; null on the product page and off the card. */
      specFile: string | null;
    };

const ORG_PAGE_ROUTES: Record<string, OrgPage> = {
  "/_dashboard/": "dashboard",
  "/projects/": "projects",
  "/projects/new": "new",
  "/skills": "skills",
  "/resources": "resources",
};

const ORG_CARD_ROUTES: Record<string, OrgCard> = {
  "/_dashboard/settings": "settings",
  "/skills/$name": "skill",
  "/skills/new": "skill",
  "/resources/$name": "resource",
  "/resources/new": "resource",
};

/** The org Page each org Card opens over, and closes back to. */
const ORG_CARD_PAGE: Record<OrgCard, OrgPage> = {
  settings: "dashboard",
  skill: "skills",
  resource: "resources",
};

const PAGE_ROUTES: Record<string, ProjectPage> = {
  "/projects/$projectName/_overview/": "overview",
  "/projects/$projectName/builds": "builds",
  "/projects/$projectName/validations": "validations",
  "/projects/$projectName/deploy": "deploy",
  "/projects/$projectName/issues": "issues",
};

const CARD_ROUTES: Record<string, ProjectCard> = {
  "/projects/$projectName/_overview/spec": "spec",
  "/projects/$projectName/_overview/design": "design",
  "/projects/$projectName/_overview/prototype": "prototype",
  "/projects/$projectName/_overview/questions": "questions",
  "/projects/$projectName/builds/$version": "build",
  "/projects/$projectName/validations/$version": "validation",
  "/projects/$projectName/deploy/$env/configure": "configure",
  "/projects/$projectName/issues/$number": "issue",
};

/** The Page each Card opens over, and closes back to. */
const CARD_PAGE: Record<ProjectCard, ProjectPage> = {
  spec: "overview",
  design: "overview",
  prototype: "overview",
  questions: "overview",
  build: "builds",
  validation: "validations",
  configure: "deploy",
  issue: "issues",
};

/** The Card a route draws, the org's or a project's, or null when it draws none. */
export function cardOfRoute(routeId: string): ProjectCard | OrgCard | null {
  return CARD_ROUTES[routeId] ?? ORG_CARD_ROUTES[routeId] ?? null;
}

/** The Page a project Card is drawn over. */
export function pageOfCard(card: ProjectCard): ProjectPage {
  return CARD_PAGE[card];
}

/** The scope of the deepest matched route. */
export function shellScope(leaf: {
  routeId: string;
  params: { projectName?: string };
  search?: { file?: unknown };
}): ShellScope {
  const { routeId, params, search } = leaf;
  if (params.projectName && routeId.startsWith("/projects/$projectName")) {
    const card = CARD_ROUTES[routeId] ?? null;
    const file = search?.file;
    return {
      kind: "project",
      projectName: params.projectName,
      // An address in a project that is neither a Page nor a Card (one it
      // does not have) reads as the overview's.
      page: card ? pageOfCard(card) : (PAGE_ROUTES[routeId] ?? "overview"),
      card,
      specFile: card === "spec" && typeof file === "string" && file ? file : null,
    };
  }
  const orgCard = ORG_CARD_ROUTES[routeId];
  if (orgCard) return { kind: "org", page: ORG_CARD_PAGE[orgCard], card: orgCard };
  return { kind: "org", page: ORG_PAGE_ROUTES[routeId] ?? "other", card: null };
}

const PAGE_TITLE: Record<ProjectPage, string> = {
  overview: "Overview",
  builds: "Builds",
  validations: "Validation",
  deploy: "Deploy",
  issues: "Issues",
};

export function pageTitle(page: ProjectPage): string {
  return PAGE_TITLE[page];
}

const CARD_TITLE: Record<ProjectCard, string> = {
  spec: "Spec",
  design: "Design",
  prototype: "Prototype",
  questions: "Questions",
  build: "Build",
  validation: "Validation",
  configure: "Configure",
  issue: "Issue",
};

export function cardTitle(card: ProjectCard): string {
  return CARD_TITLE[card];
}

/**
 * What a message sent from here would be about, for the line above the
 * composer: the design card talks about the design review; a feature open in
 * the spec card narrows it to that feature, and a change reaching past it is
 * made there too; everywhere else in a project, the whole product. That
 * includes a Build or Validation card, the Deploy Page, an environment's
 * Configure card and an Issue card: no agent works on one build, one
 * validation, one environment or one issue yet, so they set no Turn scope of
 * their own.
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

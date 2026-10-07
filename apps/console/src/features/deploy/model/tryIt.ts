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
import type { ColumnComponent, EnvironmentColumn } from "./pipeline";

// Try it, the Panel: what each component running in an environment offers a
// person, after the old console's Try it out (TryItOut.tsx, tryItAppUrl.ts,
// publishedTestUsers.ts). A web app is opened and signed in to with one of
// the project's test users; an agent has no page of its own, so the
// platform's test app (apps/tryit) is its page, signing a person in as a test
// user on the project's identity provider; a service shows its API.

type ProjectRolesView = components["schemas"]["ProjectRolesView"];

/** One test account the platform holds the password for, so the console can reveal it. */
export interface TestLogin {
  username: string;
  roles: string[];
  /** The union of its roles' grants; empty when the directory could not be asked. */
  scopes: string[];
}

/** The project's test accounts the platform owns (and so can reveal), in the view's order. */
export function testLogins(view: Pick<ProjectRolesView, "testUsers"> | undefined): TestLogin[] {
  return (view?.testUsers ?? [])
    .filter((u) => u.owned)
    .map((u) => ({ username: u.username, roles: [...new Set(u.roles ?? [])], scopes: [...new Set(u.scopes ?? [])] }));
}

export type TryItKind = "web-app" | "agent" | "service";

/** How a component is tried, or null when it is not serving (nothing answers at its URL). */
export function tryItKind(component: ColumnComponent): TryItKind | null {
  if (component.kind !== "ready" || !component.url) return null;
  if (component.type === "web-application") return "web-app";
  if (component.type === "ai-agent") return "agent";
  if (component.type === "service") return "service";
  return null;
}

/** An environment, by its name and as people read it. */
export interface EnvironmentRef {
  name: string;
  label: string;
}

/**
 * Where one component can be tried, for the overview's Try it: every
 * environment it serves in, in promotion order. The first is the one Try it
 * opens on, since every build lands in the pipeline's first environment and
 * moves on from there, so the earliest environment a component serves in runs
 * its newest version. A component serving nowhere says why: deploying or
 * failed where it was deployed, running where nothing answers to try, or
 * deployed nowhere at all.
 */
export type ComponentTry =
  | { kind: "serving"; environments: EnvironmentRef[] }
  | { kind: "deploying" | "failed" | "running"; environment: EnvironmentRef }
  | { kind: "not-deployed" };

export function componentTry(columns: readonly EnvironmentColumn[], componentName: string): ComponentTry {
  const placed = columns.flatMap((column) => {
    const row = column.components.find((c) => c.name === componentName);
    return row?.deployment ? [{ environment: { name: column.name, label: column.label }, row }] : [];
  });
  const serving = placed.filter((p) => tryItKind(p.row) !== null).map((p) => p.environment);
  if (serving.length > 0) return { kind: "serving", environments: serving };
  for (const kind of ["failed", "converging", "ready"] as const) {
    const first = placed.find((p) => p.row.kind === kind);
    if (first) return { kind: kind === "converging" ? "deploying" : kind === "ready" ? "running" : kind, environment: first.environment };
  }
  return { kind: "not-deployed" };
}

/** Everything the test app needs to sign in to a project and reach one of its components; all of it public. */
export interface TryItLaunch {
  project: string;
  component: string;
  issuer: string;
  clientId: string;
  resource: string;
  scopes: readonly string[];
  endpoint: string;
}

/**
 * The test app's launch for an agent, when the project can be signed in to
 * from outside: a sign-in client, the resource server its grants are on, and
 * at least one test account. Null otherwise; the app could only say why not.
 */
export function agentLaunch(
  project: string,
  component: ColumnComponent,
  view: Pick<ProjectRolesView, "signIn" | "resourceServer" | "projectRoles" | "testUsers"> | undefined,
): TryItLaunch | null {
  const logins = testLogins(view);
  // The view's own answer, else any of the project's roles' (they all carry the same one).
  const resource = view?.resourceServer || view?.projectRoles?.find((r) => r.resourceServer)?.resourceServer;
  if (!view?.signIn || !resource || !component.url || logins.length === 0) return null;
  return {
    project,
    component: component.name,
    issuer: view.signIn.issuer,
    clientId: view.signIn.clientId,
    resource,
    // profile and email so the test app can show who is signed in.
    scopes: ["openid", "profile", "email", ...new Set(logins.flatMap((l) => l.scopes))],
    endpoint: component.url,
  };
}

/**
 * The launch URL: `{base}/#/agent?…`. Hash routing, so the identity
 * provider's redirect back to the app's `/callback` never needs the route.
 */
export function tryItAppUrl(base: string, launch: TryItLaunch): string {
  const query = new URLSearchParams({
    project: launch.project,
    component: launch.component,
    issuer: launch.issuer,
    client_id: launch.clientId,
    resource: launch.resource,
    scopes: launch.scopes.join(" "),
    endpoint: launch.endpoint,
  });
  return `${base.replace(/\/+$/, "")}/#/agent?${query.toString()}`;
}

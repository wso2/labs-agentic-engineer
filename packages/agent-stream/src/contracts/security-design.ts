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
 * SecurityDesign v3 — the AUTHORED `specs/design/security.json`. There is no
 * prose companion: this file is the whole security design.
 *
 * The permission **catalog** is the centre of the document. A project
 * owns one OAuth resource server; `permissions[]` declares its resources and
 * the actions on them, and every other section references those handles rather
 * than restating prose:
 *
 *  - `roles[].grants` names catalog handles (`<resource>:<action>`);
 *  - `openapi.yaml` operations name handles in their `security` block.
 *
 * Screens are NOT in this file. A screen's gate is a projection of the API
 * contract: a screen is reachable when the token holds the scope of the
 * operation that LOADS it, and that operation's one scope is already in
 * `openapi.yaml` (ADR-0033).
 *
 * It is read by two very different consumers:
 *
 *  - the **coding agent**, which implements the permissions it declares.
 *    Which ROWS an operation reaches is not in this file at all: it is the
 *    operation's PATH in `openapi.yaml` — under `/me/` the caller's, otherwise
 *    every row (ADR-0031). A handle says what a caller may do, never how far;
 *  - the **platform**, deterministically at build time (no model in the loop),
 *    which ensures the resource server, its actions, the project roles, the org
 *    groups and the test users exist on the identity provider before validation
 *    runs.
 *
 * Scope of the names. A **role** is project-owned: it becomes
 * `<project>/<name>` on the directory, so two projects naming the same role do
 * NOT mean the same role. A **group** is org-owned and shared: it is reused
 * when the directory already has it and only declared here when this project
 * introduces it. That split is why `roles[].description` is now rewritten on
 * every ensure while a group's is only seeded.
 *
 * **No secret ever appears here.** The file is committed to git and pinned into
 * the project's `v<N>` tag; a test user carries a username and role names and
 * nothing else. The platform generates the password at build and seals it.
 *
 * The Zod validator (`securityDesignSchema` in `../security-design-schema.ts`)
 * is drift-guarded against this type.
 */

/** The authored `specs/design/security.json`. */
export interface SecurityDesign {
  /**
   * Schema version. Pinned to the literal `3`, not widened to `number`: one
   * version exists at a time, and a stale `1` or `2` appearing here is caught
   * by the codec with a message naming what changed.
   */
  version: 3;
  /**
   * The permission catalog — every resource this project's services expose and
   * the actions on them. At least one. `resource` is unique within the project.
   */
  permissions: Permission[];
  /**
   * The org groups this project INTRODUCES. A group the directory already holds
   * (`list_groups`) is reused by naming it in `roles[].assignTo` and is not
   * redeclared here. Created if absent; never renamed or deleted by the
   * platform. May be empty when every `assignTo` reuses an existing group.
   */
  groups: Group[];
  /** Every role this project defines. At least one. */
  roles: Role[];
  /**
   * The accounts that exist so a role's behaviour can be exercised — the
   * validation agent signs in as one to judge role-gated criteria. May be
   * empty; the build supplies the users the design omits. A `service`-kind role
   * never gets one.
   */
  testUsers: TestUser[];
}

/** One resource in the catalog, and the actions callers may take on it. */
export interface Permission {
  /**
   * The resource name — one lowercase handle segment, unique within the
   * project (`claims`, `reports`). It is the first half of every handle on it.
   */
  resource: string;
  /**
   * The component that OWNS the resource, as it appears in `design.cell`. One
   * owner per resource; other components may call it but do not declare it.
   */
  component: string;
  /** What the resource is, for the console and the coding agent. */
  description?: string | undefined;
  /** The actions on this resource. At least one; handles unique per resource. */
  actions: Action[];
}

/** One action on a resource. `<resource>:<handle>` is the scope handle. */
export interface Action {
  /**
   * The action name — one lowercase handle segment (`read`, `read-all`,
   * `submit`). Unique within its resource; the same segment may legally appear
   * under a different resource (`claims:read` and `reports:read` coexist).
   *
   * An action carries no row axis. `read` and `read-all` are two actions
   * because they guard two operations — `GET /me/claims` and `GET /claims` —
   * and the path of each says which rows it reaches; nothing here does.
   */
  handle: string;
  /** What holding this action lets a caller do. */
  description?: string | undefined;
}

/** An org group this project introduces. */
export interface Group {
  /** The group name, verbatim, as the org directory holds it. */
  name: string;
  /**
   * What the group is. A CREATE-TIME SEED only: a group is org-owned and may
   * have been described by somebody else first, so the platform never rewrites
   * an existing group's description from here.
   */
  description: string;
}

/** How a person comes to hold a role. */
export type Enrolment = "admin" | "self-service";

/** What a role is assigned to. */
export type RoleKind = "user" | "service";

/** One project role and everything it may do. */
export interface Role {
  /**
   * The role name — a PRD actor noun, unique within the project
   * (case-insensitively). Becomes `<project>/<name>` on the directory, so it is
   * project-scoped and must never equal an org group name.
   */
  name: string;
  /**
   * What the role is for. Project-owned, so the platform writes it on every
   * ensure rather than seeding it once.
   */
  description: string;
  /** The PRD story numbers this role serves. At least one. */
  stories: number[];
  /**
   * Catalog handles (`<resource>:<action>`) this role holds. At least one.
   * Every handle must exist in `permissions[]`. A role holding `X:read-all`
   * also holds `X:read`: the "all" handle widens the ROWS, it does not replace
   * the operation.
   */
  grants: string[];
  /**
   * Org groups the role is assigned to. Each must be declared in `groups[]` or
   * already exist in the directory. Required for an `admin`-enrolment `user`
   * role; absent for a self-service role and for a `service` role.
   */
  assignTo?: string[] | undefined;
  /**
   * How a person comes to hold this role. `admin` (the default) means somebody
   * puts them in an `assignTo` group; `self-service` means the web app's
   * registration flow assigns it at account creation.
   */
  enrolment?: Enrolment | undefined;
  /**
   * Role names that may hand this role out, validated against `roles[]`.
   * Records who admits people; the in-app admin screen reads it.
   */
  assignableBy?: string[] | undefined;
  /**
   * `user` (the default) or `service`. A service role is assigned to an app
   * principal, never to a group, and gets no test user.
   */
  kind?: RoleKind | undefined;
}

/**
 * One test user. A username and role names, and nothing else, ever — a password
 * here would be committed to git.
 */
export interface TestUser {
  /**
   * The IdP username. Lowercase, so the platform's own generated names
   * (`test-<role-slug>`) and authored ones cannot collide by case alone.
   */
  username: string;
  /**
   * The roles this account holds. At least one, each a declared `user`-kind
   * role. The account is enrolled in every `assignTo` group of every role
   * listed.
   */
  roles: string[];
}

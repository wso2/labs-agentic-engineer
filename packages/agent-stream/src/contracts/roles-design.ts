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
 * RolesDesign — the AUTHORED `specs/design/roles.json`, the STRUCTURED half of
 * a project's security design.
 *
 * `security.md` keeps the prose half (role resolution, the policy narrative)
 * and names no role; this file declares which roles the project uses, what each
 * may do WITHIN this project, and its test users. Nothing appears in both, so
 * the two can never contradict each other.
 *
 * It is read by two very different consumers:
 *
 *  - the **coding agent**, which implements the permissions it declares;
 *  - the **platform**, deterministically at build time (no model in the loop),
 *    which ensures each role and test user exists on the identity provider
 *    before validation runs.
 *
 * Roles and test users are SHARED directory objects — their scope is the IdP's
 * scope, not the project's. Two projects naming the same role mean the same
 * role. This file therefore DECLARES roles; only the permissions it grants them
 * are this project's.
 *
 * **No secret ever appears here.** The file is committed to git and pinned into
 * the project's `v<N>` tag; a test user carries a username and a role and
 * nothing else. The platform generates the password at build and seals it.
 *
 * The Zod validator (`rolesDesignSchema` in `../roles-design-schema.ts`) is
 * drift-guarded against this type.
 */

export interface RolesDesign {
  /**
   * Schema version. Pinned to the literal `1`, not widened to `number`: only one
   * version exists, and a `2` appearing here should be a compile error at the
   * call site rather than something the runtime gate has to catch.
   */
  version: 1;
  /**
   * The role a caller holds before anyone grants them one, or `null` when a
   * caller with no role reaches nothing. Must name a declared role when set —
   * `security-design`'s cold-start rule, made mechanical.
   */
  coldStartRole: string | null;
  /**
   * Components that serve unauthenticated traffic. Absence of sign-in is a
   * decision, so it is written down rather than inferred from silence.
   */
  publicComponents: string[];
  /** Every role this project uses. At least one. */
  roles: RoleDeclaration[];
  /**
   * The accounts that exist so a role's behaviour can be exercised — the
   * validation agent signs in as one to judge role-gated criteria. Every role
   * needs at least one; the build supplies any the design omits.
   */
  testUsers: TestUserDeclaration[];
}

/** One role, and what it may do within this project. */
export interface RoleDeclaration {
  /**
   * The role name, verbatim — it becomes the IdP group name and reaches an app
   * as a `groups` claim. Reuse an existing catalog row's name rather than
   * minting a near-duplicate (`list_roles`).
   */
  name: string;
  /**
   * What the role is for. A CREATE-TIME SEED only: the platform never updates
   * an existing group's description from here, because a shared role may have
   * been described by somebody else first.
   */
  description: string;
  /** The PRD story numbers this role serves. At least one. */
  stories: number[];
  /**
   * How a person comes to hold this role: the name of the role that can grant
   * it, or `first sign-in` for the cold-start role. Prose, not a key — the
   * matrix answer to "who admits people".
   */
  grantedBy: string;
  /** What this role may do, per component. At least one entry. */
  permissions: RolePermission[];
}

/**
 * What one role may do on one component. `actions` for a service (verbs),
 * `screens` for a web application (reachable screens) — at least one of the two
 * is non-empty.
 */
export interface RolePermission {
  /** The component name, as it appears in `design.cell`. */
  component: string;
  // `| undefined` is explicit on both: the package compiles with
  // exactOptionalPropertyTypes, so `actions?: string[]` and
  // `actions?: string[] | undefined` are DIFFERENT types, and only the second
  // matches what Zod's `.optional()` infers. Without it the drift guard below
  // fails rather than passing — which is the guard working, not a nuisance.
  /** Allowed actions on a service component. */
  actions?: string[] | undefined;
  /** Reachable screens on a web-application component. */
  screens?: string[] | undefined;
}

/**
 * One test user. Username and role, and nothing else, ever — a password here
 * would be committed to git.
 */
export interface TestUserDeclaration {
  /**
   * The IdP username. Lowercase, so the platform's own generated names
   * (`test-<role-slug>`) and authored ones cannot collide by case alone.
   */
  username: string;
  /** The role this account holds. Must name a declared role. */
  role: string;
}

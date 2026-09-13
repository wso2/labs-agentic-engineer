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
 * Every sentence the security.json referential gate can say, keyed.
 *
 * Two gates apply this rule set — the agent's FileBundle write-gate (this
 * package) and `securityspec` in the BFF (Go) — and the design's contract is
 * that "a document that passes one passes the other". Two hand-kept copies of
 * twenty sentences would drift on the first reword, so the templates live here
 * once and are published as `security-design-messages.json` beside this file
 * (`pnpm --filter @aep/agent-stream gen`); Go vendors that artifact and a
 * parity test on each side asserts it still matches.
 *
 * A template is a flat string with `{placeholder}` slots. No nesting, no
 * pluralization, no conditionals: anything a template cannot say is said by
 * choosing a different key, so the Go formatter is a `strings.NewReplacer` and
 * nothing more.
 */

/**
 * key → template. The keys are the gate's stable vocabulary; renaming one is a
 * cross-language break, so add rather than rename.
 */
export const SECURITY_DESIGN_MESSAGES = {
  // --- the catalog ---------------------------------------------------------
  duplicate_resource:
    'resource "{resource}" is declared twice — a resource name identifies one resource within the project, and exactly one component owns it.',
  duplicate_action:
    'resource "{resource}" declares action "{handle}" twice — action handles are unique within their resource. ("claims:read" and "reports:read" legally coexist; "claims:read" twice does not.)',
  resource_component_unknown:
    'resource "{resource}" is owned by component "{component}", which specs/design/design.cell does not declare — the cell is the design\'s source of truth: add the node first, or name the service that really owns the resource.',

  // --- roles ---------------------------------------------------------------
  duplicate_role_name:
    'role "{role}" is declared twice — a role name identifies one role within the project (compared without case), so it appears once.',
  role_name_whitespace:
    'role "{role}" has leading or trailing whitespace — the name becomes "<project>/{role}" on the directory verbatim.',
  role_name_is_group_name:
    'role "{role}" is also declared in groups[] — a role is project-scoped (it becomes "<project>/{role}" on the directory) and a group is org-owned and shared, so one name cannot be both. Rename the role.',
  grant_unknown_handle:
    'role "{role}" grants "{handle}", which permissions[] does not declare — add the action to its resource, or grant a declared handle.',
  read_all_without_read:
    'role "{role}" grants "{allHandle}" without "{readHandle}" — the "all" handle widens the ROWS an operation returns, it does not replace the operation, and the gateway compares scopes whole-string. Grant both.',
  assignable_by_unknown_role:
    'role "{role}" names "{ref}" in assignableBy, which no roles[] entry declares — assignableBy names project roles, never org groups.',
  admin_role_needs_assign_to:
    'role "{role}" is an admin-enrolment user role with no assignTo — name at least one org group the role is assigned to (declare it in groups[] if this project introduces it), or set "enrolment": "self-service".',
  non_admin_role_has_assign_to:
    'role "{role}" is a {kind} role and carries assignTo — remove it: a {kind} role is never assigned to an org group.',
  assign_to_directory_checked:
    'role "{role}" is assigned to group "{group}", which groups[] does not declare — that is legal for a group the org directory already holds; the build gate resolves it against list_groups and refuses it there if it does not exist.',

  // --- test users ----------------------------------------------------------
  invalid_test_username:
    'test user "{username}" is not a usable directory username — use lowercase letters, digits, ".", "_" or "-", starting with a letter or digit.',
  duplicate_test_user: 'test user "{username}" is listed twice.',
  test_user_unknown_role:
    'test user "{username}" holds role "{role}", which no roles[] entry declares.',
  test_user_role_not_user_kind:
    'test user "{username}" holds role "{role}", which is not an admin-enrolment user role — a service role belongs to an app principal and a self-service role is taken at registration, so neither gets a test account.',

  // --- screens -------------------------------------------------------------
  screen_component_unknown:
    'screen "{screen}" names component "{component}", which specs/design/design.cell does not declare — a screens[] row belongs to a web application the cell draws.',
  screen_unknown:
    'component "{component}" has no screen "{screen}" — its wireframes.dsl declares no matching `screen` (names are compared ignoring case and punctuation). Declare the screen in the wireframe, or correct the name here.',
  screen_requires_unknown_handle:
    'screen "{screen}" of component "{component}" requires "{handle}", which permissions[] does not declare — require a declared handle, null for any signed-in user, or "public".',
  screen_operation_not_granted:
    'role "{role}" reaches screen "{screen}" but does not grant "{handle}", which an operation behind that screen requires — the screen loads into a 401 that the SPA cannot tell from an expired session. Grant "{handle}" to "{role}", or gate the screen on a handle "{role}" does not hold.',

  // --- warnings ------------------------------------------------------------
  handle_used_nowhere:
    'catalog handle "{handle}" is declared, used nowhere — no operation and no screen requires it. Remove it, or require it from the operation or screen it was meant to guard.',
  handle_unreachable:
    'catalog handle "{handle}" is unreachable by any role — an operation requires it and no roles[].grants holds it. Grant it to the role that needs it, or make the operation public.',

  // --- the version boundary ------------------------------------------------
  v1_document:
    "security.json v1 is not accepted: {fields}. Version 2 declares the permission catalog in permissions[] (resource, component, actions[{handle, ownership}]), roles grant handles from it in roles[].grants and name their org groups in assignTo, screens[] maps each wireframe screen to one handle, and testUsers[].roles is a list.",
} as const;

/** A key of the gate's message vocabulary. */
export type SecurityMessageKey = keyof typeof SECURITY_DESIGN_MESSAGES;

/** The values a template slot may be filled with. */
export type MessageParams = Readonly<Record<string, string | number>>;

const SLOT_RE = /\{([a-zA-Z][a-zA-Z0-9]*)\}/g;

/**
 * Render one message. A slot with no parameter is left verbatim rather than
 * blanked: `v1_document` embeds a literal `{handle, ownership}` from the
 * schema's own shape, and a gate message that silently loses a name is worse
 * than one that shows a brace.
 */
export function securityMessage(key: SecurityMessageKey, params: MessageParams = {}): string {
  const template: string = SECURITY_DESIGN_MESSAGES[key];
  return template.replace(SLOT_RE, (whole, slot: string) => {
    const value = params[slot];
    return value === undefined ? whole : String(value);
  });
}

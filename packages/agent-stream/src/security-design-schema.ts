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
 * Runtime validation for the AUTHORED `specs/design/security.json`, version 2
 * (`SecurityDesign` in `./contracts/security-design.ts` — the wire source of
 * truth; the Zod schema below is drift-guarded against it). The FileBundle
 * calls `checkSecurityDesign` on every write to that path, so the model gets a
 * one-round-trip self-correction instead of a build gate rejecting the file
 * three steps later.
 *
 * The schema is also published as JSON Schema (`./json-schema.ts`) and vendored
 * into the BFF, so the agent's write-gate and the platform's save-gate validate
 * ONE definition.
 *
 * **Where each rule lives.** Three layers, in the order a document meets them:
 *
 *  1. the Zod object shape — publishable as JSON Schema, so the Go gate gets it
 *     for free;
 *  2. Zod refinements on leaf strings (handle spelling, the `screens[].requires`
 *     literal). These are deliberately NOT `z.string().regex()`: the Go
 *     interpreter (`internal/platform/jsonschema`) does not implement `pattern`
 *     and IGNORES what it does not implement, so emitting one would leave the
 *     platform's gate silently validating less than the agent's. Expressed as
 *     refinements they are invisible to the JSON Schema and the Go side mirrors
 *     them in code, like every rule in layer 3;
 *  3. `checkSecurityReferences` — the cross-references no standalone schema can
 *     express (a grant naming a catalog handle, a test user naming a declared
 *     role), applied separately by BOTH sides.
 *
 * **v1 is not accepted.** A version-1 document fails with one message naming
 * the fields v2 removed, rather than a Zod dump about a `const` mismatch plus
 * five unknown keys.
 */

import { z } from "zod";
import type {
  SecurityDesign,
  Permission,
  Action,
  Group,
  Role,
  Screen,
  TestUser,
} from "./contracts/security-design.js";
import type { Equal } from "./type-equal.js";
import {
  checkSecurityReferences,
  type SecurityReferenceContext,
} from "./security-design-references.js";
import { securityMessage } from "./security-design-messages.js";

/**
 * A test-user username, as the IdP will hold it. Lowercase-only so an authored
 * name and a platform-generated `test-<role-slug>` cannot collide by case
 * alone, and restricted to characters that survive a URL path segment.
 */
export const TEST_USERNAME_RE = /^[a-z0-9][a-z0-9._-]*$/;

/**
 * One segment of a scope handle — a resource name or an action name. Lowercase
 * because the handle reaches an access token's `scope` claim verbatim and a
 * space-separated claim has no room for a quoting convention.
 */
export const HANDLE_SEGMENT_RE = /^[a-z][a-z0-9-]*$/;

/** `screens[].requires` for a screen shown before sign-in. */
export const PUBLIC_SCREEN = "public";

/** True when `value` is a full `<resource>:<action>` catalog handle. */
export function isHandle(value: string): boolean {
  const parts = value.split(":");
  return parts.length === 2 && parts.every((p) => HANDLE_SEGMENT_RE.test(p));
}

const HANDLE_HINT =
  'must be a catalog handle "<resource>:<action>", each half lowercase letters, digits or "-" starting with a letter';

// strictObject everywhere, so an unknown key is rejected at author time. That is
// also what keeps a secret out of the file mechanically: there is no `password`
// property to write, at any nesting level, and the file is committed to git and
// pinned into the version tag.

const handleSegment = z
  .string()
  .min(1)
  .refine((v) => HANDLE_SEGMENT_RE.test(v), {
    message: 'must be lowercase letters, digits or "-", starting with a letter',
  });

const actionSchema = z.strictObject({
  handle: handleSegment,
  ownership: z.enum(["own", "any"]),
  description: z.string().min(1).optional(),
});

const permissionSchema = z.strictObject({
  resource: handleSegment,
  component: z.string().min(1),
  description: z.string().min(1).optional(),
  actions: z.array(actionSchema).min(1),
});

const groupSchema = z.strictObject({
  name: z.string().min(1),
  description: z.string().min(1),
});

const roleSchema = z.strictObject({
  name: z.string().min(1),
  description: z.string().min(1),
  stories: z.array(z.number().int().positive()).min(1),
  grants: z
    .array(z.string().min(1).refine(isHandle, { message: HANDLE_HINT }))
    .min(1),
  assignTo: z.array(z.string().min(1)).min(1).optional(),
  enrolment: z.enum(["admin", "self-service"]).optional(),
  assignableBy: z.array(z.string().min(1)).min(1).optional(),
  kind: z.enum(["user", "service"]).optional(),
});

const screenSchema = z.strictObject({
  component: z.string().min(1),
  screen: z.string().min(1),
  requires: z
    .string()
    .min(1)
    .refine((v) => v === PUBLIC_SCREEN || isHandle(v), {
      message: `must be one catalog handle, null for any signed-in user, or the literal "${PUBLIC_SCREEN}"`,
    })
    .nullable(),
});

const testUserSchema = z.strictObject({
  username: z.string().min(1),
  roles: z.array(z.string().min(1)).min(1),
});

export const securityDesignSchema = z.strictObject({
  version: z.literal(2),
  permissions: z.array(permissionSchema).min(1),
  groups: z.array(groupSchema),
  roles: z.array(roleSchema).min(1),
  screens: z.array(screenSchema),
  testUsers: z.array(testUserSchema),
});

// Compile-time drift guards: schema ⇄ contracts wire types.
const _driftSecurity: Equal<z.infer<typeof securityDesignSchema>, SecurityDesign> = true;
const _driftPermission: Equal<z.infer<typeof permissionSchema>, Permission> = true;
const _driftAction: Equal<z.infer<typeof actionSchema>, Action> = true;
const _driftGroup: Equal<z.infer<typeof groupSchema>, Group> = true;
const _driftRole: Equal<z.infer<typeof roleSchema>, Role> = true;
const _driftScreen: Equal<z.infer<typeof screenSchema>, Screen> = true;
const _driftTestUser: Equal<z.infer<typeof testUserSchema>, TestUser> = true;
void _driftSecurity;
void _driftPermission;
void _driftAction;
void _driftGroup;
void _driftRole;
void _driftScreen;
void _driftTestUser;

/** Matches the one authored security document. */
export const SECURITY_DESIGN_JSON_RE = /^specs\/design\/security\.json$/;

export interface SecurityDesignProblem {
  code: "INVALID_JSON" | "SCHEMA_VIOLATION";
  message: string;
}

/**
 * The fields v2 removed, in the order the message lists them. Detected BEFORE
 * the schema runs so a v1 document gets one sentence about the migration
 * instead of a Zod dump in which the real cause (`version` is 1) is one issue
 * among six.
 */
const REMOVED_V1_FIELDS: { path: string; present: (doc: Record<string, unknown>) => boolean }[] = [
  { path: "coldStartRole", present: (d) => "coldStartRole" in d },
  { path: "publicComponents", present: (d) => "publicComponents" in d },
  { path: "thunder", present: (d) => "thunder" in d },
  { path: "roles[].grantedBy", present: (d) => someEntryHas(d["roles"], "grantedBy") },
  { path: "roles[].permissions", present: (d) => someEntryHas(d["roles"], "permissions") },
  { path: "testUsers[].role", present: (d) => someEntryHas(d["testUsers"], "role") },
];

function someEntryHas(value: unknown, key: string): boolean {
  return Array.isArray(value) && value.some((e) => typeof e === "object" && e !== null && key in e);
}

/**
 * The one-line refusal for a version-1 document, or null when the document is
 * not v1. "Is v1" means it says so, or it still carries a field only v1 had —
 * a half-migrated file is v1 too.
 */
function v1Refusal(parsed: unknown): string | null {
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return null;
  const doc = parsed as Record<string, unknown>;
  const removed = REMOVED_V1_FIELDS.filter((f) => f.present(doc)).map((f) => f.path);
  const saysV1 = doc["version"] === 1;
  if (!saysV1 && removed.length === 0) return null;
  const carries =
    removed.length > 0
      ? `remove ${removed.join(", ")}`
      : "re-author it against version 2";
  return securityMessage("v1_document", { fields: carries });
}

// The referential rules themselves live in `./security-design-references.ts`:
// they are the only part of this gate that reads sibling spec files, and they
// are the part the BFF mirrors rule for rule. Re-exported so every existing
// caller keeps importing the gate from one module.
export {
  checkSecurityReferences,
  securityReferenceFindings,
  normalizeScreenName,
} from "./security-design-references.js";
export type {
  SecurityReferenceFinding,
  SecurityReferenceContext,
  SecurityFindingSeverity,
} from "./security-design-references.js";

/**
 * Validate a candidate security.json body for `path`. Returns null when the
 * path is not the security document or the content is valid; otherwise the
 * problem, phrased for the model's self-correction.
 */
export function checkSecurityDesign(
  path: string,
  content: string,
  ctx?: SecurityReferenceContext,
): SecurityDesignProblem | null {
  if (!SECURITY_DESIGN_JSON_RE.test(path)) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(content);
  } catch (e) {
    return {
      code: "INVALID_JSON",
      message: `${path} is not valid JSON: ${e instanceof Error ? e.message : String(e)}. Re-emit the whole file.`,
    };
  }

  const legacy = v1Refusal(parsed);
  if (legacy) return { code: "SCHEMA_VIOLATION", message: `${path}: ${legacy}` };

  const res = securityDesignSchema.safeParse(parsed);
  if (!res.success) {
    const issues = res.error.issues
      .map((i) => `${i.path.join(".") || "(root)"}: ${i.message}`)
      .join("; ");
    return { code: "SCHEMA_VIOLATION", message: `${path} violates the SecurityDesign schema — ${issues}.` };
  }

  const refProblem = checkSecurityReferences(res.data, ctx);
  if (refProblem) {
    return { code: "SCHEMA_VIOLATION", message: `${path}: ${refProblem}` };
  }
  return null;
}

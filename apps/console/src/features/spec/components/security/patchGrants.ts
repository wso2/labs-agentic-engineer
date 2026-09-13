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
 * The one edit the Security page makes to `security.json`: a handle joins or
 * leaves one role's `grants`.
 *
 * **Why this patches the raw parse and not the validated document.** A
 * `SecurityDesign` that has been through the schema is a NEW object: its keys
 * are in the schema's declaration order, and any field the schema does not
 * model is gone. Serialising that back would rewrite the whole file on a
 * one-cell edit — a diff the author did not ask for, landing in a CRDT room
 * where somebody else may be typing in another part of the same document.
 * `JSON.parse` keeps the authored key order and every key, so the only thing
 * that moves is the array this function is named after.
 *
 * The re-serialisation still normalises WHITESPACE to the on-disk form (two
 * spaces, trailing newline) — the same form `serializeSecurityDesign` and the
 * design agent's own write produce, so a document written by either round-trips
 * byte-identically. `patchGrants.test.ts` holds that claim against the fixture.
 *
 * Nothing here is widened or repaired. Granting `X:read-all` does not add
 * `X:read`: that implication is a gate RULE, reported as a finding on the page,
 * and applying it silently here would make the page disagree with both the
 * gate and the gateway (decision B1).
 */

/** Why a patch could not be applied. The caller shows it; it never throws. */
export type PatchFailure =
  | { kind: "unreadable"; message: string }
  | { kind: "no-such-role"; role: string };

export type PatchResult =
  | { ok: true; text: string }
  | { ok: false; failure: PatchFailure };

interface RawRole {
  name?: unknown;
  grants?: unknown;
}

/**
 * `text` with `handle` present in (or absent from) `role`'s grants.
 *
 * Idempotent: granting what is already granted returns the document unchanged,
 * which keeps a double-click from writing an identical version into the room.
 */
export function patchGrants(
  text: string,
  role: string,
  handle: string,
  granted: boolean,
): PatchResult {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (e) {
    return {
      ok: false,
      failure: {
        kind: "unreadable",
        message: e instanceof Error ? e.message : String(e),
      },
    };
  }
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return {
      ok: false,
      failure: { kind: "unreadable", message: "not a JSON object" },
    };
  }
  const roles = (raw as { roles?: unknown }).roles;
  if (!Array.isArray(roles)) {
    return {
      ok: false,
      failure: { kind: "unreadable", message: "roles[] is missing" },
    };
  }
  const key = role.toLowerCase();
  const target = roles.find(
    (r): r is RawRole =>
      typeof r === "object" &&
      r !== null &&
      typeof (r as RawRole).name === "string" &&
      ((r as RawRole).name as string).toLowerCase() === key,
  );
  if (!target) return { ok: false, failure: { kind: "no-such-role", role } };

  const current = Array.isArray(target.grants)
    ? target.grants.filter((g): g is string => typeof g === "string")
    : [];
  const has = current.includes(handle);
  if (has === granted) return { ok: true, text };
  // Appended rather than sorted into catalog order: the array's order is not
  // rendered anywhere, and re-ordering it would put lines in the diff that the
  // author's one click did not ask for.
  target.grants = granted
    ? [...current, handle]
    : current.filter((g) => g !== handle);

  return { ok: true, text: `${JSON.stringify(raw, null, 2)}\n` };
}

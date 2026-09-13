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

import { describe, expect, it } from "vitest";

import { patchGrants } from "./patchGrants";
import { EXPENSE_TRACKER_TEXT } from "./securityTestFixtures";

describe("patchGrants — the one edit the Security page makes", () => {
  it("leaves the rest of the document byte-identical", () => {
    const before = EXPENSE_TRACKER_TEXT;
    const after = patchGrants(before, "Employee", "reports:read", true);
    expect(after.ok).toBe(true);
    if (!after.ok) return;

    // Everything except the one array is untouched, character for character.
    const strip = (text: string) =>
      text.replace(/"grants": \[[^\]]*\]/g, '"grants": []');
    expect(strip(after.text)).toBe(strip(before));
  });

  // The claim `patchGrants`'s doc comment makes: a document already in the
  // on-disk form survives a no-op patch unchanged. If this ever fails, the page
  // is rewriting files it was only asked to read.
  it("round-trips a no-op byte-identically", () => {
    const granted = patchGrants(
      EXPENSE_TRACKER_TEXT,
      "Employee",
      "claims:read",
      true,
    );
    expect(granted).toEqual({ ok: true, text: EXPENSE_TRACKER_TEXT });
  });

  it("keeps the authored key order rather than the schema's", () => {
    const source = `{\n  "roles": [\n    {\n      "grants": [],\n      "name": "Admin",\n      "description": "d",\n      "stories": [1]\n    }\n  ],\n  "version": 2\n}\n`;
    const result = patchGrants(source, "Admin", "orders:read", true);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // "roles" still before "version"; "grants" still before "name".
    expect(result.text.indexOf('"roles"')).toBeLessThan(
      result.text.indexOf('"version"'),
    );
    expect(result.text.indexOf('"grants"')).toBeLessThan(
      result.text.indexOf('"name"'),
    );
  });

  it("keeps fields the console's schema does not model", () => {
    const source = `{\n  "version": 2,\n  "somethingNewer": { "kept": true },\n  "roles": [\n    { "name": "Admin", "grants": [] }\n  ]\n}\n`;
    const result = patchGrants(source, "Admin", "orders:read", true);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.text).toContain('"somethingNewer"');
    expect(result.text).toContain('"kept": true');
  });

  it("adds and removes exactly one handle", () => {
    const added = patchGrants(EXPENSE_TRACKER_TEXT, "Employee", "reports:read", true);
    expect(added.ok).toBe(true);
    if (!added.ok) return;
    expect(rolesOf(added.text)["Employee"]).toEqual([
      "claims:read",
      "claims:submit",
      "reports:read",
    ]);
    expect(rolesOf(added.text)["Approver"]).toEqual(
      rolesOf(EXPENSE_TRACKER_TEXT)["Approver"],
    );

    const removed = patchGrants(added.text, "Employee", "claims:submit", false);
    expect(removed.ok).toBe(true);
    if (!removed.ok) return;
    expect(rolesOf(removed.text)["Employee"]).toEqual([
      "claims:read",
      "reports:read",
    ]);
  });

  // The "all" handle widens rows, it does not replace the operation. Nothing
  // here repairs the document — the page shows the gate's warning instead.
  it("does not add X:read when X:read-all is granted", () => {
    const result = patchGrants(
      EXPENSE_TRACKER_TEXT,
      "Employee",
      "claims:read-all",
      true,
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(rolesOf(result.text)["Employee"]).toEqual([
      "claims:read",
      "claims:submit",
      "claims:read-all",
    ]);
  });

  it("matches the role name without case, and reports one it cannot find", () => {
    expect(
      patchGrants(EXPENSE_TRACKER_TEXT, "employee", "reports:read", true).ok,
    ).toBe(true);
    expect(
      patchGrants(EXPENSE_TRACKER_TEXT, "Nobody", "reports:read", true),
    ).toEqual({ ok: false, failure: { kind: "no-such-role", role: "Nobody" } });
  });

  // `roleSchema.grants` is `min(1)`: a role with an empty array is a document
  // the console reads back as "empty or incomplete", which would replace the
  // matrix with an info box and take away the cell that could undo the edit.
  it("refuses to take away a role's last grant", () => {
    const oneGrant = EXPENSE_TRACKER_TEXT.replace(
      '"claims:read",\n        "claims:submit"',
      '"claims:submit"',
    );
    expect(oneGrant).not.toBe(EXPENSE_TRACKER_TEXT);

    expect(patchGrants(oneGrant, "Employee", "claims:submit", false)).toEqual({
      ok: false,
      failure: { kind: "last-grant", role: "Employee" },
    });
    // And nothing else about the role is blocked.
    expect(patchGrants(oneGrant, "Employee", "claims:read", true).ok).toBe(true);
  });

  it("still removes a grant from a role that holds more than one", () => {
    const result = patchGrants(
      EXPENSE_TRACKER_TEXT,
      "Employee",
      "claims:submit",
      false,
    );
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(rolesOf(result.text)["Employee"]).toEqual(["claims:read"]);
  });

  it("reports an unreadable document instead of throwing", () => {
    const result = patchGrants('{"version": 2,', "Employee", "claims:read", true);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.failure.kind).toBe("unreadable");
  });
});

function rolesOf(text: string): Record<string, string[]> {
  const doc = JSON.parse(text) as {
    roles: { name: string; grants: string[] }[];
  };
  return Object.fromEntries(doc.roles.map((r) => [r.name, r.grants]));
}

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

// The canonical documents, imported as text from where the write gate's own
// tests keep them. `?raw` rather than a JSON import so nothing here depends on
// `resolveJsonModule`, and so the console's parser is what turns the bytes
// into a document — which is itself the assertion that it can read them.
import clinicJson from "../../../../../../packages/agent-stream/test/fixtures/security/clinic.json?raw";
import expenseTrackerJson from "../../../../../../packages/agent-stream/test/fixtures/security/expense-tracker.json?raw";
import vendorJson from "../../../../../../packages/agent-stream/test/fixtures/security/vendor.json?raw";

import {
  baselineScreens,
  grantsOf,
  isGranted,
  needsTestUser,
  parseSecurityDesign,
  plannedUsersFor,
  planUsers,
  referencePaths,
  roleEnrolment,
  roleKind,
  roleSlug,
  rolesGranting,
  securityMatrix,
  serializeSecurityDesign,
  suppliedUsernameFor,
  type SecurityDesign,
} from "./securityDesign";

type Role = SecurityDesign["roles"][number];

function role(name: string): Role {
  return {
    name,
    description: `What ${name} may do`,
    stories: [1],
    grants: ["orders:read"],
  };
}

function doc(over: Partial<SecurityDesign> = {}): SecurityDesign {
  return {
    version: 2,
    permissions: [
      {
        resource: "orders",
        component: "orders-api",
        actions: [{ handle: "read", ownership: "own" }],
      },
    ],
    groups: [],
    roles: [role("Admin"), role("Viewer")],
    screens: [],
    testUsers: [],
    ...over,
  };
}

/** Fully populated document for parse and planUsers round-trip tests. */
function richDoc(): SecurityDesign {
  return {
    version: 2,
    permissions: [
      {
        resource: "orders",
        component: "orders-api",
        description: "Customer orders",
        actions: [
          { handle: "read", ownership: "own", description: "See own orders" },
          { handle: "read-all", ownership: "any" },
        ],
      },
    ],
    groups: [{ name: "Staff", description: "Everyone on payroll" }],
    roles: [
      {
        name: "Admin",
        description: "What Admin may do",
        stories: [1, 2],
        grants: ["orders:read", "orders:read-all"],
        assignTo: ["Staff"],
        assignableBy: ["Admin"],
      },
      {
        name: "Viewer",
        description: "What Viewer may do",
        stories: [3],
        grants: ["orders:read"],
        enrolment: "self-service",
      },
    ],
    screens: [
      { component: "storefront", screen: "Orders", requires: "orders:read" },
      { component: "storefront", screen: "Catalog", requires: "public" },
      { component: "storefront", screen: "My account", requires: null },
    ],
    testUsers: [{ username: "test-admin", roles: ["Admin", "Viewer"] }],
  };
}

/** A complete version-1 document — the previous schema, not a half-written one. */
const V1_DOCUMENT = JSON.stringify({
  version: 1,
  coldStartRole: null,
  publicComponents: [],
  roles: [
    {
      name: "Admin",
      description: "What Admin may do",
      stories: [1],
      grantedBy: "an administrator",
      permissions: [{ component: "orders-api", actions: ["read"] }],
    },
  ],
  testUsers: [{ username: "ada", role: "Admin" }],
  thunder: { name: "orders-app", type: "browser" },
});

describe("parseSecurityDesign", () => {
  // A design with no sign-in legitimately has no security document. "Empty" is a
  // state the panel puts into words; "invalid" is an error it shows in red.
  it.each([
    ["null", null],
    ["undefined", undefined],
    ["empty", ""],
    ["whitespace only", "  \n\t  "],
  ])("reads %s as empty rather than as a failure", (_label, text) => {
    expect(parseSecurityDesign(text)).toEqual({ kind: "empty" });
  });

  // The room streams this file in a line at a time, so most of the text a
  // reader sees mid-turn is a PREFIX. Calling that broken would raise an alarm
  // about a document nothing is wrong with.
  it.each([
    ["a truncated object", '{"version": 2,'],
    ["a key with no value yet", '{"version": 2, "permissions"'],
    ["an unterminated string", '{"version": 2, "roles": [{"name": "Admi'],
    ["an array still open", '{"version": 2, "roles": ['],
  ])("reads %s as unfinished rather than as a failure", (_label, text) => {
    expect(parseSecurityDesign(text)).toEqual({ kind: "unfinished" });
  });

  // Balanced, and still not JSON: no further typing repairs these, so the
  // reader is owed the error.
  it.each([
    ["a closer that does not match its opener", '{"roles": [1, 2}'],
    ["one closer too many", '{"version": 2}}'],
    ["text after the document", '{"version": 2} and then some'],
  ])("reports %s as invalid", (_label, text) => {
    const parsed = parseSecurityDesign(text);
    expect(parsed.kind).toBe("invalid");
    if (parsed.kind !== "invalid") throw new Error("unreachable");
    expect(parsed.message).not.toBe("");
  });

  it.each([
    ["empty object", "{}"],
    ["JSON array", "[]"],
  ])("reads %s as empty rather than as a failure", (_label, text) => {
    expect(parseSecurityDesign(text)).toEqual({ kind: "empty" });
  });

  it("reads well-formed JSON missing required fields as empty", () => {
    const bad = {
      ...doc(),
      roles: [{ ...role("Admin"), description: undefined }],
    };
    expect(parseSecurityDesign(JSON.stringify(bad))).toEqual({ kind: "empty" });
  });

  // The schema is strict, but an unknown key means incomplete, not unparseable.
  it("reads an unknown top-level key as empty", () => {
    const parsed = parseSecurityDesign(
      JSON.stringify({ ...doc(), password: "hunter2" }),
    );
    expect(parsed).toEqual({ kind: "empty" });
  });

  // A v1 file is FINISHED — it is the previous schema, not a draft — so the
  // panel has to say what happened rather than claim the document is empty.
  it("refuses a version-1 document with the write gate's migration sentence", () => {
    const parsed = parseSecurityDesign(V1_DOCUMENT);
    expect(parsed.kind).toBe("invalid");
    if (parsed.kind !== "invalid") throw new Error("unreachable");
    expect(parsed.message).toContain("security.json v1 is not accepted");
    expect(parsed.message).toContain("coldStartRole");
    expect(parsed.message).toContain("permissions[]");
    // The gate names the path it checked; the panel already does.
    expect(parsed.message).not.toContain("specs/design/security.json");
  });

  it("accepts a well-formed document and hands back the parsed shape", () => {
    const good = richDoc();
    const parsed = parseSecurityDesign(serializeSecurityDesign(good));
    expect(parsed).toEqual({ kind: "ok", doc: good });
  });

  it("accepts a document with no groups, screens or test users", () => {
    const good = doc();
    const parsed = parseSecurityDesign(serializeSecurityDesign(good));
    expect(parsed).toEqual({ kind: "ok", doc: good });
  });
});

describe("plannedUsersFor", () => {
  it("returns the authored users of a role, none of them supplied", () => {
    const d = doc({
      testUsers: [
        { username: "ada", roles: ["Admin"] },
        { username: "grace", roles: ["Admin"] },
        { username: "vera", roles: ["Viewer"] },
      ],
    });
    expect(plannedUsersFor(d, "Admin")).toEqual([
      { username: "ada", role: "Admin", supplied: false },
      { username: "grace", role: "Admin", supplied: false },
    ]);
  });

  // v2's test user holds a LIST of roles, and the panel lists users inside role
  // cards — so one account satisfies every role it names.
  it("counts a user holding several roles under each of them", () => {
    const d = doc({ testUsers: [{ username: "ada", roles: ["Admin", "Viewer"] }] });
    expect(plannedUsersFor(d, "Admin")).toEqual([
      { username: "ada", role: "Admin", supplied: false },
    ]);
    expect(plannedUsersFor(d, "Viewer")).toEqual([
      { username: "ada", role: "Viewer", supplied: false },
    ]);
  });

  it("gives a role with no authored user exactly one supplied test-<slug>", () => {
    const d = doc({ testUsers: [{ username: "ada", roles: ["Admin"] }] });
    expect(plannedUsersFor(d, "Viewer")).toEqual([
      { username: "test-viewer", role: "Viewer", supplied: true },
    ]);
  });

  // `securityspec.Role.NeedsTestUser`: only an admin-enrolment USER role owes a
  // login. Promising a `test-…` name for the other two would name an account
  // the build never creates.
  it("supplies no user for a service role", () => {
    const d = doc({ roles: [role("Admin"), { ...role("Ledger Sync"), kind: "service" }] });

    expect(plannedUsersFor(d, "Ledger Sync")).toEqual([]);
    expect(planUsers(d)).toEqual([
      { username: "test-admin", role: "Admin", supplied: true },
    ]);
  });

  it("supplies no user for a self-service role", () => {
    const d = doc({
      roles: [role("Admin"), { ...role("Shopper"), enrolment: "self-service" }],
    });

    expect(plannedUsersFor(d, "Shopper")).toEqual([]);
    expect(planUsers(d)).toEqual([
      { username: "test-admin", role: "Admin", supplied: true },
    ]);
  });

  // The ordinal is the DECLARED role's index on both sides, so a role that owes
  // no login still consumes one — skipping it here would hand a colliding role
  // a different suffix than the build creates.
  it("counts skipped roles in the ordinal the collision suffix uses", () => {
    const d = doc({
      roles: [
        { ...role("Ledger Sync"), kind: "service" },
        role("Ops Support"),
        role("Ops/Support"),
      ],
      testUsers: [{ username: "test-ops-support", roles: ["Ops Support"] }],
    });

    // `Ops/Support` is the THIRD declared role, so its disambiguated name is
    // `-3` — the service role ahead of it still counts.
    expect(planUsers(d).map((u) => u.username)).toEqual([
      "test-ops-support",
      "test-ops-support-3",
    ]);
  });

  it("matches a test user to its role case-insensitively", () => {
    const d = doc({ testUsers: [{ username: "ada", roles: ["aDmIn"] }] });
    expect(plannedUsersFor(d, "Admin")).toEqual([
      { username: "ada", role: "Admin", supplied: false },
    ]);
    // …and the lookup itself is case-insensitive from either side.
    expect(plannedUsersFor(d, "ADMIN")).toEqual([
      { username: "ada", role: "Admin", supplied: false },
    ]);
  });
});

describe("roleSlug", () => {
  it.each([
    ["Compliance Admin", "compliance-admin"],
    ["Ops/Support", "ops-support"],
    ["  Spaced  Name  ", "spaced-name"],
    ["ADMIN", "admin"],
    ["a--b", "a-b"],
  ])("slugs %j into %j", (name, expected) => {
    expect(roleSlug(name)).toBe(expected);
  });

  // A username must start with a letter or digit, so an empty slug cannot be
  // allowed to produce the bare "test-".
  it('falls back to "role" for a name that slugs to nothing', () => {
    expect(roleSlug("!!!")).toBe("role");
    expect(roleSlug("   ")).toBe("role");
    expect(suppliedUsernameFor(doc({ roles: [role("!!!")] }), "!!!")).toBe(
      "test-role",
    );
  });
});

describe("suppliedUsernameFor", () => {
  it("builds the name from the role slug", () => {
    const d = doc({ roles: [role("Compliance Admin"), role("Ops/Support")] });
    expect(suppliedUsernameFor(d, "Compliance Admin")).toBe(
      "test-compliance-admin",
    );
    expect(suppliedUsernameFor(d, "Ops/Support")).toBe("test-ops-support");
  });
});

/**
 * The panel promises the user a name before Build runs, and the build has to
 * produce that same name. The generator therefore exists twice — here and as
 * `securityspec.supplyUsername` in `services/aep-api/internal/platform/securityspec`
 * — and the two disagreeing is a real defect, not a cosmetic one: the panel
 * would show a login that never appears.
 *
 * The expectations below are the OBSERVED output of the Go `securityspec.Plan`
 * for the same documents, transcribed and restated on the v2 shape (only the
 * test users' `role` → `roles` changed; the generator itself did not). Change
 * one side and this goes red.
 */
describe("suppliedUsernameFor agrees with the Go build's securityspec.supplyUsername", () => {
  // Two DISTINCT role names that slug identically. The schema's uniqueness rule
  // is on the NAME, so this document is legal, and the Go build resolves it by
  // adding each generated name to its taken set as it mints it. A TS generator
  // that only consulted the AUTHORED users would promise both roles
  // `test-ops-support` while the build actually created `-support` and
  // `-support-2` — one role would show a login that never gets created.
  it("suffixes the second of two roles whose names slug identically, as Go does", () => {
    const d = doc({
      roles: [role("Ops Support"), role("Ops/Support")],
      testUsers: [],
    });
    expect(planUsers(d).map((u) => u.username)).toEqual([
      "test-ops-support",
      "test-ops-support-2",
    ]);
    expect(suppliedUsernameFor(d, "Ops/Support")).toBe("test-ops-support-2");
  });

  it("suffixes the role ordinal when an authored user of ANOTHER role holds the natural name", () => {
    // Go: Plan → [{test-viewer Admin} {test-viewer-2 Viewer supplied}]
    const d = doc({ testUsers: [{ username: "test-viewer", roles: ["Admin"] }] });
    expect(suppliedUsernameFor(d, "Viewer")).toBe("test-viewer-2");
  });

  it("uses ordinal+1, so the first declared role suffixes -1 and not -0", () => {
    // Go: Plan → [{test-admin-1 Admin supplied} {test-admin Viewer}]
    const d = doc({ testUsers: [{ username: "test-admin", roles: ["Viewer"] }] });
    expect(suppliedUsernameFor(d, "Admin")).toBe("test-admin-1");
  });

  it("leaves the natural name alone when nothing has taken it", () => {
    // Go: Plan → [{test-compliance-admin …} {test-ops-support …}]
    const d = doc({ roles: [role("Compliance Admin"), role("Ops/Support")] });
    expect(suppliedUsernameFor(d, "Compliance Admin")).toBe(
      "test-compliance-admin",
    );
    expect(suppliedUsernameFor(d, "Ops/Support")).toBe("test-ops-support");
  });

  it("is unaffected by the case a test user spells its role in", () => {
    // Go: Plan → [{alice Admin}] — the authored user satisfies the role, so
    // nothing is supplied at all.
    const d = doc({
      roles: [role("Admin")],
      testUsers: [{ username: "alice", roles: ["admin"] }],
    });
    expect(plannedUsersFor(d, "Admin")).toEqual([
      { username: "alice", role: "Admin", supplied: false },
    ]);
  });
});

// ---------------------------------------------------------------------------
// The matrix model
// ---------------------------------------------------------------------------

/**
 * The three canonical security documents, read from where the write gate's own
 * tests keep them rather than copied here. They are the documents the design's
 * Console pictures are drawn from, so a selector that gets one of them wrong
 * draws the wrong page — and a copy would drift the moment the gate's fixtures
 * were corrected.
 */
const CANONICAL: Record<"expense-tracker" | "clinic" | "vendor", string> = {
  "expense-tracker": expenseTrackerJson,
  clinic: clinicJson,
  vendor: vendorJson,
};

function canonical(name: keyof typeof CANONICAL): SecurityDesign {
  const parsed = parseSecurityDesign(CANONICAL[name]);
  if (parsed.kind !== "ok") {
    throw new Error(
      `canonical fixture ${name}.json did not parse: ${JSON.stringify(parsed)}`,
    );
  }
  return parsed.doc;
}

describe("roleKind / roleEnrolment", () => {
  it("applies the document's defaults, which are what the build applies", () => {
    const plain = role("Admin");
    expect(roleKind(plain)).toBe("user");
    expect(roleEnrolment(plain)).toBe("admin");
    expect(needsTestUser(plain)).toBe(true);
  });

  it("reads the stated kind and enrolment", () => {
    const service = { ...role("Ledger Sync"), kind: "service" as const };
    const shopper = { ...role("Shopper"), enrolment: "self-service" as const };
    expect(roleKind(service)).toBe("service");
    expect(roleEnrolment(shopper)).toBe("self-service");
    expect(needsTestUser(service)).toBe(false);
    expect(needsTestUser(shopper)).toBe(false);
  });
});

describe("securityMatrix", () => {
  it("groups every action under its resource, carrying the owning component", () => {
    const matrix = securityMatrix(canonical("expense-tracker"));

    expect(
      matrix.groups.map((g) => ({
        resource: g.resource,
        component: g.component,
        actions: g.rows.map((r) => r.action),
      })),
    ).toEqual([
      {
        resource: "claims",
        component: "expense-api",
        actions: ["read", "read-all", "submit", "approve", "reject"],
      },
      { resource: "reports", component: "expense-api", actions: ["read", "export"] },
    ]);
  });

  it("carries the handle, ownership and prose of each action", () => {
    const [claims] = securityMatrix(canonical("expense-tracker")).groups;
    expect(claims?.description).toBe("Expense claims and their approval");
    expect(claims?.rows[0]).toEqual({
      handle: "claims:read",
      resource: "claims",
      action: "read",
      component: "expense-api",
      ownership: "own",
      description: "See own claims",
      grantedBy: ["Employee", "Approver"],
    });
    expect(claims?.rows[1]?.ownership).toBe("any");
  });

  // The document's `description` is optional and the schema forbids "", so an
  // absent one is "" here and the renderer needs no third state.
  it('reads an unauthored description as ""', () => {
    const [, reports] = securityMatrix(canonical("expense-tracker")).groups;
    expect(reports?.description).toBe("");
  });

  it("scores each row with the roles that grant it, in declaration order", () => {
    const matrix = securityMatrix(canonical("expense-tracker"));
    const rows = matrix.groups.flatMap((g) => g.rows);
    expect(
      Object.fromEntries(rows.map((r) => [r.handle, r.grantedBy])),
    ).toEqual({
      "claims:read": ["Employee", "Approver"],
      "claims:read-all": ["Approver"],
      "claims:submit": ["Employee"],
      "claims:approve": ["Approver"],
      "claims:reject": ["Approver"],
      "reports:read": ["Approver"],
      // The design's "⚠ used nowhere" row: no role grants it.
      "reports:export": [],
    });
  });

  it("makes the columns the user-kind roles, in declaration order", () => {
    const matrix = securityMatrix(canonical("expense-tracker"));
    expect(matrix.columns.map((c) => c.name)).toEqual(["Employee", "Approver"]);
    expect(matrix.serviceColumns).toEqual([]);
    expect(matrix.columns[0]).toMatchObject({ kind: "user", enrolment: "admin" });
  });

  // A self-service role is user-kind: how somebody comes to hold it changes the
  // role card, not what the role may do, so it is still a column.
  it("keeps a self-service role as a column and says so", () => {
    const matrix = securityMatrix(canonical("clinic"));
    expect(matrix.columns.map((c) => c.name)).toEqual([
      "Patient",
      "Receptionist",
      "Doctor",
    ]);
    expect(matrix.columns[0]).toMatchObject({
      name: "Patient",
      kind: "user",
      enrolment: "self-service",
    });
  });

  it("splits a service role out of the columns but still scores its grants", () => {
    const matrix = securityMatrix(canonical("vendor"));
    expect(matrix.columns.map((c) => c.name)).toEqual([
      "Buyer",
      "Supplier",
      "Finance",
    ]);
    expect(matrix.serviceColumns.map((c) => c.name)).toEqual([
      "reconciliation-job",
    ]);

    const rows = matrix.groups.flatMap((g) => g.rows);
    const invoicesRead = rows.find((r) => r.handle === "invoices:read");
    expect(invoicesRead?.grantedBy).toEqual([
      "Buyer",
      "Supplier",
      "Finance",
      "reconciliation-job",
    ]);
  });

  it("carries the role declaration on the column, for the card it heads", () => {
    const matrix = securityMatrix(canonical("expense-tracker"));
    expect(matrix.columns[1]?.role.assignTo).toEqual(["Finance"]);
    expect(matrix.columns[1]?.role.assignableBy).toEqual(["Approver"]);
  });

  it("has no rows and no columns for a document with neither", () => {
    // Not reachable through the schema (it requires one of each), but the
    // matrix is a fold and must not assume its input is non-empty.
    const matrix = securityMatrix({ ...doc(), permissions: [], roles: [] });
    expect(matrix.groups).toEqual([]);
    expect(matrix.columns).toEqual([]);
  });
});

describe("isGranted", () => {
  it("answers the cell for a column and a row", () => {
    const matrix = securityMatrix(canonical("expense-tracker"));
    const readAll = matrix.groups[0]?.rows[1];
    const [employee, approver] = matrix.columns;
    if (!readAll || !employee || !approver) throw new Error("unreachable");

    expect(isGranted(readAll, employee)).toBe(false);
    expect(isGranted(readAll, approver)).toBe(true);
  });
});

describe("baselineScreens", () => {
  // The three kinds of `requires` in one document: a handle, `null` and the
  // literal. Only the last two are baseline rows.
  it("separates the signed-in screens from the public ones", () => {
    expect(baselineScreens(richDoc())).toEqual({
      signedInScreens: [{ component: "storefront", screen: "My account" }],
      publicScreens: [{ component: "storefront", screen: "Catalog" }],
    });
  });

  it("reads the clinic's one public screen and no signed-in screen", () => {
    expect(baselineScreens(canonical("clinic"))).toEqual({
      signedInScreens: [],
      publicScreens: [{ component: "booking-site", screen: "Find a slot" }],
    });
  });

  // The design's "any signed-in user · GET /me" row is only half derivable
  // here: this document declares screens and never operations, so a document
  // whose every screen carries a handle has an EMPTY baseline even when its
  // components expose unscoped operations. That half comes from each
  // component's openapi.yaml, which the panel reads separately.
  it("is empty for a document whose every screen requires a handle", () => {
    expect(baselineScreens(canonical("expense-tracker"))).toEqual({
      signedInScreens: [],
      publicScreens: [],
    });
  });

  it("is reachable through the matrix as well", () => {
    expect(securityMatrix(richDoc()).baseline).toEqual(baselineScreens(richDoc()));
  });
});

describe("grantsOf / rolesGranting", () => {
  it("returns a role's handles as authored, widening nothing", () => {
    const d = canonical("expense-tracker");
    expect(grantsOf(d, "Employee")).toEqual(["claims:read", "claims:submit"]);
    // `claims:read-all` does NOT imply `claims:read`: the gateway matches
    // scopes exactly, so the authored list is the whole truth.
    expect(grantsOf(d, "Approver")).toEqual([
      "claims:read",
      "claims:read-all",
      "claims:approve",
      "claims:reject",
      "reports:read",
    ]);
  });

  it("looks a role up case-insensitively and reads an unknown one as empty", () => {
    const d = canonical("expense-tracker");
    expect(grantsOf(d, "eMpLoYeE")).toEqual(grantsOf(d, "Employee"));
    expect(grantsOf(d, "Nobody")).toEqual([]);
  });

  it("inverts the lookup, in declaration order, service roles included", () => {
    expect(rolesGranting(canonical("expense-tracker"), "claims:read")).toEqual([
      "Employee",
      "Approver",
    ]);
    expect(rolesGranting(canonical("vendor"), "payments:read")).toEqual([
      "Finance",
      "reconciliation-job",
    ]);
  });

  it("reads a handle nothing grants, and an unknown handle, as empty", () => {
    expect(rolesGranting(canonical("expense-tracker"), "reports:export")).toEqual([]);
    expect(rolesGranting(canonical("expense-tracker"), "no:such")).toEqual([]);
  });
});

describe("referencePaths", () => {
  it("asks for the cell, the owners' contracts and the screens' wireframes", () => {
    expect(referencePaths(canonical("expense-tracker"))).toEqual([
      "specs/design/design.cell",
      "specs/design/components/expense-api/openapi.yaml",
      "specs/design/components/expense-api/openapi.yml",
      "specs/design/components/expense-webapp/wireframes.dsl",
    ]);
  });

  // Two components own resources and two more carry screens: every one of them
  // is a file some rule reads, and none of them is asked for twice.
  it("covers every component named, once each", () => {
    expect(referencePaths(canonical("clinic"))).toEqual([
      "specs/design/design.cell",
      "specs/design/components/appointments-api/openapi.yaml",
      "specs/design/components/appointments-api/openapi.yml",
      "specs/design/components/booking-site/wireframes.dsl",
      "specs/design/components/staff-webapp/wireframes.dsl",
    ]);
    expect(referencePaths(canonical("vendor"))).toEqual([
      "specs/design/design.cell",
      "specs/design/components/orders-api/openapi.yaml",
      "specs/design/components/orders-api/openapi.yml",
      "specs/design/components/payments-api/openapi.yaml",
      "specs/design/components/payments-api/openapi.yml",
      "specs/design/components/vendor-webapp/wireframes.dsl",
    ]);
  });

  // A component that only carries screens has no contract to judge the catalog
  // against, and a component that only owns resources has no screens.
  it("does not ask for a contract from a screen-only component", () => {
    const paths = referencePaths(canonical("expense-tracker"));
    expect(paths).not.toContain(
      "specs/design/components/expense-webapp/openapi.yaml",
    );
    expect(paths).not.toContain(
      "specs/design/components/expense-api/wireframes.dsl",
    );
  });

  it("asks for the cell alone when the document names no screens", () => {
    expect(referencePaths({ ...doc(), screens: [], permissions: [] })).toEqual([
      "specs/design/design.cell",
    ]);
  });
});

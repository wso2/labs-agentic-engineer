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
 * The referential rules of `specs/design/security.json` v2 — one passing case
 * per design document, one failing case per rule.
 *
 * The three documents under `fixtures/security/` are the design's own worked
 * examples. Each now comes with the sibling files its cross-checks need — a
 * `design.cell`, the owning components' `openapi.yaml`, the web apps'
 * `wireframes.dsl` — assembled into a bundle at the paths the real spec tree
 * uses. All three must come out CLEAN, so a rule that would refuse one of the
 * design's own documents fails here rather than in a live run.
 *
 * The Go save-gate (`internal/platform/securityspec`) mirrors this rule set and
 * formats the same vendored `security-design-messages.json`, so a rule added
 * here without a Go counterpart is a divergence its parity test must catch.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  securityReferenceFindings,
  checkSecurityReferences,
  normalizeScreenName,
  type SecurityReferenceFinding,
  type SecurityReferenceContext,
} from "../src/security-design-references.ts";
import { checkSecurityDesign } from "../src/security-design-schema.ts";
import { SECURITY_DESIGN_MESSAGES, securityMessage } from "../src/security-design-messages.ts";
import type { SecurityDesign } from "../src/contracts/security-design.ts";

const PATH = "specs/design/security.json";
const FIXTURES = ["expense-tracker", "clinic", "vendor"] as const;
type FixtureName = (typeof FIXTURES)[number];

// --- the fixture bundles -----------------------------------------------------

function fixtureText(name: FixtureName): string {
  return readFileSync(new URL(`./fixtures/security/${name}.json`, import.meta.url), "utf8");
}

function fixtureDoc(name: FixtureName): SecurityDesign {
  return JSON.parse(fixtureText(name)) as SecurityDesign;
}

/**
 * The sibling spec files of one worked example, at the paths the real tree
 * uses. On disk they are flat (`expense-api.openapi.yaml`) so the fixture
 * directory reads as a list; here they are folded back into
 * `specs/design/components/<component>/…`.
 */
function bundleFiles(name: FixtureName): Record<string, string> {
  const dir = new URL(`./fixtures/security/${name}/`, import.meta.url);
  const files: Record<string, string> = {};
  for (const entry of readdirSync(fileURLToPath(dir))) {
    const body = readFileSync(new URL(entry, dir), "utf8");
    if (entry === "design.cell") {
      files["specs/design/design.cell"] = body;
      continue;
    }
    const m = /^(.+)\.(openapi\.yaml|wireframes\.dsl)$/.exec(entry);
    if (!m) throw new Error(`unexpected fixture file ${name}/${entry}`);
    files[`specs/design/components/${m[1]}/${m[2]}`] = body;
  }
  return files;
}

function contextFor(name: FixtureName, extra: Record<string, string> = {}): SecurityReferenceContext {
  const files = { ...bundleFiles(name), ...extra };
  return { read: (path) => files[path] };
}

// --- finding helpers ---------------------------------------------------------

const errors = (found: SecurityReferenceFinding[]): SecurityReferenceFinding[] =>
  found.filter((f) => f.severity === "error");

function only(found: SecurityReferenceFinding[], severity: SecurityReferenceFinding["severity"]) {
  return found.filter((f) => f.severity === severity);
}

/** The one error the document has, asserted by key and by the slots it named. */
function assertOnlyError(
  found: SecurityReferenceFinding[],
  key: keyof typeof SECURITY_DESIGN_MESSAGES,
  params: Record<string, string>,
): void {
  const es = errors(found);
  assert.deepEqual(
    es.map((e) => e.key),
    [key],
    `expected exactly one ${key}, got: ${es.map((e) => e.message).join(" | ")}`,
  );
  assert.equal(es[0]!.message, securityMessage(key, params));
}

/** The Expense Tracker document with `mutate` applied to a deep copy. */
function expense(mutate: (doc: SecurityDesign) => void): SecurityDesign {
  const doc = fixtureDoc("expense-tracker");
  mutate(doc);
  return doc;
}

function role(doc: SecurityDesign, name: string): SecurityDesign["roles"][number] {
  const found = doc.roles.find((r) => r.name === name);
  if (!found) throw new Error(`fixture has no role ${name}`);
  return found;
}

// --- the design's own documents come out clean -------------------------------

for (const name of FIXTURES) {
  test(`the design's ${name} document has no referential error against its own bundle`, () => {
    const found = securityReferenceFindings(fixtureDoc(name), contextFor(name));
    assert.deepEqual(
      errors(found).map((e) => e.message),
      [],
    );
    assert.equal(checkSecurityDesign(PATH, fixtureText(name), contextFor(name)), null);
  });
}

test("the Expense Tracker's reused org group is an info note, not a refusal", () => {
  const found = securityReferenceFindings(fixtureDoc("expense-tracker"), contextFor("expense-tracker"));
  const info = only(found, "info");
  assert.deepEqual(
    info.map((f) => f.params),
    [{ role: "Approver", group: "Finance" }],
  );
  // The Vendor Portal reuses `Finance` as a ROLE name for the same reason: it
  // is not in this document's groups[], so nothing collides.
  assert.equal(
    errors(securityReferenceFindings(fixtureDoc("vendor"), contextFor("vendor"))).length,
    0,
  );
});

test("the two coverage warnings fire on the Expense Tracker catalog", () => {
  const found = securityReferenceFindings(fixtureDoc("expense-tracker"), contextFor("expense-tracker"));
  const warnings = only(found, "warning").map((f) => `${f.key}:${f.params["handle"]}`);
  // reports:export is required by GET /reports/export and granted by no role.
  assert.ok(warnings.includes("handle_unreachable:reports:export"), warnings.join(", "));
  // claims:read-all guards no operation, but it widens a read that IS used —
  // the design's own shape, so it must not be reported as unused.
  assert.ok(!warnings.some((w) => w.startsWith("handle_used_nowhere:claims:read-all")), warnings.join(", "));
});

test("a catalog handle nothing requires is reported as declared, used nowhere", () => {
  const doc = expense((d) => {
    d.permissions[0]!.actions.push({ handle: "archive", ownership: "any" });
  });
  const warnings = only(securityReferenceFindings(doc, contextFor("expense-tracker")), "warning");
  assert.ok(warnings.some((w) => w.key === "handle_used_nowhere" && w.params["handle"] === "claims:archive"));
});

// --- THE reachability cross-check (plan §1.2, Δ P6 §5) -----------------------

test("the design's own Expense Tracker defect is refused: Approver reaches Approvals without claims:read", () => {
  // Verbatim the shape the design shipped and P6 §5 measured: Approvals is
  // gated on claims:approve, the list it renders is GET /claims (claims:read),
  // and Approver grants only claims:read-all. Live, that was a 401 the SPA read
  // as an expired session — an infinite sign-in loop.
  const doc = expense((d) => {
    const approver = role(d, "Approver");
    approver.grants = approver.grants.filter((g) => g !== "claims:read");
  });
  const found = errors(securityReferenceFindings(doc, contextFor("expense-tracker")));
  const reach = found.find((f) => f.key === "screen_operation_not_granted");
  if (!reach) throw new Error(`no reachability error; got: ${found.map((f) => f.message).join(" | ")}`);
  assert.deepEqual(reach.params, { role: "Approver", screen: "Approvals", handle: "claims:read" });
  assert.match(reach.message, /role "Approver" reaches screen "Approvals"/);
  assert.match(reach.message, /claims:read/);
  // Decision B1 names the same omission from the other side.
  assert.ok(found.some((f) => f.key === "read_all_without_read"));
});

test("the reachability rule is silent when the owning component's spec is not in the bundle", () => {
  const doc = expense((d) => {
    const approver = role(d, "Approver");
    approver.grants = approver.grants.filter((g) => g !== "claims:read");
  });
  const files = bundleFiles("expense-tracker");
  delete files["specs/design/components/expense-api/openapi.yaml"];
  const found = errors(securityReferenceFindings(doc, { read: (p) => files[p] }));
  assert.ok(!found.some((f) => f.key === "screen_operation_not_granted"));
});

test("a service role reaches no screen, so the reachability rule skips it", () => {
  // The Vendor Portal's reconciliation-job grants invoices:read and
  // payments:read; it must not be judged against the screens those handles gate.
  const doc = fixtureDoc("vendor");
  const job = role(doc, "reconciliation-job");
  job.grants = ["invoices:read-all", "invoices:read", "payments:read"];
  const found = errors(securityReferenceFindings(doc, contextFor("vendor")));
  assert.deepEqual(found, []);
});

// --- screen names normalize on both sides ------------------------------------

test('a screen name normalizes on both sides — "My Claims" is the DSL\'s MyClaims', () => {
  assert.equal(normalizeScreenName("My Claims"), normalizeScreenName("MyClaims"));
  const doc = expense((d) => {
    d.screens[0]!.screen = "my-claims!";
  });
  assert.deepEqual(errors(securityReferenceFindings(doc, contextFor("expense-tracker"))), []);
});

test("a screen the wireframe does not declare is refused", () => {
  const doc = expense((d) => {
    d.screens[0]!.screen = "My Claim";
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "screen_unknown", {
    component: "expense-webapp",
    screen: "My Claim",
  });
});

test("a screen rule is silent when the component has no wireframes.dsl in the bundle", () => {
  const files = bundleFiles("expense-tracker");
  delete files["specs/design/components/expense-webapp/wireframes.dsl"];
  const doc = expense((d) => {
    d.screens[0]!.screen = "My Claim";
  });
  assert.deepEqual(errors(securityReferenceFindings(doc, { read: (p) => files[p] })), []);
});

// --- one negative per rule ---------------------------------------------------

test("a grant naming a handle the catalog does not declare", () => {
  const doc = expense((d) => {
    role(d, "Employee").grants = ["claims:read", "claims:archive"];
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "grant_unknown_handle", {
    role: "Employee",
    handle: "claims:archive",
  });
});

test("read-all without read, named from both ends (decision B1)", () => {
  const doc = expense((d) => {
    const approver = role(d, "Approver");
    approver.grants = ["claims:read-all", "claims:approve", "claims:reject", "reports:read"];
    // Take the Approvals screen out so only the B1 rule speaks.
    d.screens = d.screens.filter((s) => s.requires !== "claims:approve");
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "read_all_without_read", {
    role: "Approver",
    allHandle: "claims:read-all",
    readHandle: "claims:read",
  });
});

test("assignableBy naming something that is not a declared role", () => {
  const doc = expense((d) => {
    role(d, "Approver").assignableBy = ["Finance"];
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "assignable_by_unknown_role",
    { role: "Approver", ref: "Finance" },
  );
});

test("an admin-enrolment user role with no assignTo", () => {
  const doc = expense((d) => {
    delete role(d, "Employee").assignTo;
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "admin_role_needs_assign_to",
    { role: "Employee" },
  );
});

test("a self-service role carrying assignTo", () => {
  const doc = expense((d) => {
    const employee = role(d, "Employee");
    employee.enrolment = "self-service";
    d.testUsers = d.testUsers.filter((u) => !u.roles.includes("Employee"));
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "non_admin_role_has_assign_to",
    { role: "Employee", kind: "self-service" },
  );
});

test("a service role carrying assignTo", () => {
  const doc = expense((d) => {
    const employee = role(d, "Employee");
    employee.kind = "service";
    d.testUsers = d.testUsers.filter((u) => !u.roles.includes("Employee"));
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "non_admin_role_has_assign_to",
    { role: "Employee", kind: "service" },
  );
});

test("a test user naming a role nobody declared", () => {
  const doc = expense((d) => {
    d.testUsers[0]!.roles = ["Auditor"];
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "test_user_unknown_role", {
    username: "test-employee",
    role: "Auditor",
  });
});

test("a test user for a role that is not an admin-enrolment user role", () => {
  const doc = expense((d) => {
    const employee = role(d, "Employee");
    employee.enrolment = "self-service";
    delete employee.assignTo;
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "test_user_role_not_user_kind",
    { username: "test-employee", role: "Employee" },
  );
});

test("the same test username twice", () => {
  const doc = expense((d) => {
    d.testUsers.push({ username: "test-employee", roles: ["Approver"] });
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "duplicate_test_user", {
    username: "test-employee",
  });
});

test("a test username the directory cannot hold", () => {
  const doc = expense((d) => {
    d.testUsers[0]!.username = "Test.Employee";
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "invalid_test_username", {
    username: "Test.Employee",
  });
});

test("a resource declared twice", () => {
  const doc = expense((d) => {
    d.permissions.push({
      resource: "claims",
      component: "expense-api",
      actions: [{ handle: "archive", ownership: "any" }],
    });
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "duplicate_resource", {
    resource: "claims",
  });
});

test("an action handle declared twice on ONE resource — while the same handle on two resources is legal", () => {
  const clean = securityReferenceFindings(fixtureDoc("expense-tracker"), contextFor("expense-tracker"));
  assert.deepEqual(errors(clean), []); // claims:read and reports:read coexist
  const doc = expense((d) => {
    d.permissions[0]!.actions.push({ handle: "read", ownership: "any" });
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "duplicate_action", {
    resource: "claims",
    handle: "read",
  });
});

test("a resource owned by a component the cell does not draw", () => {
  const doc = expense((d) => {
    d.permissions[1]!.component = "expense-worker";
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "resource_component_unknown",
    { resource: "reports", component: "expense-worker" },
  );
});

test("a quoted component id resolves — the cell grammar reads the cell, not a regex", () => {
  // `component "My API" service` is ONE token to the cell's own tokenizer, and
  // the platform's `---` frontmatter is not a statement. A hand-rolled
  // /^component\s+(\S+)/ read `"My` and refused a document that names the
  // component correctly, so this rule resolves through `cellNodeIds`.
  const cell = `---
schemaVersion: 1
---
cell "Expense Tracker" {
  component expense-webapp as "Expense Web App" webapp
  component expense-api as "Expense API" service
  component "My API" as "My API" service
  component expense-db as "Expense DB" database

  expense-webapp -> expense-api
  expense-api -> expense-db
}
`;
  const doc = expense((d) => {
    d.permissions[1]!.component = "My API";
  });
  const found = securityReferenceFindings(
    doc,
    contextFor("expense-tracker", { "specs/design/design.cell": cell }),
  );
  assert.deepEqual(
    errors(found).map((e) => e.message),
    [],
  );
});

test("a role name declared twice, compared without case", () => {
  const doc = expense((d) => {
    d.roles.push({ ...role(d, "Employee"), name: "employee" });
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "duplicate_role_name", {
    role: "employee",
  });
});

test("a role name that is also a group name in THIS document", () => {
  const doc = expense((d) => {
    role(d, "Employee").name = "Employees";
    d.testUsers[0]!.roles = ["Employees"];
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "role_name_is_group_name",
    { role: "Employees" },
  );
});

test("a role name carrying whitespace the directory would keep", () => {
  const doc = expense((d) => {
    role(d, "Employee").name = " Employee ";
    d.testUsers[0]!.roles = [" Employee "];
  });
  assertOnlyError(securityReferenceFindings(doc, contextFor("expense-tracker")), "role_name_whitespace", {
    role: " Employee ",
  });
});

test("a screen on a component the cell does not draw", () => {
  const doc = expense((d) => {
    d.screens[0]!.component = "expense-admin";
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "screen_component_unknown",
    { component: "expense-admin", screen: "My Claims" },
  );
});

test("a screen requiring a handle the catalog does not declare", () => {
  const doc = expense((d) => {
    d.screens[2]!.requires = "claims:archive";
  });
  assertOnlyError(
    securityReferenceFindings(doc, contextFor("expense-tracker")),
    "screen_requires_unknown_handle",
    { component: "expense-webapp", screen: "Approvals", handle: "claims:archive" },
  );
});

// --- what a missing sibling does ---------------------------------------------

test("with no bundle at all, only the rules the document can answer alone run", () => {
  const doc = expense((d) => {
    d.permissions[1]!.component = "expense-worker";
    d.screens[0]!.component = "expense-admin";
    d.screens[0]!.screen = "Nowhere";
  });
  // The info note about a reused org group is all that survives: every rule
  // with a sibling-file input is skipped in silence.
  assert.deepEqual(
    securityReferenceFindings(doc).map((f) => f.key),
    ["assign_to_directory_checked"],
  );
  assert.equal(checkSecurityReferences(doc), null);
});

test("checkSecurityReferences returns the FIRST error, and nothing for a clean document", () => {
  assert.equal(checkSecurityReferences(fixtureDoc("clinic"), contextFor("clinic")), null);
  const doc = expense((d) => {
    role(d, "Employee").grants = ["claims:read", "claims:archive"];
  });
  assert.equal(
    checkSecurityReferences(doc, contextFor("expense-tracker")),
    securityMessage("grant_unknown_handle", { role: "Employee", handle: "claims:archive" }),
  );
});

// --- the message catalog -----------------------------------------------------

test("every message key renders every slot it declares", () => {
  for (const [key, template] of Object.entries(SECURITY_DESIGN_MESSAGES)) {
    const slots = [...template.matchAll(/\{([a-zA-Z][a-zA-Z0-9]*)\}/g)].map((m) => m[1]!);
    const params = Object.fromEntries(slots.map((s) => [s, `<${s}>`]));
    const rendered = securityMessage(key as keyof typeof SECURITY_DESIGN_MESSAGES, params);
    for (const slot of slots) {
      assert.ok(rendered.includes(`<${slot}>`), `${key} dropped {${slot}}`);
      assert.ok(!rendered.includes(`{${slot}}`), `${key} left {${slot}} unfilled`);
    }
  }
});

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
 * The security half of the `openapi.yaml` write-gate.
 *
 * The positive case is not a hand-made specimen: it is the Expense API spec
 * that was hand-written and served live behind the API Platform gateway with
 * per-operation scope policies, checked against the same `security.json` the
 * referential gate's fixtures use. Every negative below is
 * one mutation of that document, so a rule that starts rejecting real specs
 * shows up as the positive test failing rather than as a live 401.
 *
 * Each negative names a message KEY from `src/openapi-security-messages.json`
 * in its title — that key list is the vocabulary the Go mirror (`securityspec`)
 * codes against, so a rename is visible here.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { checkOpenapiSpec } from "../src/openapi-spec.ts";
import { FileBundle } from "../src/bundle.ts";
import { OPENAPI_SECURITY_MESSAGES } from "../src/openapi-security-messages.ts";
import type { DiagramBundleReader } from "../src/design-diagrams.ts";

const SPEC_PATH = "specs/design/components/expense-api/openapi.yaml";
const DESIGN_PATH = "specs/design/components/expense-api/design.json";
const SECURITY_PATH = "specs/design/security.json";

const EXPENSE_API_SPEC = readFileSync(new URL("./fixtures/openapi/expense-api.yaml", import.meta.url), "utf8");
/** The component design.json that carries the sign-in dependency (`thunder-app`). */
const PROTECTED_DESIGN = readFileSync(
  new URL("./fixtures/openapi/expense-api.design.json", import.meta.url),
  "utf8",
);
/** The design's own worked example, shared with the security.json gate's fixtures. */
const CATALOG = readFileSync(
  new URL("./fixtures/security/expense-tracker.json", import.meta.url),
  "utf8",
);

/** Same component, no sign-in dependency: every operation is public. */
const UNPROTECTED_DESIGN = JSON.stringify({
  name: "expense-api",
  type: "service",
  version: "0.1.0",
  dependencies: [],
});

/** A catalog whose `notifications` resource belongs to a DIFFERENT component. */
const CATALOG_WITH_FOREIGN_RESOURCE = JSON.stringify({
  version: 2,
  permissions: [
    ...(JSON.parse(CATALOG) as { permissions: unknown[] }).permissions,
    {
      resource: "notifications",
      component: "notify-api",
      actions: [{ handle: "send", ownership: "any", description: "Send a notification" }],
    },
  ],
  groups: [],
  roles: [],
  screens: [],
  testUsers: [],
});

function reader(files: Record<string, string>): DiagramBundleReader {
  return { read: (p) => files[p] };
}

/** Run the gate over `spec` with the given siblings in the bundle. */
function gate(
  spec: string,
  files: Record<string, string> = { [DESIGN_PATH]: PROTECTED_DESIGN, [SECURITY_PATH]: CATALOG },
) {
  return checkOpenapiSpec(SPEC_PATH, spec, reader(files));
}

/** One literal substitution that MUST match, so a fixture reword fails loudly. */
function mutate(spec: string, from: string, to: string): string {
  assert.ok(spec.includes(from), `fixture no longer contains ${JSON.stringify(from)}`);
  return spec.replace(from, to);
}

// -------------------------------------------------------------------------
// The published artifact
// -------------------------------------------------------------------------

test("openapi-security-messages.json is in step with the templates it publishes", () => {
  // Go vendors the JSON; this package renders from the TS. A reword that lands
  // in one and not the other would give the two gates two wordings of one rule.
  const published = JSON.parse(
    readFileSync(new URL("../src/openapi-security-messages.json", import.meta.url), "utf8"),
  ) as Record<string, string>;
  assert.deepEqual(published, { ...OPENAPI_SECURITY_MESSAGES });
});

// -------------------------------------------------------------------------
// The positive
// -------------------------------------------------------------------------

test("the P6 expense-api spec passes against the expense-tracker catalog", () => {
  assert.equal(gate(EXPENSE_API_SPEC), null);
});

test("the same spec passes with security.json absent — catalog rules narrow, they do not block", () => {
  // The design lineup writes security.json before the per-component artifacts,
  // but a re-emitted spec must not be refused for a file that is not there yet.
  assert.equal(gate(EXPENSE_API_SPEC, { [DESIGN_PATH]: PROTECTED_DESIGN }), null);
});

test("a stale handle is NOT caught when security.json is absent, but the structure still is", () => {
  const stale = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]");
  assert.equal(gate(stale, { [DESIGN_PATH]: PROTECTED_DESIGN }), null);

  const twoScopes = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]");
  assert.match(gate(twoScopes, { [DESIGN_PATH]: PROTECTED_DESIGN })!.message, /more than one scope/);
});

test("design.json absent means the premise is unknowable — no security verdict at all", () => {
  // Also the back-compatibility of every structural test: a bundle holding only
  // the spec gets exactly the gate it had before.
  const noScheme = mutate(EXPENSE_API_SPEC, "security:\n  - oauth2: []\n", "");
  assert.equal(gate(noScheme, { [SECURITY_PATH]: CATALOG }), null);
  assert.equal(checkOpenapiSpec(SPEC_PATH, noScheme), null);
});

// -------------------------------------------------------------------------
// The scheme and the document default
// -------------------------------------------------------------------------

test("missing_oauth2_scheme: a component behind sign-in must declare the scheme", () => {
  const noScheme = mutate(
    EXPENSE_API_SPEC,
    `  securitySchemes:
    oauth2:
      type: oauth2`,
    `  securitySchemes: {}
  _removed:
    oauth2:
      type: oauth2`,
  );
  const problem = gate(noScheme);
  assert.equal(problem?.code, "INVALID_OPENAPI");
  assert.match(problem!.message, /components\.securitySchemes\.oauth2 is absent/);
  assert.match(problem!.message, /one that does not declares none/);
});

test("scheme_wrong_type: the scheme must be type oauth2", () => {
  const wrong = mutate(EXPENSE_API_SPEC, "    oauth2:\n      type: oauth2", "    oauth2:\n      type: http");
  assert.match(gate(wrong)!.message, /declared as type: http/);
});

test("missing_document_security: the document default is `security: [{oauth2: []}]`", () => {
  const noDefault = mutate(EXPENSE_API_SPEC, "\nsecurity:\n  - oauth2: []\n", "\n");
  const problem = gate(noDefault);
  assert.match(problem!.message, /not the document-level default/);
  // The literal brace pair the model must write survives the slot formatter.
  assert.match(problem!.message, /security: \[\{oauth2: \[\]\}\]/);
});

// A default that NAMES a scope reads as "every operation needs this
// permission", but nothing enforces that: an operation with no `security` block
// of its own is projected onto the gateway as plain signed-in, so the scope is
// silently dropped and the operation ships open to any signed-in caller. The
// only default that says what is enforced is the empty one.
test("missing_document_security: a default naming a scope is refused", () => {
  const scoped = mutate(EXPENSE_API_SPEC, "\nsecurity:\n  - oauth2: []\n", "\nsecurity:\n  - oauth2: [claims:read]\n");
  const problem = gate(scoped);
  assert.equal(problem?.code, "INVALID_OPENAPI");
  assert.match(problem!.message, /not the document-level default/);
  assert.match(problem!.message, /EMPTY scope list/);
});

// -------------------------------------------------------------------------
// A component with no sign-in dependency declares nothing
// -------------------------------------------------------------------------

test("scheme_without_dependency: no sign-in dependency, no security scheme", () => {
  const problem = gate(EXPENSE_API_SPEC, { [DESIGN_PATH]: UNPROTECTED_DESIGN, [SECURITY_PATH]: CATALOG });
  assert.equal(problem?.code, "INVALID_OPENAPI");
  assert.match(problem!.message, /remove components\.securitySchemes/);
});

const BARE_SPEC = `openapi: 3.0.3
info:
  title: Expense API
  version: 1.0.0
paths:
  /claims:
    get:
      operationId: listClaims
      responses:
        "200":
          description: ok
`;

test("document_security_without_dependency: a root security block with no dependency", () => {
  const withDefault = BARE_SPEC.replace("paths:", "security:\n  - oauth2: []\npaths:");
  const problem = gate(withDefault, { [DESIGN_PATH]: UNPROTECTED_DESIGN });
  assert.match(problem!.message, /remove the document-level `security` block/);
});

test("security_without_dependency: an operation-level security block with no dependency", () => {
  const withOp = BARE_SPEC.replace(
    "      operationId: listClaims",
    "      operationId: listClaims\n      security: []",
  );
  const problem = gate(withOp, { [DESIGN_PATH]: UNPROTECTED_DESIGN });
  assert.match(problem!.message, /GET \/claims must declare no security/);
});

// -------------------------------------------------------------------------
// One requirement object, one scope
// -------------------------------------------------------------------------

test("operation_multiple_requirements: two requirement objects (OpenAPI anyOf) are refused", () => {
  const anyOf = mutate(
    EXPENSE_API_SPEC,
    "        - oauth2: [claims:approve]",
    "        - oauth2: [claims:approve]\n        - oauth2: [claims:reject]",
  );
  const problem = gate(anyOf);
  assert.match(problem!.message, /more than one security requirement object/);
  assert.match(problem!.message, /at most one requirement object with at most one scope/);
});

test("operation_multiple_scopes: two scopes in one requirement object are refused", () => {
  const two = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]");
  assert.match(gate(two)!.message, /POST \/claims names more than one scope/);
});

test("operation_unknown_scheme: an operation may only name the oauth2 scheme", () => {
  const other = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- bearerAuth: [claims:approve]");
  assert.match(gate(other)!.message, /secured with the scheme `bearerAuth`/);
});

test("operation_security_not_a_list: a scalar `security` is refused", () => {
  const scalar = mutate(
    EXPENSE_API_SPEC,
    "      security:\n        - oauth2: [claims:approve]",
    "      security: oauth2",
  );
  assert.match(gate(scalar)!.message, /is not a list/);
});

test("an operation that spells the document default out (`oauth2: []`) is accepted", () => {
  // "at most one requirement object with at most one scope" — zero scopes means
  // exactly what inheriting means, and the gateway renders the same policy.
  const explicit = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: []");
  assert.equal(gate(explicit), null);
});

// -------------------------------------------------------------------------
// Every scope is a catalog handle this component owns
// -------------------------------------------------------------------------

test("scope_not_in_catalog: a stale handle no longer in security.json", () => {
  const stale = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]");
  const problem = gate(stale);
  assert.match(problem!.message, /requires the scope `claims:archive`/);
  assert.match(problem!.message, /reference catalog handles; they never define them/);
});

test("scope_not_owned: a handle whose resource another component owns", () => {
  const foreign = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: [notifications:send]");
  const problem = gate(foreign, {
    [DESIGN_PATH]: PROTECTED_DESIGN,
    [SECURITY_PATH]: CATALOG_WITH_FOREIGN_RESOURCE,
  });
  assert.match(problem!.message, /the catalog assigns to component `notify-api`/);
});

test("flow_scope_not_in_catalog: an advertised flow scope the catalog does not declare", () => {
  const bogus = mutate(
    EXPENSE_API_SPEC,
    "            reports:read: Monthly totals",
    "            reports:read: Monthly totals\n            claims:archive: Archive a claim",
  );
  assert.match(gate(bogus)!.message, /advertises the scope `claims:archive` in flows/);
});

test("flow_scope_not_owned: an advertised flow scope another component owns", () => {
  const foreign = mutate(
    EXPENSE_API_SPEC,
    "            reports:read: Monthly totals",
    "            reports:read: Monthly totals\n            notifications:send: Send a notification",
  );
  const problem = gate(foreign, {
    [DESIGN_PATH]: PROTECTED_DESIGN,
    [SECURITY_PATH]: CATALOG_WITH_FOREIGN_RESOURCE,
  });
  assert.match(problem!.message, /`notifications:send` in flows, whose resource the catalog assigns to component `notify-api`/);
});

// -------------------------------------------------------------------------
// The OIDC scopes ride every token — refused anywhere
// -------------------------------------------------------------------------

test("reserved_oidc_scope: `openid` as an operation scope fails wide open", () => {
  const wideOpen = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: [openid]");
  const problem = gate(wideOpen);
  assert.match(problem!.message, /`openid` is an OIDC scope, not a permission handle/);
  assert.match(problem!.message, /the security of POST \/claims\/\{claimId\}\/approve/);
});

test("reserved_oidc_scope: an OIDC scope as a flows key is refused too", () => {
  const inFlows = mutate(
    EXPENSE_API_SPEC,
    "            reports:read: Monthly totals",
    "            reports:read: Monthly totals\n            group: Groups the user is in",
  );
  const problem = gate(inFlows);
  assert.match(problem!.message, /`group` is an OIDC scope/);
  assert.match(problem!.message, /flows\.authorizationCode\.scopes/);
});

test("every reserved OIDC scope is refused, not just openid", () => {
  for (const scope of ["openid", "profile", "email", "group", "ou"]) {
    const spec = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", `- oauth2: [${scope}]`);
    assert.match(gate(spec)!.message, new RegExp(`\`${scope}\` is an OIDC scope`), scope);
  }
});

// -------------------------------------------------------------------------
// The injected identity header
// -------------------------------------------------------------------------

test("identity_header_required: X-User-Id must be required: false", () => {
  const required = mutate(
    EXPENSE_API_SPEC,
    `    UserId:
      name: X-User-Id
      in: header
      required: false`,
    `    UserId:
      name: X-User-Id
      in: header
      required: true`,
  );
  const problem = gate(required);
  assert.match(problem!.message, /declares the header parameter X-User-Id as `required: true`/);
  assert.match(problem!.message, /answered 400 by the parameter binder instead of 401/);
});

test("identity_header_required catches any X-User-* header, wherever it is declared", () => {
  const inline = mutate(
    EXPENSE_API_SPEC,
    `  /reports/monthly:
    parameters:
      - $ref: '#/components/parameters/UserId'`,
    `  /reports/monthly:
    parameters:
      - $ref: '#/components/parameters/UserId'
      - name: X-User-Scopes
        in: header
        required: true
        schema: { type: string }`,
  );
  assert.match(gate(inline)!.message, /GET \/reports\/monthly declares the header parameter X-User-Scopes/);
});

test("public_operation_declares_identity_header: a `security: []` operation reads no identity", () => {
  const leaky = mutate(
    EXPENSE_API_SPEC,
    `  /health:
    get:`,
    `  /health:
    parameters:
      - $ref: '#/components/parameters/UserId'
    get:`,
  );
  const problem = gate(leaky);
  assert.match(problem!.message, /GET \/health is public \(`security: \[\]`\) and declares the header parameter X-User-Id/);
  assert.match(problem!.message, /does not strip inbound x-user-\* headers/);
});

// -------------------------------------------------------------------------
// Rule ORDER — the half a per-rule test cannot prove
// -------------------------------------------------------------------------

// A document with TWO defects must name the same first violation as the BFF's
// gate (`internal/spec/openapi_security_gate_test.go`, the same four cases), or
// the model fixing one refusal meets a different one from the other side and
// the two gates read as two rule sets.
for (const tc of [
  {
    // The scheme is judged before any operation.
    name: "the scheme outranks a stale operation handle",
    mutate: (spec: string) =>
      mutate(
        mutate(spec, "    oauth2:\n      type: oauth2", "    oauth2:\n      type: http"),
        "- oauth2: [claims:approve]",
        "- oauth2: [claims:archive]",
      ),
    wantHas: /declared as type: http/,
  },
  {
    // The advertised flow scopes are judged before any operation.
    name: "a flow scope outranks a stale operation handle",
    mutate: (spec: string) =>
      mutate(
        mutate(
          spec,
          "            reports:read: Monthly totals",
          "            reports:read: Monthly totals\n            claims:archive: Archive a claim",
        ),
        "- oauth2: [claims:approve]",
        "- oauth2: [claims:vanish]",
      ),
    wantHas: /advertises the scope `claims:archive` in flows/,
  },
  {
    // Operations are judged in DOCUMENT order: POST /claims precedes
    // POST /claims/{claimId}/approve.
    name: "the earlier operation wins",
    mutate: (spec: string) =>
      mutate(
        mutate(spec, "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]"),
        "- oauth2: [claims:approve]",
        "- oauth2: [claims:archive]",
      ),
    wantHas: /POST \/claims names more than one scope/,
  },
  {
    // Every security rule outranks the identity-header rules, which run last on
    // both sides.
    name: "an operation scope outranks a required identity header",
    mutate: (spec: string) =>
      mutate(
        mutate(
          spec,
          "    UserId:\n      name: X-User-Id\n      in: header\n      required: false",
          "    UserId:\n      name: X-User-Id\n      in: header\n      required: true",
        ),
        "- oauth2: [claims:approve]",
        "- oauth2: [claims:archive]",
      ),
    wantHas: /requires the scope `claims:archive`/,
  },
]) {
  test(`first violation: ${tc.name}`, () => {
    assert.match(gate(tc.mutate(EXPENSE_API_SPEC))!.message, tc.wantHas);
  });
}

// -------------------------------------------------------------------------
// Through the bundle, which is how the agent meets it
// -------------------------------------------------------------------------

test("a rejected security block leaves the bundle byte-for-byte unchanged", () => {
  const bundle = new FileBundle({
    [DESIGN_PATH]: PROTECTED_DESIGN,
    [SECURITY_PATH]: CATALOG,
    [SPEC_PATH]: EXPENSE_API_SPEC,
  });
  const stale = mutate(EXPENSE_API_SPEC, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]");

  const res = bundle.editFile(SPEC_PATH, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]");
  assert.ok(!res.ok, "the edit must be rejected");
  if (!res.ok) assert.equal(res.code, "INVALID_OPENAPI");
  assert.equal(bundle.read(SPEC_PATH), EXPENSE_API_SPEC, "the file is unchanged");
  assert.notEqual(stale, EXPENSE_API_SPEC);
});

test("the gate accepts the spec written whole into a bundle that already holds its siblings", () => {
  const bundle = new FileBundle({ [DESIGN_PATH]: PROTECTED_DESIGN, [SECURITY_PATH]: CATALOG });
  const res = bundle.addFile(SPEC_PATH, EXPENSE_API_SPEC);
  assert.ok(res.ok, `the write must be accepted: ${res.ok ? "" : res.message}`);
});

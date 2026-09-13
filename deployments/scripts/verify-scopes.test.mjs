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

// The one thing in verify-scopes.sh that can report a WRONG VERDICT rather than
// fail loudly: WHICH SCOPES THE TEST USERS' TOKENS ARE MINTED WITH.
//
// Thunder returns the intersection of what was asked for with what the user's
// roles grant. A run that asks only for the OIDC five therefore gets a token
// with no permission on it, every 200 row 401s, and the output accuses the
// gateway of a fault that is entirely in the probe. The script derives the ask
// from the RestApi it is about to call; this pins that derivation against a
// rendered CR, and pins that the minter is actually handed the result.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const script = readFileSync(path.join(here, "verify-scopes.sh"), "utf8");

// Each derivation, lifted out of the shell quoting exactly as the script embeds
// it, so this test cannot pass against a snippet the script no longer runs.
function between(startMarker, openMarker, closeMarker) {
  const start = script.indexOf(startMarker);
  assert.notEqual(start, -1, `verify-scopes.sh no longer defines ${startMarker}`);
  const open = script.indexOf(openMarker, start) + openMarker.length;
  const close = script.indexOf(closeMarker, open);
  assert.ok(close > open, `could not delimit the python in ${startMarker}`);
  return script.slice(open, close);
}

// The fallback: the RestApi's own operation table.
const restapiSnippet = () => between("restapi_scopes() {", "python3 -c '", "'\n}");
// The preferred source: the project resource server's catalog on the directory.
const catalogSnippet = () => between("catalog_scopes() {", "python3 - <<'PY'\n", "\nPY\n}");

function run(cr) {
  const r = spawnSync("python3", ["-c", restapiSnippet()], {
    input: JSON.stringify(cr),
    encoding: "utf8",
  });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.trim();
}

test("the fallback is every scope the rendered operation table can demand, in order, deduplicated", () => {
  const cr = {
    spec: {
      operations: [
        { method: "GET", path: "/me", policies: [{ name: "jwt-auth", params: {} }] },
        { method: "GET", path: "/health" },
        {
          method: "GET",
          path: "/claims",
          policies: [{ name: "jwt-auth", params: { scopes: { anyOf: ["claims:read"] } } }],
        },
        {
          method: "POST",
          path: "/claims",
          policies: [{ name: "jwt-auth", params: { scopes: { anyOf: ["claims:submit"] } } }],
        },
        // the synthesised OPTIONS row repeats its sibling's handle
        {
          method: "OPTIONS",
          path: "/claims",
          policies: [{ name: "jwt-auth", params: { scopes: { anyOf: ["claims:read"] } } }],
        },
        {
          method: "GET",
          path: "/reports/monthly",
          policies: [{ name: "jwt-auth", params: { scopes: { allOf: ["reports:read"] } } }],
        },
      ],
    },
  };
  assert.equal(run(cr), "claims:read claims:submit reports:read");
});

test("an API with no scoped operation asks for nothing extra", () => {
  assert.equal(run({ spec: { operations: [{ method: "GET", path: "/health" }] } }), "");
});

test("an unreadable CR yields an empty ask rather than a crash", () => {
  const r = spawnSync("python3", ["-c", restapiSnippet()], { input: "not json", encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout.trim(), "");
});

// The catalog, not the operation table, is what the run must ask for: the
// widening handle `claims:read-all` guards no operation, so it is absent from
// every RestApi policy — and without it the approver's collection read returns
// only their own rows and the ownership assertion fails 0-vs-0 for a reason
// that has nothing to do with the service.
test("the catalog source yields the widening handle the operation table cannot", () => {
  const directory = {
    "/resource-servers": {
      resourceServers: [
        { id: "other", identifier: "https://example/other" },
        { id: "rs-1", identifier: "https://aep.wso2.com/orgs/o/projects/p" },
      ],
    },
    "/resource-servers/rs-1/resources": {
      resources: [{ id: "r-claims", handle: "claims" }, { id: "r-reports", handle: "reports" }],
    },
    "/resource-servers/rs-1/resources/r-claims/actions": {
      actions: [
        { handle: "read", permission: "claims:read" },
        { handle: "read-all", permission: "claims:read-all" },
        { handle: "submit", permission: "claims:submit" },
      ],
    },
    "/resource-servers/rs-1/resources/r-reports/actions": {
      // no `permission` field: the handle pair must still compose
      actions: [{ handle: "read" }],
    },
  };
  // A stub urlopen keeps the assertion on the WALK (which paths, which fields,
  // dedupe and ordering) rather than on a live directory.
  const stub = `
import json, urllib.request
ROUTES = ${JSON.stringify(directory)}
class R:
    def __init__(self, b): self.b = b
    def read(self): return self.b
    def __enter__(self): return self
    def __exit__(self, *a): return False
def fake(req, *a, **kw):
    path = req.full_url[len("https://t2"):].split("?")[0]
    return R(json.dumps(ROUTES.get(path, {})).encode())
urllib.request.urlopen = fake
`;
  const r = spawnSync("python3", ["-c", stub + catalogSnippet()], {
    encoding: "utf8",
    env: {
      ...process.env,
      AEP_T2: "https://t2",
      AEP_RS: "https://aep.wso2.com/orgs/o/projects/p",
      AEP_TOKEN: "stub",
    },
  });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout.trim(), "claims:read claims:read-all claims:submit reports:read");
});

test("an identifier the directory does not hold yields an empty ask, so the caller falls back", () => {
  const stub = `
import json, urllib.request
class R:
    def read(self): return json.dumps({"resourceServers": []}).encode()
    def __enter__(self): return self
    def __exit__(self, *a): return False
urllib.request.urlopen = lambda *a, **kw: R()
`;
  const r = spawnSync("python3", ["-c", stub + catalogSnippet()], {
    encoding: "utf8",
    env: { ...process.env, AEP_T2: "https://t2", AEP_RS: "https://nope", AEP_TOKEN: "stub" },
  });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout.trim(), "");
});

test("the catalog is preferred and the operation table is only the fallback", () => {
  assert.match(script, /API_SCOPES="\$\(catalog_scopes\)"/);
  assert.match(script, /if \[ -z "\$API_SCOPES" \]; then\n\s+API_SCOPES="\$\(restapi_scopes\)"/);
});

test("the minter is handed the derived scopes", () => {
  assert.match(script, /REQUESTED_SCOPES="openid profile email group ou\$\{API_SCOPES:\+ \$\{API_SCOPES\}\}"/);
  assert.match(script, /AEP_SCOPE="\$REQUESTED_SCOPES"/);
});

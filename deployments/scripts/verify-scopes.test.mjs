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

// The extractor, lifted out of the shell quoting exactly as the script embeds
// it, so this test cannot pass against a snippet the script no longer runs.
function extractor() {
  const start = script.indexOf("API_SCOPES=");
  assert.notEqual(start, -1, "verify-scopes.sh no longer derives API_SCOPES");
  const open = script.indexOf("python3 -c '", start) + "python3 -c '".length;
  const close = script.indexOf("')\"", open);
  assert.ok(close > open, "could not delimit the embedded python");
  return script.slice(open, close);
}

function run(cr) {
  const r = spawnSync("python3", ["-c", extractor()], {
    input: JSON.stringify(cr),
    encoding: "utf8",
  });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.trim();
}

test("the ask is every scope the rendered operation table can demand, in order, deduplicated", () => {
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
  const r = spawnSync("python3", ["-c", extractor()], { input: "not json", encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout.trim(), "");
});

test("the minter is handed the derived scopes", () => {
  assert.match(script, /REQUESTED_SCOPES="openid profile email group ou\$\{API_SCOPES:\+ \$\{API_SCOPES\}\}"/);
  assert.match(script, /AEP_SCOPE="\$REQUESTED_SCOPES"/);
});

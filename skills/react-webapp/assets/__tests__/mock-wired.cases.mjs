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

// The wired gateway stand-in, driven directly. Run by ./mock-wired.test.mjs,
// which supplies the type-stripping flag.
//
// What is pinned here is the thing the real gateway does and the browser mock
// cannot: a request is refused BEFORE the service sees it, and one that is
// admitted arrives carrying a signature the service can verify. So the
// assertion is verified with real crypto against the public half of the key,
// not matched against a string — a minting bug that still produces a
// well-formed JWT is exactly the failure that would otherwise reach a walk as
// "every screen 401s".

import { test } from "node:test";
import assert from "node:assert/strict";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import {
  mintAssertion,
  parseMockBearer,
  usernameFor,
  wiredFromEnv,
  wiredMiddleware,
  wiredProxy,
} from "../mock-wired.ts";

const dir = fs.mkdtempSync(path.join(os.tmpdir(), "wired-"));
const { privateKey, publicKey } = crypto.generateKeyPairSync("rsa", {
  modulusLength: 2048,
  privateKeyEncoding: { type: "pkcs8", format: "pem" },
  publicKeyEncoding: { type: "spki", format: "pem" },
});
const keyPath = path.join(dir, "key.pem");
fs.writeFileSync(keyPath, privateKey);

const securityPath = path.join(dir, "security.json");
fs.writeFileSync(
  securityPath,
  JSON.stringify({
    roles: [{ name: "HRCoordinator", grants: ["tasks:read"] }],
    testUsers: [{ username: "test-hrcoordinator", roles: ["HRCoordinator"] }],
  }),
);

const OPTS = {
  target: "http://localhost:19090",
  keyPath,
  securityPath,
  issuer: "aep-playground-wire",
  header: "x-jwt-assertion",
};

/** The table mock/plugin.ts projects out of openapi.yaml, for three shapes of operation. */
const TABLE = {
  prefix: "/api",
  operations: [
    { method: "GET", path: "/tasks", pattern: "^/tasks$", scope: "tasks:read", isPublic: false },
    { method: "GET", path: "/health", pattern: "^/health$", scope: null, isPublic: true },
    { method: "GET", path: "/me/tasks", pattern: "^/me/tasks$", scope: null, isPublic: false },
  ],
};

/** A request/response pair a Connect middleware can be driven with. */
function exchange(method, url, headers = {}) {
  const res = {
    statusCode: 200,
    headers: {},
    ended: false,
    setHeader(name, value) {
      this.headers[name] = value;
    },
    end() {
      this.ended = true;
    },
  };
  const req = { method, url, headers };
  let nexted = false;
  wiredMiddleware(TABLE, OPTS)(req, res, () => {
    nexted = true;
  });
  return { req, res, nexted };
}

/** The proxy's `proxyReq` hook, driven with a header sink standing in for the upstream request. */
function forward(req) {
  const entry = wiredProxy(TABLE, OPTS)["/api"];
  const set = {};
  const removed = [];
  const proxyReq = {
    setHeader(name, value) {
      set[name] = value;
    },
    removeHeader(name) {
      removed.push(name);
    },
  };
  let hook;
  entry.configure({
    on(event, callback) {
      if (event === "proxyReq") hook = callback;
    },
  });
  hook(proxyReq, req);
  return { set, removed, entry };
}

// --- the bearer ------------------------------------------------------------

test("the bearer names the role as well as the scopes", () => {
  assert.deepEqual(parseMockBearer("Bearer mock:HRCoordinator;tasks%3Aread,openid"), {
    role: "HRCoordinator",
    scopes: ["tasks:read", "openid"],
  });
});

test("a token with no role segment is the pre-wired shape, read as all scopes", () => {
  assert.deepEqual(parseMockBearer("Bearer mock:tasks%3Aread"), { role: "", scopes: ["tasks:read"] });
});

test("`?role=` is signed in holding nothing, which is not the same as no session", () => {
  assert.deepEqual(parseMockBearer("Bearer mock:;"), { role: "", scopes: [] });
  assert.equal(parseMockBearer("Bearer something-else"), null);
  assert.equal(parseMockBearer(undefined), null);
});

test("a malformed escape is a refused session, never a thrown one", () => {
  // `decodeURIComponent` throws on a broken percent-escape, and a throw here
  // leaves the middleware as a 500 — so the ONE unusable token that reads as
  // "the mock is broken" rather than "your token is". Both halves are decoded,
  // so both halves are tried.
  assert.equal(parseMockBearer("Bearer mock:%zz;tasks%3Aread"), null, "a broken role");
  assert.equal(parseMockBearer("Bearer mock:HRCoordinator;%zz"), null, "a broken scope");
  assert.equal(parseMockBearer("Bearer mock:%"), null, "a truncated escape with no role segment");
});

test("the username is the role's testUsers row, and the role name when it has none", () => {
  assert.equal(usernameFor("HRCoordinator", securityPath), "test-hrcoordinator");
  assert.equal(usernameFor("Nobody", securityPath), "Nobody");
  assert.equal(usernameFor("HRCoordinator", path.join(dir, "absent.json")), "HRCoordinator");
});

// --- the refusals ----------------------------------------------------------

test("a path no contract declares is 404 and never reaches the service", () => {
  const { res, nexted } = exchange("GET", "/api/invented");
  assert.equal(nexted, false);
  assert.equal(res.statusCode, 404);
  assert.match(res.headers["x-aep-wired-reason"], /no operation in any openapi\.yaml/);
});

test("no session is 401", () => {
  const { res, nexted } = exchange("GET", "/api/tasks");
  assert.equal(nexted, false);
  assert.equal(res.statusCode, 401);
  assert.match(res.headers["x-aep-wired-reason"], /no session/);
});

test("a role without the handle is 401, and the refusal names the handle", () => {
  const { res } = exchange("GET", "/api/tasks", { authorization: "Bearer mock:HiringManagerRole;new-hires%3Aread-team" });
  assert.equal(res.statusCode, 401);
  // The em dash the message is written with is sanitized to ASCII on the way out.
  assert.equal(res.headers["x-aep-wired-reason"], "GET /tasks - role HiringManagerRole lacks tasks:read");
});

test("the reason header is ASCII, because a header value that is not makes every call a 500", () => {
  const { res } = exchange("GET", "/api/tasks", { authorization: "Bearer mock:x;nothing" });
  const reason = res.headers["x-aep-wired-reason"];
  assert.ok(/^[\x20-\x7e]*$/.test(reason), `non-ASCII in ${JSON.stringify(reason)}`);
  // The em dash the message is written with is what would have thrown.
  assert.ok(!reason.includes("—"));
});

test("an operation that declares no handle admits any signed-in caller", () => {
  const { nexted, res } = exchange("GET", "/api/me/tasks", { authorization: "Bearer mock:;" });
  assert.equal(nexted, true);
  assert.equal(res.ended, false);
});

test("a request outside the prefix is none of this middleware's business", () => {
  const { nexted } = exchange("GET", "/src/main.tsx");
  assert.equal(nexted, true);
});

// --- the forward -----------------------------------------------------------

test("an admitted request arrives with an assertion the service can verify", () => {
  const { req, nexted } = exchange("GET", "/api/tasks", {
    authorization: "Bearer mock:HRCoordinator;tasks%3Aread",
  });
  assert.equal(nexted, true);

  const { set, removed } = forward(req);
  assert.ok(removed.includes("authorization"), "the browser's own bearer must not reach the service");
  const token = set["x-jwt-assertion"];
  assert.ok(token, "no assertion was set");

  const [header, payload, signature] = token.split(".");
  const verified = crypto.verify(
    "sha256",
    Buffer.from(`${header}.${payload}`),
    publicKey,
    Buffer.from(signature, "base64url"),
  );
  assert.equal(verified, true, "the assertion does not verify against the public key");

  const claims = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
  assert.equal(claims.iss, "aep-playground-wire");
  assert.equal(claims.username, "test-hrcoordinator");
  assert.equal(claims.ouHandle, "local");
  assert.match(claims.sub, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  // The OIDC base scopes ride beside the project handles, as the real token's do.
  assert.deepEqual(claims.scope.split(" ").sort(), ["email", "group", "openid", "ou", "profile", "tasks:read"]);
  assert.equal(claims.exp - claims.iat, 60);
  assert.equal(JSON.parse(Buffer.from(header, "base64url").toString("utf8")).alg, "RS256");
});

test("a public operation is forwarded with no identity at all", () => {
  const { req, nexted } = exchange("GET", "/api/health");
  assert.equal(nexted, true);
  const { set, removed } = forward(req);
  assert.equal(set["x-jwt-assertion"], undefined);
  assert.ok(removed.includes("x-jwt-assertion"), "a replayed assertion must be cleared, not left to pass");
});

// Pinned on the playground side too (playground/test/wire-session.test.ts): the
// dev server mints this per request and `wire` mints one for its printed curl
// table, and a role must be the same caller in both.
const HRCOORDINATOR_SUBJECT = "672ff732-07fd-0a47-5c2f-f1217acca0af";

test("the same role is the same subject on every run, and the same one the playground mints", () => {
  const caller = { role: "HRCoordinator", username: "test-hrcoordinator", scopes: ["tasks:read"] };
  const claims = (token) => JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
  assert.equal(claims(mintAssertion(caller, OPTS)).sub, claims(mintAssertion(caller, OPTS)).sub);
  assert.equal(claims(mintAssertion(caller, OPTS)).sub, HRCOORDINATOR_SUBJECT);
  assert.notEqual(
    claims(mintAssertion(caller, OPTS)).sub,
    claims(mintAssertion({ ...caller, role: "ITOnboardingStaff" }, OPTS)).sub,
  );
});

test("the prefix is stripped on the way out, as the production nginx strips it", () => {
  const { entry } = forward({ headers: {} });
  assert.equal(entry.target, "http://localhost:19090");
  assert.equal(entry.rewrite("/api/tasks"), "/tasks");
  assert.equal(entry.rewrite("/api"), "/");
});

// --- the switch ------------------------------------------------------------

test("wired mode is off unless AEP_WIRED_API says otherwise, and then it needs the key", () => {
  const saved = { ...process.env };
  try {
    delete process.env.AEP_WIRED_API;
    assert.equal(wiredFromEnv(dir), null);

    process.env.AEP_WIRED_API = "http://localhost:19090";
    delete process.env.AEP_WIRED_KEY;
    assert.throws(() => wiredFromEnv(dir), /AEP_WIRED_KEY/);

    process.env.AEP_WIRED_KEY = keyPath;
    const opts = wiredFromEnv(dir);
    assert.equal(opts.issuer, "aep-playground-wire");
    assert.equal(opts.header, "x-jwt-assertion");
    assert.equal(opts.securityPath, path.resolve(dir, "..", "specs", "design", "security.json"));
  } finally {
    process.env = saved;
  }
});

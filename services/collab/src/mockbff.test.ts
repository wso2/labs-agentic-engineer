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

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import type http from "node:http";
import { startMockBff } from "./mockbff.js";
import { createBffClient, BffAccessDeniedError } from "./bff.js";
import { devSpecFiles } from "./fixtures.js";

let server: http.Server;
let base: string;

before(async () => {
  server = await startMockBff(0);
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}/api/v1`;
});

after(() => server.close());

// A syntactically valid JWT whose payload is {"name":"Jo","email":"jo@x.io"}.
const jwt = [
  Buffer.from(JSON.stringify({ alg: "none" })).toString("base64url"),
  Buffer.from(JSON.stringify({ name: "Jo", email: "jo@x.io" })).toString(
    "base64url",
  ),
  "sig",
].join(".");

test("validate: 401 without a token", async () => {
  const res = await fetch(`${base}/collab/validate`, {
    headers: { "X-Room-Id": "spec-acme-shop" },
  });
  assert.equal(res.status, 401);
});

test("validate: 403 for the deny token (rejection test hook)", async () => {
  const bff = createBffClient(base);
  await assert.rejects(
    bff.validateAccess("deny", "spec-acme-shop"),
    BffAccessDeniedError,
  );
});

test("validate: identity decoded from a JWT payload + project from the room", async () => {
  const bff = createBffClient(base);
  const id = await bff.validateAccess(jwt, "spec-acme-demo-shop");
  assert.deepEqual(id, { name: "Jo", email: "jo@x.io", projectName: "demo-shop" });
});

test("validate: opaque tokens get the fixed mock identity", async () => {
  const bff = createBffClient(base);
  const id = await bff.validateAccess("opaque-token", "spec-acme-shop");
  assert.deepEqual(id, {
    name: "Mock User",
    email: "mock@localhost",
    projectName: "shop",
  });
});

test("validate: rooms outside the caller's org are denied", async () => {
  const bff = createBffClient(base);
  await assert.rejects(
    bff.validateAccess("opaque-token", "spec-otherorg-shop"),
    BffAccessDeniedError,
  );
});

test("spec files: list + per-file reads through the real client, keys stripped of specs/", async () => {
  const bff = createBffClient(base);
  const files = await bff.fetchSpecFiles("opaque-token", "demo-shop");
  assert.equal(files.length, devSpecFiles.length);
  const prd = files.find((f) => f.path === "requirements/prd.md");
  assert.ok(prd, "room key is the repo path without specs/");
  assert.match(prd.content, /Demo Shop — PRD/);
});

test("spec files: unlisted projects get the dev files (org-permissive, like the console mocks)", async () => {
  const bff = createBffClient(base);
  const files = await bff.fetchSpecFiles("opaque-token", "any-other-project");
  assert.equal(files.length, devSpecFiles.length);
});

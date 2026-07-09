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

import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import type { CollabConfig } from "./env.js";
import type { BffClient } from "./bff.js";
import { BffAccessDeniedError } from "./bff.js";
import { buildAuthenticateHook, buildLoadDocumentHook } from "./server.js";
import { filesMap } from "./seed.js";
import { devSeedFiles } from "./fixtures.js";
import type { Document } from "@hocuspocus/server";

const prodConfig: CollabConfig = {
  port: 0,
  aepApiBase: "http://bff.test/api/v1",
  devMode: false,
  mockBff: false,
  mockBffPort: 0,
};
const devConfig: CollabConfig = {
  port: 0,
  aepApiBase: null,
  devMode: true,
  mockBff: false,
  mockBffPort: 0,
};

function fakeBff(overrides: Partial<BffClient> = {}): BffClient {
  return {
    validateAccess: async () => ({
      name: "Jo",
      email: "jo@example.com",
      projectName: "shop",
    }),
    fetchSpecFiles: async () => [
      { path: "requirements/prd.md", content: "# P\n" },
    ],
    ...overrides,
  };
}

test("auth rejects unknown rooms before hitting the oracle", async () => {
  let oracleCalled = false;
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async () => {
        oracleCalled = true;
        return { name: "x", email: "x", projectName: "x" };
      },
    }),
  });
  await assert.rejects(
    auth({ token: "t", documentName: "not-a-spec-room" }),
    /unknown room/,
  );
  assert.equal(oracleCalled, false);
});

test("auth passes token + room to the oracle; identity and project come back", async () => {
  const seen: string[] = [];
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async (token, room) => {
        seen.push(token, room);
        return { name: "Jo", email: "jo@example.com", projectName: "shop" };
      },
    }),
  });
  const ctx = await auth({ token: "jwt-abc", documentName: "spec-acme-shop" });
  assert.deepEqual(seen, ["jwt-abc", "spec-acme-shop"]);
  assert.equal(ctx.user.name, "Jo");
  assert.equal(ctx.user.kind, "user");
  assert.equal(ctx.token, "jwt-abc");
  assert.equal(ctx.projectName, "shop");
});

test("auth propagates oracle denial", async () => {
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async () => {
        throw new BffAccessDeniedError(403);
      },
    }),
  });
  await assert.rejects(
    auth({ token: "t", documentName: "spec-acme-shop" }),
    BffAccessDeniedError,
  );
});

test("auth rejects a missing token outside dev mode", async () => {
  const auth = buildAuthenticateHook(prodConfig, { bff: fakeBff() });
  await assert.rejects(
    auth({ token: "", documentName: "spec-acme-shop" }),
    /missing token/,
  );
});

test("dev mode skips the oracle entirely", async () => {
  const auth = buildAuthenticateHook(devConfig, { bff: null });
  const ctx = await auth({ token: "", documentName: "spec-acme-shop" });
  assert.equal(ctx.user.kind, "dev");
});

test("dev mode still rejects unknown rooms", async () => {
  const auth = buildAuthenticateHook(devConfig, { bff: null });
  await assert.rejects(
    auth({ token: "", documentName: "lobby" }),
    /unknown room/,
  );
});

test("load seeds from fixtures in dev mode (md files become fragments)", async () => {
  const load = buildLoadDocumentHook(devConfig, { bff: null });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: { user: { name: "d", email: "d", kind: "dev" }, token: null, projectName: null },
  });
  for (const file of devSeedFiles) {
    if (file.path.endsWith(".md")) {
      assert.ok(
        doc.getXmlFragment(file.path).length > 0,
        `fragment for ${file.path}`,
      );
    } else {
      assert.ok(filesMap(doc).has(file.path), `map entry for ${file.path}`);
    }
  }
});

test("load seeds from the BFF's Files API with the joiner's token", async () => {
  const calls: string[] = [];
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async (token, project) => {
        calls.push(token, project);
        return [{ path: "requirements/prd.md", content: "hi" }];
      },
    }),
  });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: {
      user: { name: "Jo", email: "j", kind: "user" },
      token: "jwt-abc",
      projectName: "shop",
    },
  });
  assert.deepEqual(calls, ["jwt-abc", "shop"]);
  assert.match(doc.getXmlFragment("requirements/prd.md").toString(), /hi/);
});

test("load opens an unseeded doc when the files fetch fails (room must survive)", async () => {
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async () => {
        throw new Error("files fetch exploded (404)");
      },
    }),
  });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: {
      user: { name: "Jo", email: "j", kind: "user" },
      token: "t",
      projectName: "shop",
    },
  });
  assert.equal(filesMap(doc).size, 0);
});

test("load opens an empty doc when the oracle gave no project (pre-phase-2 BFF)", async () => {
  const load = buildLoadDocumentHook(prodConfig, { bff: fakeBff() });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: { user: { name: "Jo", email: "j", kind: "user" }, token: "t", projectName: null },
  });
  assert.equal(filesMap(doc).size, 0);
});

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

import assert from "node:assert/strict";
import test from "node:test";
import {
  assembleDatabaseUrl,
  readDiscreteDatabaseConfig,
  resolveDatabaseUrl,
} from "../src/shared/database-url.js";

test("DATABASE_URL wins over discrete", () => {
  const env = {
    DATABASE_URL: "postgres://u:p@h:5432/db",
    DB_HOST: "other",
    DB_USER: "u",
    DB_PASSWORD: "p",
    DB_NAME: "db",
  };
  assert.equal(resolveDatabaseUrl(env), "postgres://u:p@h:5432/db");
});

test("assembles discrete without sslmode when unset", () => {
  const url = assembleDatabaseUrl({
    host: "db.example",
    port: 5432,
    user: "app_factory_db_user",
    password: "s3cret",
    name: "app_factory_db",
  });
  assert.equal(
    url,
    "postgres://app_factory_db_user:s3cret@db.example:5432/app_factory_db",
  );
  assert.ok(!url.includes("sslmode"));
});

test("includes sslmode only when set", () => {
  const url = assembleDatabaseUrl({
    host: "h",
    port: 5432,
    user: "u",
    password: "p",
    name: "n",
    sslMode: "disable",
  });
  assert.ok(url.includes("sslmode=disable"));
});

test("percent-encodes password special characters", () => {
  const url = assembleDatabaseUrl({
    host: "h",
    port: 5432,
    user: "u",
    password: "a@b:c/d",
    name: "n",
  });
  assert.ok(url.includes(encodeURIComponent("a@b:c/d")));
  assert.ok(!url.includes("@b:c"));
});

test("incomplete discrete returns undefined (in-memory path)", () => {
  assert.equal(resolveDatabaseUrl({ DB_HOST: "h" }), undefined);
  assert.equal(readDiscreteDatabaseConfig({ DB_HOST: "h" }), null);
});

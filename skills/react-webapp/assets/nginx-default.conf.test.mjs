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

// The SPA's own `/api` proxy is a lane into the service that the API gateway
// does NOT sit on, and the service is required to believe the `X-User-*`
// headers it is handed. So every one of them must be cleared here, and
// `X-User-Scopes` above all: it is the authorization authority, and a browser
// that set it on a call to this SPA's `/api` would otherwise hand itself every
// permission in the project's catalog. Missing ONE of these lines is a silent
// full authorization bypass with every test still green, which is why the list
// is pinned rather than reviewed.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const asset = readFileSync(
  path.join(path.dirname(fileURLToPath(import.meta.url)), "nginx-default.conf"),
  "utf8",
);

// Exactly the headers the api-management skill's table declares the gateway
// maps. A header added there and not here is a hole this test cannot see, so
// the two lists are kept in step by hand — and by this comment.
const MAPPED_IDENTITY_HEADERS = [
  "X-User-Scopes",
  "X-User-Id",
  "X-User-Name",
  "X-User-Groups",
  "X-User-Ou",
];

test("the /api proxy clears every gateway-mapped identity header", () => {
  for (const header of MAPPED_IDENTITY_HEADERS) {
    assert.match(
      asset,
      new RegExp(`^\\s*proxy_set_header\\s+${header}\\s+"";\\s*$`, "m"),
      `nginx-default.conf must carry: proxy_set_header ${header} "";  — ` +
        "a browser can set it, and the service behind this proxy believes it",
    );
  }
});

test("the clears are inside the /api location, not the SPA one", () => {
  const apiLocation = asset.slice(
    asset.indexOf("location /api/"),
    asset.indexOf("location /", asset.indexOf("location /api/") + 1),
  );
  for (const header of MAPPED_IDENTITY_HEADERS) {
    assert.ok(
      apiLocation.includes(`proxy_set_header ${header} "";`),
      `${header} is cleared outside the /api/ location block`,
    );
  }
});

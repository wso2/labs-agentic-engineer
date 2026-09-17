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
// does NOT sit on, so nothing should be able to put an identity on it.
// `x-jwt-assertion` above all: it is the gateway-signed statement the service
// actually believes, and on a PUBLIC operation the gateway overwrites nothing,
// so a replayed one would arrive intact. The `X-User-*` headers are unsigned
// and no generated service reads them any more, but they are cleared too.
// Missing ONE of these lines is a hole with every other test still green,
// which is why the list is pinned rather than reviewed.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const asset = readFileSync(
  path.join(path.dirname(fileURLToPath(import.meta.url)), "nginx-default.conf"),
  "utf8",
);

// The assertion plus exactly the headers the api-management skill declares the
// gateway maps. A header added there and not here is a hole this test cannot
// see, so the two lists are kept in step by hand — and by this comment.
const MAPPED_IDENTITY_HEADERS = [
  "x-jwt-assertion",
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
        "a browser can set it on this lane, which the gateway does not sit on",
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

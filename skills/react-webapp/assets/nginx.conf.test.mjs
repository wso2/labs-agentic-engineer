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

// The cloud `web-application` type runs the SPA container on a read-only root
// filesystem as a non-root UID, and mounts no writable volume: /dev/shm is the
// only path nginx can write. A pid file, temp path or rendered conf anywhere
// else crash-loops the pod on its first cloud deploy while every local deploy
// (writable root) stays green, so the contract is pinned here rather than
// discovered there.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));
const nginxConf = readFileSync(path.join(dir, "nginx.conf"), "utf8");
const dropIn = readFileSync(path.join(dir, "15-aep-api-proxy.sh"), "utf8");

const RUNTIME_DIR = "/dev/shm/nginx";

// Every directive that names a path nginx writes at runtime. A temp path left
// unset falls back to the compiled-in /var/cache/nginx, so all five are listed.
const WRITTEN_PATH_DIRECTIVES = [
  "pid",
  "client_body_temp_path",
  "proxy_temp_path",
  "fastcgi_temp_path",
  "uwsgi_temp_path",
  "scgi_temp_path",
];

test("nginx.conf writes only under /dev/shm/nginx", () => {
  for (const directive of WRITTEN_PATH_DIRECTIVES) {
    const match = nginxConf.match(new RegExp(`^\\s*${directive}\\s+(\\S+);`, "m"));
    assert.ok(match, `nginx.conf must set ${directive} (its default is on the read-only root)`);
    assert.ok(
      match[1].startsWith(`${RUNTIME_DIR}/`),
      `${directive} ${match[1]} is outside ${RUNTIME_DIR}, which is the only writable path in the cloud`,
    );
  }
});

test("the drop-in renders the conf nginx.conf includes, and nothing under /etc", () => {
  assert.match(dropIn, new RegExp(`^RUNTIME_DIR=${RUNTIME_DIR}$`, "m"));
  assert.match(dropIn, /^CONF="\$RUNTIME_DIR\/default\.conf"$/m);
  assert.match(nginxConf, new RegExp(`^\\s*include\\s+${RUNTIME_DIR}/default\\.conf;`, "m"));
  for (const line of dropIn.split("\n").filter((l) => /\bsed -i\b/.test(l))) {
    assert.ok(line.includes('"$CONF"'), `in-place edit outside the rendered conf: ${line.trim()}`);
  }
});

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

/** Where the CLI's shipped files are: resolved from the built module (dist/assets.js), so it works installed or in the workspace. */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

/** The package root (one level above dist/). */
export const PACKAGE_ROOT = fileURLToPath(new URL("../", import.meta.url));

/** The prebuilt preview host bundle. */
export const HOST_SCRIPT_PATH = join(PACKAGE_ROOT, "dist", "host", "host.js");

/** The `init` scaffold. */
export const INIT_TEMPLATE_DIR = join(PACKAGE_ROOT, "templates", "init");

export function packageVersion(): string {
  return (JSON.parse(readFileSync(join(PACKAGE_ROOT, "package.json"), "utf8")) as { version: string }).version;
}

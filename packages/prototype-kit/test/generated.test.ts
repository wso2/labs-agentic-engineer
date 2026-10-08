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

/** Anti-drift: each committed generated file is a fresh render of the kit's source. */

import { readFileSync } from "node:fs";
import { basename } from "node:path";
import { describe, expect, it } from "vitest";
import { ARTIFACTS } from "../scripts/artifacts.js";

// Rendering the reference runs the TypeScript checker over the kit, which
// takes several seconds on a CI runner — well past vitest's 5 s default.
const RENDER_TIMEOUT_MS = 60_000;

describe("generated artifacts", () => {
  it.each(ARTIFACTS.map((a) => [basename(a.path), a] as const))(
    "%s is fresh",
    (_name, artifact) => {
      expect(readFileSync(artifact.path, "utf8"), "stale — run `pnpm --filter @wso2/prototype-kit gen`").toBe(artifact.render());
    },
    RENDER_TIMEOUT_MS,
  );
});

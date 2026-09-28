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

// The `aep-web` MCP server has to be IN this image: a run whose connection
// searches through Ollama's API finds it through AEP_WEB_SEARCH_SERVER or has no
// search at all (`lib/aep_web.ts`). Same reasoning as the agent-eval harness
// (agent_eval_packaging.test.ts): a named build context passed by one builder
// and not another yields an image that searches locally and not in the cloud,
// so every builder is checked against the same Dockerfile.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

const read = (rel: string): string => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const DOCKERFILE = read("../Dockerfile");

// Every builder of this image, images.yml included: its RUNNER_CONTEXTS feeds
// both runner rows.
const BUILDERS: ReadonlyArray<readonly [string, string]> = [
  ["the release workflow", read("../../../.github/workflows/release.yml")],
  ["the image workflow", read("../../../.github/workflows/images.yml")],
  ["the local cluster build", read("../../../deployments/scripts/build-runner.sh")],
  ["the standalone local run", read("../local/run-local.sh")],
];

describe("the runner image carries the aep-web server", () => {
  it("bundles the server from its own named build context and says where it is", () => {
    assert.match(DOCKERFILE, /COPY --from=web-search src/, "the image no longer takes packages/web-search as a build context");
    assert.match(DOCKERFILE, /esbuild "\$AEP_WEB_SEARCH_HOME\/src\/aep-web\.ts"/, "the server is not bundled");
    assert.match(DOCKERFILE, /ENV AEP_WEB_SEARCH_SERVER=/, "the runner finds the server through AEP_WEB_SEARCH_SERVER");
  });

  for (const [name, source] of BUILDERS) {
    it(`is passed the web-search context by ${name}`, () => {
      // Comments stripped first, as in agent_eval_packaging.test.ts: each file
      // explains the context it passes, and a comment must not satisfy this.
      const code = source
        .split("\n")
        .filter((line) => !/^\s*#/.test(line))
        .join("\n");
      assert.match(code, /web-search=[^\s"]*packages\/web-search/, `${name} builds this image without the web-search context`);
    });
  }

  it("release.yml passes it on BOTH runner rows", () => {
    const code = read("../../../.github/workflows/release.yml")
      .split("\n")
      .filter((line) => !/^\s*#/.test(line))
      .join("\n");
    assert.equal(code.match(/web-search=packages\/web-search/g)?.length, 2);
  });
});

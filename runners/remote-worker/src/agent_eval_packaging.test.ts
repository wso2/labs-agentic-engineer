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

// The agent-evaluation harness has to be IN this image. A build pod holds no
// monorepo, so the step the `agent-building` skill prescribes either finds the
// harness on the pod or does not run at all — and a step that silently does not
// run reads exactly like a step that ran and found nothing.
//
// The Dockerfile alone cannot be trusted for that: a named build context that no
// builder passes fails the build outright in CI, but a context passed by ONE of
// the three builders produces an image that carries the harness locally and not
// in the cloud, or the reverse. So every builder is checked here, against the
// same Dockerfile.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

const read = (rel: string): string =>
  readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const DOCKERFILE = read("../Dockerfile");

// The three builders of this image. skaffold.yaml builds aep-api only.
const BUILDERS: ReadonlyArray<readonly [string, string]> = [
  ["the release workflow", read("../../../.github/workflows/release.yml")],
  ["the local cluster build", read("../../../deployments/scripts/build-runner.sh")],
  ["the standalone local run", read("../local/run-local.sh")],
];

describe("the runner image carries the agent-evaluation harness", () => {
  it("installs the harness from its own named build context", () => {
    assert.match(
      DOCKERFILE,
      /COPY --from=agent-eval package\.json/,
      "the image no longer takes packages/agent-eval as a build context",
    );
    assert.match(DOCKERFILE, /COPY --from=agent-eval src/, "the harness sources are not copied in");
    // `npm ci`, never `npm install`: a floating resolution would leave a pod
    // grading agents against a promptfoo the tests never saw.
    assert.match(
      DOCKERFILE,
      /npm ci --omit=dev --omit=optional/,
      "the harness needs its own dependency tree, installed from its lockfile — promptfoo is not a runner dependency, and its optional provider SDKs are not the harness's",
    );
    // The one optional the harness cannot start without: promptfoo's SQLite
    // layer loads libsql's native binding at startup.
    assert.match(
      DOCKERFILE,
      /@libsql\/linux-\$\{LIBSQL_ARCH\}-gnu@\$\{LIBSQL_VERSION\}/,
      "omitting optionals drops libsql's platform binding; it has to be installed back by name at the lockfile's version",
    );
    assert.match(
      DOCKERFILE,
      /COPY --from=agent-eval package\.json package-lock\.json/,
      "the lockfile is not copied in, so `npm ci` has nothing to install from",
    );
  });

  // The one command the skill runs in a pod. Without it the harness is present
  // and unreachable, which is the same as absent.
  it("puts agent-eval on PATH", () => {
    assert.match(DOCKERFILE, /\/usr\/local\/bin\/agent-eval/);
    assert.match(DOCKERFILE, /AEP_AGENT_EVAL_HOME=\/opt\/aep\/agent-eval/);
  });

  for (const [name, source] of BUILDERS) {
    it(`is passed the agent-eval context by ${name}`, () => {
      // Comments stripped first. Every one of these files EXPLAINS the context
      // it passes a line or two above passing it, and an assertion a comment can
      // satisfy is an assertion that cannot fail when the argument is deleted.
      const code = source
        .split("\n")
        .filter((line) => !/^\s*#/.test(line))
        .join("\n");
      assert.match(
        code,
        /agent-eval=[^\s"]*packages\/agent-eval/,
        `${name} builds this image without the harness's build context — the build would fail, or worse, differ from the others`,
      );
    });
  }
});

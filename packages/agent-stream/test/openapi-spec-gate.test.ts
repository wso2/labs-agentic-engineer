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

/**
 * Write-gate behavior for a component's `openapi.yaml`. The gate exists so
 * validating a spec costs nothing: the platform's `validate_openapi_spec` MCP
 * tool takes the document as a STRING, so an agent asking it about a file it
 * had just written had to retype the whole thing as tool input — measured at
 * 4.1k output tokens and 28.9s for a 13KB spec. Coverage matches that tool's
 * (structural: 3.x, has paths, has operations), so nothing is lost by moving
 * the check here. Same seam as the design.json and wireframes gates.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkOpenapiSpec } from "../src/openapi-spec.ts";
import { FileBundle } from "../src/bundle.ts";

const PATH = "specs/design/components/todo-api/openapi.yaml";

const CLEAN = `openapi: 3.0.3
info:
  title: Todo API
  version: 0.1.0
paths:
  /todos:
    get:
      summary: List todos
      responses:
        "200":
          description: ok
`;

test("accepts a well-formed OpenAPI 3.x document", () => {
  assert.equal(checkOpenapiSpec(PATH, CLEAN), null);
});

test("rejects a document that is not OpenAPI 3.x", () => {
  const swagger = CLEAN.replace("openapi: 3.0.3", "swagger: '2.0'");
  const problem = checkOpenapiSpec(PATH, swagger);
  assert.equal(problem?.code, "INVALID_OPENAPI");
  assert.match(problem!.message, /not an OpenAPI 3\.x document/);
});

test("rejects a document with no paths", () => {
  const noPaths = `openapi: 3.0.3
info:
  title: Todo API
  version: 0.1.0
paths: {}
`;
  assert.equal(checkOpenapiSpec(PATH, noPaths)?.code, "INVALID_OPENAPI");
});

test("rejects a paths block that declares no operations", () => {
  // A path item with only shared `parameters` and no method — the shape a model
  // lands on when it stubs a path out and moves on.
  const noOps = `openapi: 3.0.3
info:
  title: Todo API
  version: 0.1.0
paths:
  /todos:
    parameters:
      - name: limit
        in: query
        schema:
          type: integer
`;
  const problem = checkOpenapiSpec(PATH, noOps);
  assert.equal(problem?.code, "INVALID_OPENAPI");
  assert.match(problem!.message, /no operations/);
});

test("ignores a dependency's committed spec (someone else's document)", () => {
  // `dependencies/<name>.openapi.yaml` is a third-party spec recorded as-is.
  // Holding it to this gate would reject a write the agent is only relaying.
  const dep = "specs/design/components/todo-api/dependencies/stripe.openapi.yaml";
  const swagger = CLEAN.replace("openapi: 3.0.3", "swagger: '2.0'");
  assert.equal(checkOpenapiSpec(dep, swagger), null);
});

test("ignores non-spec paths", () => {
  assert.equal(checkOpenapiSpec("specs/design/domain-model.md", "# not a spec"), null);
});

test("a rejected write leaves the bundle byte-for-byte unchanged", () => {
  const bundle = new FileBundle({ [PATH]: CLEAN });
  const broken = CLEAN.replace("openapi: 3.0.3", "openapi: 2.0.0");

  const res = bundle.removeFile(PATH).ok ? bundle.addFile(PATH, broken) : undefined;
  assert.ok(res && !res.ok, "the write must be rejected");
  if (!res.ok) assert.equal(res.code, "INVALID_OPENAPI");
  assert.equal(bundle.read(PATH), undefined, "the remove applied; the rejected add wrote nothing");
});

test("the gate runs on an edit too, not just a whole-file write", () => {
  const bundle = new FileBundle({ [PATH]: CLEAN });
  // Deleting the sole operation via an anchored edit leaves valid YAML that is
  // no longer a usable spec — the reason this gate is separate from the YAML one.
  const res = bundle.editFile(
    PATH,
    `    get:
      summary: List todos
      responses:
        "200":
          description: ok
`,
    "",
  );
  assert.ok(!res.ok, "an edit that empties the paths block must be rejected");
  if (!res.ok) assert.equal(res.code, "INVALID_OPENAPI");
  assert.equal(bundle.read(PATH), CLEAN, "the file is unchanged");
});

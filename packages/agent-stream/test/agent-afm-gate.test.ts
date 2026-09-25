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
 * Write-gate behavior for the `agent.afm.md` structural schema — the
 * self-contained half checkable from the document alone (parse, required
 * fields, enum values, body sections, and the no-literal-credentials rule).
 * The Go fold gate (agentfold/afmgate.go) has its own parity tests, and the
 * two must agree — an AFM document that passes one gate MUST pass the other.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { checkAgentAfm } from "../src/agent-afm-schema.ts";

const VALID = `---
spec_version: "0.4.0"
name: "lunch-agent"
description: "Helps a teammate order lunch."
max_iterations: 12
model:
  provider: "anthropic"
  name: "\${env:MODEL_NAME}"
  url: "\${env:MODEL_ENDPOINT}"
  authentication:
    type: "api-key"
    api_key: "\${env:MODEL_API_KEY}"
interfaces:
  - type: webchat
x-aep:
  tools:
    openapi:
      - component: "lunch-api"
        baseUrl: "\${env:LUNCH_API_URL}"
        allow: [addItem]
---

# Role

You help teammates order lunch.

# Instructions

- Confirm before adding anything.
`;

test("accepts a valid document", () => {
  assert.equal(checkAgentAfm(VALID, "lunch-agent"), null);
});

test("rejects a missing model.provider", () => {
  const problem = checkAgentAfm(VALID.replace('  provider: "anthropic"\n', ""), "lunch-agent");
  assert.match(problem!.message, /model\.provider/);
});

test("rejects a literal credential", () => {
  const problem = checkAgentAfm(VALID.replace('"${env:MODEL_API_KEY}"', '"sk-ant-real"'), "lunch-agent");
  assert.match(problem!.message, /must be an \$\{env:...\} reference/);
});

test("rejects an unsupported interface type", () => {
  const problem = checkAgentAfm(VALID.replace("type: webchat", "type: webhook"), "lunch-agent");
  assert.match(problem!.message, /webchat/);
});

test("rejects a spec path field", () => {
  const problem = checkAgentAfm(
    VALID.replace('        baseUrl:', '        spec: "./openapi.yaml"\n        baseUrl:'),
    "lunch-agent",
  );
  assert.match(problem!.message, /unknown property/);
});

test("rejects a name that is not the directory name", () => {
  const problem = checkAgentAfm(VALID, "other-agent");
  assert.match(problem!.message, /must equal the component directory name/);
});

test("rejects a body with no # Role", () => {
  const problem = checkAgentAfm(VALID.replace("# Role", "# Purpose"), "lunch-agent");
  assert.match(problem!.message, /# Role/);
});

test("rejects front matter that is not YAML", () => {
  assert.equal(checkAgentAfm("no front matter here", "lunch-agent")!.code, "INVALID_AFM");
});

test("rejects tools.mcp with a reason, not a bare unknown-property", () => {
  const problem = checkAgentAfm(VALID.replace("x-aep:", "tools:\n  mcp: []\nx-aep:"), "lunch-agent");
  assert.match(problem!.message, /MCP tools are not supported/);
});

test("rejects skills with a reason", () => {
  const problem = checkAgentAfm(VALID.replace("x-aep:", "skills:\n  - type: local\n    path: ./s\nx-aep:"), "lunch-agent");
  assert.match(problem!.message, /agent skills are not supported/);
});

test("accepts x-aep.memory.type server", () => {
  const content = VALID.replace("x-aep:\n", 'x-aep:\n  memory:\n    type: "server"\n');
  assert.equal(checkAgentAfm(content, "lunch-agent"), null);
});

test("rejects an unknown memory type", () => {
  const content = VALID.replace("x-aep:\n", 'x-aep:\n  memory:\n    type: "shared"\n');
  const problem = checkAgentAfm(content, "lunch-agent");
  assert.ok(problem, "expected a problem for memory.type shared");
});

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
 * Two properties matter here and neither is about wiring. The model must not
 * be able to name the dedupe key or the adoption flag — they are not in the
 * schema it reads. And actionStatuses, once declared, must be REQUIRED: a call
 * that omits it is a schema-validation rejection, not a silent skip of
 * classification.
 */

import assert from "node:assert/strict";
import { test } from "node:test";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

import { createAepMcpServer } from "./server.js";

interface RegisteredTool {
  inputSchema: { shape: Record<string, unknown> };
  handler: (args: unknown) => unknown;
}

function server() {
  return createAepMcpServer({ baseUrl: "http://aep-api", bearer: "Bearer t" });
}

function toolSchema(name: string): Record<string, unknown> {
  const registered = (server() as unknown as { _registeredTools: Record<string, RegisteredTool> })._registeredTools;
  return registered[name]!.inputSchema.shape;
}

test("the handoff server exposes exactly the two SRE handoff tools", () => {
  const registered = (server() as unknown as { _registeredTools: Record<string, RegisteredTool> })._registeredTools;

  assert.deepEqual(Object.keys(registered).sort(), ["create_issue", "search_related_issues"]);
});

test("the model is never shown the dedupe key or the adoption flag", () => {
  const schema = toolSchema("create_issue");

  assert.equal("dedupeKey" in schema, false);
  assert.equal("adopt" in schema, false);
  assert.equal("componentName" in schema, true);
  assert.equal("project" in schema, true);
});

test("actionStatuses is a required field in the schema (no .optional())", () => {
  const schema = toolSchema("create_issue");
  const field = schema.actionStatuses as { isOptional?: () => boolean } | undefined;

  assert.ok(field, "actionStatuses must be declared in the schema");
  assert.equal(field?.isOptional?.(), false);
});

test("project and componentName pass straight through to aep-api", async () => {
  const seen: unknown[] = [];
  const s = createAepMcpServer({ baseUrl: "http://aep-api", bearer: "Bearer t" }, async (_opts, project, req) => {
    seen.push({ project, req });
    return { number: 1, url: "u", nodeId: "n" };
  });
  const registered = (s as unknown as { _registeredTools: Record<string, RegisteredTool> })._registeredTools;
  const handler = registered["create_issue"]!.handler;

  await handler({
    project: "myproj",
    title: "t",
    body: "b",
    labels: ["mine"],
    componentName: "service1",
    actionStatuses: ["revised", null],
  });

  assert.deepEqual(seen, [
    {
      project: "myproj",
      req: {
        title: "t",
        body: "b",
        labels: ["mine", "bug", "incident"],
        componentName: "service1",
        actionStatuses: ["revised", null],
      },
    },
  ]);
});

test("a call omitting actionStatuses is rejected before the handler forwards anything", async () => {
  const seen: unknown[] = [];
  const s = createAepMcpServer({ baseUrl: "http://aep-api", bearer: "Bearer t" }, async (_opts, _project, req) => {
    seen.push(req);
    return { number: 1, url: "u", nodeId: "n" };
  });
  // Through a connected client rather than the raw handler: the point under
  // test is that the SCHEMA rejects the call, which only the SDK's own
  // tools/call path exercises.
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "server-test", version: "0.0.0" });
  await s.connect(serverTransport);
  await client.connect(clientTransport);

  try {
    const result = await client.callTool({
      name: "create_issue",
      arguments: { project: "p", title: "t", body: "b" },
    });

    assert.equal(result.isError, true);
    const [content] = result.content as { type: string; text: string }[];
    assert.match(content?.text ?? "", /Input validation error/);
    assert.match(content?.text ?? "", /actionStatuses/);
    assert.deepEqual(seen, []);
  } finally {
    await client.close();
    await s.close();
  }
});

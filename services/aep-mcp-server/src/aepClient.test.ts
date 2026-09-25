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
 * The recurrence facts are the ones a reader must not lose: `reopened` and
 * `recurrence` say a fix AE already merged for this incident did not hold, which
 * is a different situation from a new bug. They were undeclared on IssueResult
 * and survived only by JSON pass-through.
 */

import assert from "node:assert/strict";
import { mock, test } from "node:test";

import { createIssue, listIssues } from "./aepClient.js";

test("every answer field aep-api returns reaches the caller", async () => {
  const body = {
    number: 12,
    url: "https://github.com/o/r/issues/12",
    nodeId: "I_1",
    deduped: false,
    adopted: true,
    reopened: true,
    recurrence: 3,
    suppressed: false,
  };
  mock.method(globalThis, "fetch", async () =>
    new Response(JSON.stringify(body), { status: 200 }),
  );

  const result = await createIssue(
    { baseUrl: "http://aep-api", bearer: "Bearer t" },
    "proj",
    { title: "t", body: "b" },
  );

  assert.equal(result.reopened, true);
  assert.equal(result.recurrence, 3);
  assert.equal(result.suppressed, false);
  mock.restoreAll();
});

test("issue search forwards the space-separated query to the issue list API", async () => {
  let seen: { url: string; authorization?: string } | undefined;
  mock.method(globalThis, "fetch", async (url: string | URL | Request, init?: RequestInit) => {
    const headers = init?.headers as Record<string, string> | undefined;
    seen = {
      url: String(url),
      ...(headers?.authorization !== undefined ? { authorization: headers.authorization } : {}),
    };
    return new Response(JSON.stringify([]), { status: 200 });
  });

  await listIssues(
    { baseUrl: "http://aep-api", bearer: "Bearer token" },
    "project/a",
    { query: "service timeout panic", labels: ["bug", "incident"] },
  );

  assert.equal(
    seen?.url,
    "http://aep-api/api/v1/projects/project%2Fa/issues?labels=bug%2Cincident&q=service+timeout+panic",
  );
  assert.equal(seen?.authorization, "Bearer token");
  mock.restoreAll();
});

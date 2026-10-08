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

// @vitest-environment jsdom

import { getResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Handing an issue to the coding agent in mock mode: refused, in aep-api's
// words, until the mock has a deployed version (a built one); once handed
// over, the issue's task shows the coding agent's run, so its log takes over.

let deployed: string | null = null;
vi.mock("../buildsState", () => ({ deployedVersion: () => deployed }));

const { issuesHandlers } = await import("./issues");

const BASE = "http://localhost/api/v1/projects/acme-expenses/tasks/11";

function promote(componentName = "expense-api") {
  return getResponse(
    issuesHandlers,
    new Request(`${BASE}/promote-from-issue`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ componentName }),
    }),
  );
}

async function codingRuns(): Promise<unknown[]> {
  const res = await getResponse(issuesHandlers, new Request(BASE));
  const task = (await res?.json()) as { executions: Record<string, { kind: string }> };
  return Object.values(task.executions).filter((e) => e.kind === "coding");
}

beforeEach(() => {
  sessionStorage.clear();
  deployed = null;
});

describe("promote-from-issue in mock mode", () => {
  it("refuses with 409 while there is no deployed version", async () => {
    const res = await promote();
    expect(res?.status).toBe(409);
    expect(await res?.json()).toMatchObject({
      message: "Deploy a version first: the coding agent works in a deployed version's milestone.",
    });
    expect(await codingRuns()).toEqual([]);
  });

  it("accepts with 202 once a version is deployed, and the issue's task shows the coding run", async () => {
    deployed = "v1";
    const res = await promote();
    expect(res?.status).toBe(202);
    expect(res?.headers.get("Content-Length")).toBe("0");
    expect(await codingRuns()).toHaveLength(1);
  });
});

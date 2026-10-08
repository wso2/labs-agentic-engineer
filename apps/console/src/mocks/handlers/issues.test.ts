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

// Handing an issue to the coding agent in mock mode, as aep-api answers it:
// refused (409, in its words) while there is no deployed version, for a closed
// issue, and for one the coding agent does not take on; otherwise adopted
// (202, no body): the issue is armed and joins the deployed version's
// milestone, which is what the issue list then shows, a reload included.

let deployed: { version: string; milestoneNumber: number } | null = null;
vi.mock("../buildsState", () => ({ deployedVersion: () => deployed }));

const { issuesHandlers } = await import("./issues");

const API = "http://localhost/api/v1/projects/acme-expenses";

function promote(number: number, componentName = "expense-api") {
  return getResponse(
    issuesHandlers,
    new Request(`${API}/tasks/${number}/promote-from-issue`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ componentName }),
    }),
  );
}

async function listed(number: number): Promise<{ Labels: string[]; milestoneNumber?: number }> {
  const res = await getResponse(issuesHandlers, new Request(`${API}/issues`));
  const issues = (await res?.json()) as { Number: number; Labels: string[]; milestoneNumber?: number }[];
  return issues.find((i) => i.Number === number)!;
}

async function refusal(res: Response | undefined): Promise<string> {
  expect(res?.status).toBe(409);
  return ((await res?.json()) as { message: string }).message;
}

beforeEach(() => {
  sessionStorage.clear();
  deployed = null;
});

describe("promote-from-issue in mock mode", () => {
  it("refuses with 409 while there is no deployed version, and changes nothing", async () => {
    expect(await refusal(await promote(11))).toBe(
      "Deploy a version first: the coding agent works in a deployed version's milestone.",
    );
    expect(await listed(11)).toMatchObject({ Labels: [] });
    expect((await listed(11)).milestoneNumber).toBeUndefined();
  });

  it("adopts with 202 once a version is deployed: the issue is armed, in that version's milestone", async () => {
    deployed = { version: "v2", milestoneNumber: 2 };
    const res = await promote(11);
    expect(res?.status).toBe(202);
    expect(res?.headers.get("Content-Length")).toBe("0");
    expect(await listed(11)).toMatchObject({ Labels: ["aep"], milestoneNumber: 2 });
  });

  it("refuses a closed issue", async () => {
    deployed = { version: "v2", milestoneNumber: 2 };
    expect(await refusal(await promote(9))).toBe("This issue is closed.");
  });

  it("refuses an issue the coding agent does not take on", async () => {
    deployed = { version: "v2", milestoneNumber: 2 };
    expect(await refusal(await promote(3))).toBe("The coding agent doesn't take on this kind of issue.");
  });

  it("serves no coding run on the task: aep-api mints none", async () => {
    deployed = { version: "v2", milestoneNumber: 2 };
    await promote(11);
    const res = await getResponse(issuesHandlers, new Request(`${API}/tasks/11`));
    expect(((await res?.json()) as { executions: object }).executions).toEqual({});
  });
});

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

// Mock mode reads agent activity from the pod's running turn, as the console
// does: a fresh project's kickoff must show as a running turn there, or the
// spec leg, the spec workspace and the rail all read idle.

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { aeStudioUrls } from "../fixtures/aeStudio";
import { agentChatHandlers } from "./agent-chat";

const server = setupServer(...agentChatHandlers);
const V1 = `${aeStudioUrls.designAgent}/v1`;

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => localStorage.clear());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

async function drain(res: Response): Promise<string> {
  return await res.text();
}

describe("GET turns/active in mock mode", () => {
  it("answers 204 when nothing runs", async () => {
    localStorage.setItem("aep:mock:project", "spec");
    const res = await fetch(`${V1}/projects/demo-shop/turns/active`);
    expect(res.status).toBe(204);
  });

  it("answers the fresh project's running kickoff, until its stream has played", async () => {
    localStorage.setItem("aep:mock:project", "fresh");
    const res = await fetch(`${V1}/projects/fresh-shop/turns/active`);
    expect(res.status).toBe(200);
    const turn = (await res.json()) as { turnId: string; kind: string; flow: string; status: string };
    expect(turn).toMatchObject({ kind: "kickoff", flow: "start", status: "running" });

    // Asking again before anyone streams it: still running.
    expect((await fetch(`${V1}/projects/fresh-shop/turns/active`)).status).toBe(200);

    const stream = await fetch(`${V1}/projects/fresh-shop/turns/${turn.turnId}/stream?from=0`);
    expect(await drain(stream)).toContain("data: [DONE]");

    expect((await fetch(`${V1}/projects/fresh-shop/turns/active`)).status).toBe(204);
  }, 30_000);
});

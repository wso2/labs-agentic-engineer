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

import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/token", () => ({
  getAccessToken: () => Promise.resolve("tok"),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

const { setAeStudioUrls } = await import("../../../api/aeStudio");
const { fetchCurrentConversationId, rotateConversation } = await import("./conversations");

const DESIGN = "http://ae-design-agent.mock";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => setAeStudioUrls({ tools: "http://ae-studio-tools.mock", designAgent: DESIGN }));
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

const view = (conversationId: string) => ({
  conversationId,
  createdAt: "2026-10-03T00:00:00Z",
  current: true,
});

describe("the project's thread on the design agent", () => {
  it("resolves the current thread with the user's token", async () => {
    let auth: string | null = null;
    server.use(
      http.get(`${DESIGN}/v1/projects/p/conversations/current`, ({ request }) => {
        auth = request.headers.get("authorization");
        return HttpResponse.json(view("c-1"));
      }),
    );
    await expect(fetchCurrentConversationId("p")).resolves.toBe("c-1");
    expect(auth).toBe("Bearer tok");
  });

  it("rotates to a fresh thread (201)", async () => {
    server.use(http.post(`${DESIGN}/v1/projects/p/conversations`, () => HttpResponse.json(view("c-2"), { status: 201 })));
    await expect(rotateConversation("p")).resolves.toBe("c-2");
  });

  it("refuses to rotate while a turn runs (409 turn_in_progress)", async () => {
    server.use(
      http.post(`${DESIGN}/v1/projects/p/conversations`, () =>
        HttpResponse.json({ code: "turn_in_progress", activeTurnId: "t-1" }, { status: 409 }),
      ),
    );
    await expect(rotateConversation("p")).rejects.toThrow(/turn is running/i);
  });

  it("surfaces the pod's sentence when it cannot resolve the thread", async () => {
    server.use(
      http.get(`${DESIGN}/v1/projects/p/conversations/current`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Not Found", status: 404, code: "project_unknown", detail: "project p is unknown" },
          { status: 404 },
        ),
      ),
    );
    await expect(fetchCurrentConversationId("p")).rejects.toThrow("project p is unknown");
  });
});

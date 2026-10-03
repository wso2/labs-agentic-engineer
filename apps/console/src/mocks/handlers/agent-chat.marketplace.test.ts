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

// Mock mode serves the marketplace register chat on the pod's
// /v1/marketplace/* routes, so the register page is drivable without a pod.

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { setupServer } from "msw/node";
import { REGISTER_EXTERNAL_RESOURCE_COMMAND } from "@aep/contracts/commands";
import { aeStudioUrls } from "../fixtures/aeStudio";
import { agentChatHandlers } from "./agent-chat";

const server = setupServer(...agentChatHandlers);
const MARKET = `${aeStudioUrls.designAgent}/v1/marketplace`;

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => localStorage.clear());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("the marketplace register chat in mock mode", () => {
  it("starts a conversation, takes a turn in it, and streams the scripted register draft", async () => {
    const started = await fetch(`${MARKET}/conversations`, { method: "POST" });
    expect(started.status).toBe(201);
    const { conversationId } = (await started.json()) as { conversationId: string };

    const turn = await fetch(`${MARKET}/conversations/${conversationId}/turns`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ instruction: `${REGISTER_EXTERNAL_RESOURCE_COMMAND} Register Stripe` }),
    });
    expect(turn.status).toBe(202);
    const { turnId } = (await turn.json()) as { turnId: string };

    const history = await fetch(`${MARKET}/conversations/${conversationId}/messages`);
    expect(await history.json()).toMatchObject({ messages: [{ role: "user" }] });

    const status = await fetch(`${MARKET}/turns/${turnId}`);
    expect(await status.json()).toMatchObject({ turnId, conversationId, status: "completed" });

    const stream = await fetch(`${MARKET}/turns/${turnId}/stream?from=0`);
    const body = await stream.text();
    expect(body).toContain("draftExternalResource");
    expect(body).toContain("turn-completed");
  }, 30_000);
});

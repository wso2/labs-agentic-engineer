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

// The chat's turn transport on the org's design agent (`/v1` of the AE Studio
// pod): what it puts on the wire and how it reads the pod's answers.

vi.mock("../../../auth/token", () => ({
  getAccessToken: () => Promise.resolve(null),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

const { AE_STUDIO_RESTARTING, AeStudioNotReadyError, setAeStudioUrls } = await import("../../../api/aeStudio");
const {
  ConversationRotatedError,
  TurnInProgressError,
  getActiveTurn,
  getTurn,
  isTurnStreamNotFound,
  isTurnStreamReplayTruncated,
  openTurnStream,
  startTurn,
} = await import("./turns");

const POD = "http://ae-design-agent.mock/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => setAeStudioUrls({ designAgent: "http://ae-design-agent.mock" }));
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

const problem = (status: number, code: string, detail: string, headers: Record<string, string> = {}) =>
  HttpResponse.json(
    { type: "about:blank", title: "x", status, code, detail },
    { status, headers: { "Content-Type": "application/problem+json", ...headers } },
  );

const STATUS = {
  turnId: "11111111-1111-1111-1111-111111111111",
  conversationId: "22222222-2222-2222-2222-222222222222",
  kind: "browser",
  flow: "",
  status: "running",
  instruction: "Go",
  authorId: "sub-1",
  authorDisplayName: "Dev",
  createdAt: "2026-10-07T09:00:00Z",
} as const;

describe("startTurn", () => {
  it("posts the body, as sent, to the conversation's turns on the pod and returns the turn id", async () => {
    let seen: unknown;
    server.use(
      http.post(`${POD}/projects/shop/conversations/conv-1/turns`, async ({ request }) => {
        seen = await request.json();
        return HttpResponse.json({ turnId: "t-1" }, { status: 202 });
      }),
    );
    const body = { instruction: "Go", scope: { kind: "feature" as const, feature: "F2" } };
    expect(await startTurn("shop", "conv-1", body)).toBe("t-1");
    expect(seen).toEqual(body);
  });

  it("reads a 409 turn_in_progress as the running turn, by id", async () => {
    server.use(
      http.post(`${POD}/projects/shop/conversations/conv-1/turns`, () =>
        HttpResponse.json({ code: "turn_in_progress", activeTurnId: "t-9" }, { status: 409 }),
      ),
    );
    const err = await startTurn("shop", "conv-1", { instruction: "Go" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TurnInProgressError);
    expect((err as InstanceType<typeof TurnInProgressError>).activeTurnId).toBe("t-9");
  });

  it("reads a 409 conversation_rotated as a replaced thread", async () => {
    server.use(
      http.post(`${POD}/projects/shop/conversations/conv-1/turns`, () =>
        HttpResponse.json({ code: "conversation_rotated" }, { status: 409 }),
      ),
    );
    await expect(startTurn("shop", "conv-1", { instruction: "Go" })).rejects.toBeInstanceOf(ConversationRotatedError);
  });

  it.each([
    [400, "invalid_turn", "scope must be {kind, feature?}"],
    [409, "no_default_key", "No model is connected for this organization."],
  ])("carries the pod's words for a %i %s problem", async (status, code, detail) => {
    server.use(http.post(`${POD}/projects/shop/conversations/conv-1/turns`, () => problem(status, code, detail)));
    const err = await startTurn("shop", "conv-1", { instruction: "Go" }).catch((e: unknown) => e);
    expect(err).toMatchObject({ message: detail, code });
  });

  it("says AE Studio is restarting for a 503, and before AE Studio is ready", async () => {
    server.use(
      http.post(`${POD}/projects/shop/conversations/conv-1/turns`, () =>
        problem(503, "shutting_down", "draining", { "Retry-After": "5" }),
      ),
    );
    await expect(startTurn("shop", "conv-1", { instruction: "Go" })).rejects.toThrow(AE_STUDIO_RESTARTING);
    setAeStudioUrls(null);
    await expect(startTurn("shop", "conv-1", { instruction: "Go" })).rejects.toBeInstanceOf(AeStudioNotReadyError);
  });
});

describe("getActiveTurn", () => {
  it("is the running turn, or null when the pod says none (204)", async () => {
    server.use(http.get(`${POD}/projects/shop/turns/active`, () => HttpResponse.json(STATUS)));
    expect(await getActiveTurn("shop")).toEqual(STATUS);
    server.use(http.get(`${POD}/projects/shop/turns/active`, () => new HttpResponse(null, { status: 204 })));
    expect(await getActiveTurn("shop")).toBeNull();
  });

  it("is null when it cannot be read: a 503, or AE Studio not ready", async () => {
    server.use(http.get(`${POD}/projects/shop/turns/active`, () => problem(503, "shutting_down", "x")));
    expect(await getActiveTurn("shop")).toBeNull();
    setAeStudioUrls(null);
    expect(await getActiveTurn("shop")).toBeNull();
  });
});

describe("getTurn", () => {
  it("is the turn's status", async () => {
    server.use(http.get(`${POD}/projects/shop/turns/t-1`, () => HttpResponse.json({ ...STATUS, status: "completed" })));
    expect(await getTurn("shop", "t-1")).toMatchObject({ status: "completed" });
  });

  it("is gone when the pod no longer holds the turn (404), and null when it cannot say", async () => {
    server.use(http.get(`${POD}/projects/shop/turns/t-1`, () => problem(404, "turn_unknown", "x")));
    expect(await getTurn("shop", "t-1")).toBe("gone");
    server.use(http.get(`${POD}/projects/shop/turns/t-1`, () => problem(503, "shutting_down", "x")));
    expect(await getTurn("shop", "t-1")).toBeNull();
    server.use(http.get(`${POD}/projects/shop/turns/t-1`, () => HttpResponse.error()));
    expect(await getTurn("shop", "t-1")).toBeNull();
  });
});

describe("openTurnStream", () => {
  it("attaches at the frame asked for", async () => {
    let from: string | null = null;
    server.use(
      http.get(`${POD}/projects/shop/turns/t-1/stream`, ({ request }) => {
        from = new URL(request.url).searchParams.get("from");
        return new HttpResponse("data: [DONE]\n\n", { headers: { "Content-Type": "text/event-stream" } });
      }),
    );
    const body = await openTurnStream("shop", "t-1", 7, new AbortController().signal);
    expect(body).toBeInstanceOf(ReadableStream);
    expect(from).toBe("7");
  });

  it("tells a turn the pod does not stream (404) from a replay it refused as truncated (409)", async () => {
    server.use(http.get(`${POD}/projects/shop/turns/t-1/stream`, () => problem(404, "turn_unknown", "x")));
    const gone = await openTurnStream("shop", "t-1", 0, new AbortController().signal).catch((e: unknown) => e);
    expect(isTurnStreamNotFound(gone)).toBe(true);
    expect(isTurnStreamReplayTruncated(gone)).toBe(false);
    server.use(http.get(`${POD}/projects/shop/turns/t-1/stream`, () => problem(409, "replay_truncated", "x")));
    const truncated = await openTurnStream("shop", "t-1", 0, new AbortController().signal).catch((e: unknown) => e);
    expect(isTurnStreamReplayTruncated(truncated)).toBe(true);
    expect(isTurnStreamNotFound(truncated)).toBe(false);
  });
});

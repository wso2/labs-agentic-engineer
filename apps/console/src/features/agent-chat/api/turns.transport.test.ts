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

// What the turn calls actually put ON THE WIRE to the org's design agent
// (ae-design-agent /v1, reached through designAgent()), and how each of its
// refusals reaches the chat. A value carried correctly up to a boundary and
// never across it is the defect shape this file exists for (#428).

import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/token", () => ({
  getAccessToken: () => Promise.resolve("tok"),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

const { setAeStudioUrls, AeStudioNotReadyError } = await import("../../../api/aeStudio");
const {
  ConversationRotatedError,
  TurnInProgressError,
  getActiveTurn,
  getConversationMessages,
  getTurn,
  isTurnStreamNotFound,
  isTurnStreamReplayTruncated,
  openTurnStream,
  readTurnStatus,
  startTurn,
} = await import("./turns");

const DESIGN = "http://ae-design-agent.mock";
const TURNS = `${DESIGN}/v1/projects/p/conversations/c/turns`;
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => setAeStudioUrls({ tools: "http://ae-studio-tools.mock", designAgent: DESIGN }));
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

interface Seen {
  auth: string | null;
  contentType: string;
  json?: unknown;
  form?: FormData;
}

/** Answer the turn start with 202 and record what arrived. */
function acceptTurns(): Seen[] {
  const seen: Seen[] = [];
  server.use(
    http.post(TURNS, async ({ request }) => {
      const contentType = request.headers.get("content-type") ?? "";
      const entry: Seen = { auth: request.headers.get("authorization"), contentType };
      if (contentType.includes("multipart/form-data")) entry.form = await request.formData();
      else entry.json = await request.json();
      seen.push(entry);
      return HttpResponse.json({ turnId: "t-1" }, { status: 202 });
    }),
  );
  return seen;
}

function refuse(status: number, body: Record<string, unknown>, problem = true) {
  server.use(
    http.post(TURNS, () =>
      HttpResponse.json(body, {
        status,
        headers: { "Content-Type": problem ? "application/problem+json" : "application/json" },
      }),
    ),
  );
}

const problem = (status: number, code: string, detail: string) => ({
  type: "about:blank",
  title: "refused",
  status,
  code,
  detail,
});

const fileOf = (name: string, body = "x") => new File([body], name);

const anchor = {
  file: "specs/requirements/PRD.md",
  nodes: [{ name: "Rounds close automatically.", kind: "paragraph" }],
};

describe("startTurn on the design agent", () => {
  it("posts JSON to the pod's conversation turns with the user's token, and no collab or workspace field", async () => {
    const seen = acceptTurns();
    await expect(startTurn("p", "c", { instruction: "tidy the requirements" })).resolves.toEqual({
      turnId: "t-1",
    });
    expect(seen).toHaveLength(1);
    expect(seen[0]!.auth).toBe("Bearer tok");
    expect(seen[0]!.contentType).toContain("application/json");
    expect(seen[0]!.json).toEqual({ instruction: "tidy the requirements" });
  });

  it("puts the anchor and intent beside the instruction, never inside it", async () => {
    const seen = acceptTurns();
    await startTurn("p", "c", { instruction: "shorter please", aiming: { anchor, intent: "change" } });
    expect(seen[0]!.json).toEqual({ instruction: "shorter please", anchor, intent: "change" });
  });

  it("switches to multipart when files ride along: every file, the anchor as a JSON part, no collab", async () => {
    const seen = acceptTurns();
    await startTurn("p", "c", {
      instruction: "what is wrong here?",
      files: [fileOf("error.png"), fileOf("rows.csv", "a,b")],
      aiming: { anchor, intent: "discuss" },
    });
    const form = seen[0]!.form!;
    expect(seen[0]!.auth).toBe("Bearer tok");
    expect(form.get("instruction")).toBe("what is wrong here?");
    expect(form.getAll("files").map((f) => (f as File).name)).toEqual(["error.png", "rows.csv"]);
    expect(JSON.parse(await (form.get("anchor") as Blob).text())).toEqual(anchor);
    expect(form.get("intent")).toBe("discuss");
    expect(form.get("collab")).toBeNull();
    expect(form.get("workspace")).toBeNull();
  });

  it("sends JSON when the files array is empty", async () => {
    const seen = acceptTurns();
    await startTurn("p", "c", { instruction: "hello", files: [] });
    expect(seen[0]!.json).toEqual({ instruction: "hello" });
  });

  it("throws AeStudioNotReadyError before AE Studio is ready", async () => {
    setAeStudioUrls(null);
    await expect(startTurn("p", "c", { instruction: "hi" })).rejects.toBeInstanceOf(AeStudioNotReadyError);
  });
});

describe("startTurn refusals", () => {
  it("names a rotated thread (TurnConflict conversation_rotated)", async () => {
    refuse(409, { code: "conversation_rotated" }, false);
    await expect(startTurn("p", "c", { instruction: "hi" })).rejects.toBeInstanceOf(ConversationRotatedError);
  });

  it("carries the running turn's id on turn_in_progress", async () => {
    refuse(409, { code: "turn_in_progress", activeTurnId: "t-running" }, false);
    const err = await startTurn("p", "c", { instruction: "hi" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TurnInProgressError);
    expect(err).toMatchObject({ activeTurnId: "t-running", status: 409, code: "turn_in_progress" });
    expect((err as Error).message).toMatch(/already running/);
  });

  it("sends the user to Settings when the org has no default model key", async () => {
    refuse(409, problem(409, "no_default_key", "the organization has no default model key"));
    const err = await startTurn("p", "c", { instruction: "hi" }).catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 409, code: "no_default_key" });
    expect((err as Error).message).toMatch(/model connection/i);
    expect((err as Error).message).toMatch(/Settings/);
  });

  it("says the studio's tools are not answering on 503 tools_unavailable", async () => {
    refuse(503, problem(503, "tools_unavailable", "the studio's tools are not answering"));
    const err = await startTurn("p", "c", { instruction: "hi" }).catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 503, code: "tools_unavailable" });
    expect((err as Error).message).toMatch(/try again/i);
  });

  it("says AE Studio is restarting on 503 shutting_down", async () => {
    refuse(503, problem(503, "shutting_down", "the design agent is shutting down"));
    const err = await startTurn("p", "c", { instruction: "hi" }).catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 503, code: "shutting_down" });
    expect((err as Error).message).toMatch(/restarting/i);
  });

  it("passes the pod's own sentence through for a rejected attachment", async () => {
    refuse(400, problem(400, "attachment_rejected", "a.exe: unsupported type"));
    await expect(startTurn("p", "c", { instruction: "hi", files: [fileOf("a.exe")] })).rejects.toThrow(
      "a.exe: unsupported type",
    );
  });

  it("falls back to a generic sentence when the pod does not answer", async () => {
    server.use(http.post(TURNS, () => HttpResponse.error()));
    await expect(startTurn("p", "c", { instruction: "hi" })).rejects.toThrow(/start the agent turn/);
  });
});

describe("turn status and stream on the design agent", () => {
  const status = {
    turnId: "t-1",
    project: "p",
    conversationId: "c",
    kind: "browser",
    flow: "",
    status: "running",
    instruction: "hi",
    authorId: "sub-1",
    authorDisplayName: "Ada",
    createdAt: "2026-10-03T00:00:00Z",
  };

  it("reads the running turn, null on 204", async () => {
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/active`, () => HttpResponse.json(status)));
    await expect(getActiveTurn("p")).resolves.toEqual(status);
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/active`, () => new HttpResponse(null, { status: 204 })));
    await expect(getActiveTurn("p")).resolves.toBeNull();
  });

  // The active-turn query keeps its last answer through a failed read, so a
  // pod hiccup must not read as "nothing is running".
  it("throws on a failed read instead of answering no turn", async () => {
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/active`, () =>
        HttpResponse.json(problem(503, "shutting_down", "rolling"), { status: 503 }),
      ),
    );
    await expect(getActiveTurn("p")).rejects.toThrow();
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/active`, () => HttpResponse.error()));
    await expect(getActiveTurn("p")).rejects.toThrow();
  });

  it("throws AeStudioNotReadyError while AE Studio is not ready", async () => {
    setAeStudioUrls(null);
    await expect(getActiveTurn("p")).rejects.toBeInstanceOf(AeStudioNotReadyError);
  });

  it("reads a finished turn the pod still answers as none running", async () => {
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/active`, () => HttpResponse.json({ ...status, status: "completed" })),
    );
    await expect(getActiveTurn("p")).resolves.toBeNull();
  });

  it("reads one turn, null when the pod no longer holds it", async () => {
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/t-1`, () => HttpResponse.json({ ...status, status: "completed" })));
    await expect(getTurn("p", "t-1")).resolves.toMatchObject({ status: "completed" });
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/t-1`, () =>
        HttpResponse.json(problem(404, "turn_unknown", "no such turn"), { status: 404 }),
      ),
    );
    await expect(getTurn("p", "t-1")).resolves.toBeNull();
  });

  // The truncated-replay wait must tell "the turn is gone" from "the pod did
  // not answer right now": only the first one ends it.
  it("tells a turn the pod no longer holds (404) from a read that failed for now", async () => {
    const turnUrl = `${DESIGN}/v1/projects/p/turns/t-1`;
    server.use(http.get(turnUrl, () => HttpResponse.json({ ...status, status: "completed" })));
    await expect(readTurnStatus("p", "t-1")).resolves.toEqual({ kind: "status", status: { ...status, status: "completed" } });

    server.use(http.get(turnUrl, () => HttpResponse.json(problem(404, "turn_unknown", "no such turn"), { status: 404 })));
    await expect(readTurnStatus("p", "t-1")).resolves.toEqual({ kind: "gone" });

    server.use(http.get(turnUrl, () => HttpResponse.json(problem(503, "shutting_down", "rolling"), { status: 503 })));
    await expect(readTurnStatus("p", "t-1")).resolves.toEqual({ kind: "unavailable" });

    server.use(http.get(turnUrl, () => HttpResponse.error()));
    await expect(readTurnStatus("p", "t-1")).resolves.toEqual({ kind: "unavailable" });

    setAeStudioUrls(null);
    await expect(readTurnStatus("p", "t-1")).resolves.toEqual({ kind: "unavailable" });
  });

  it("attaches to the stream from the given frame, with the user's token", async () => {
    let seen: { from: string | null; auth: string | null } | undefined;
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/t-1/stream`, ({ request }) => {
        seen = { from: new URL(request.url).searchParams.get("from"), auth: request.headers.get("authorization") };
        return new HttpResponse('id: 0\ndata: {"type":"turn-completed"}\n\ndata: [DONE]\n\n', {
          headers: { "Content-Type": "text/event-stream" },
        });
      }),
    );
    const body = await openTurnStream("p", "t-1", 3, new AbortController().signal);
    expect(await new Response(body).text()).toContain("turn-completed");
    expect(seen).toEqual({ from: "3", auth: "Bearer tok" });
  });

  it("tells a turn past its retention (404) from a truncated replay (409 replay_truncated)", async () => {
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/t-1/stream`, () =>
        HttpResponse.json(problem(404, "turn_unknown", "gone"), { status: 404 }),
      ),
    );
    const gone = await openTurnStream("p", "t-1", 0, new AbortController().signal).catch((e: unknown) => e);
    expect(isTurnStreamNotFound(gone)).toBe(true);
    expect(isTurnStreamReplayTruncated(gone)).toBe(false);

    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/t-1/stream`, () =>
        HttpResponse.json(problem(409, "replay_truncated", "overflowed"), { status: 409 }),
      ),
    );
    const truncated = await openTurnStream("p", "t-1", 0, new AbortController().signal).catch((e: unknown) => e);
    expect(isTurnStreamReplayTruncated(truncated)).toBe(true);
    expect(isTurnStreamNotFound(truncated)).toBe(false);
  });
});

describe("getConversationMessages on the design agent", () => {
  const url = `${DESIGN}/v1/projects/p/conversations/c/messages`;

  it("maps the thread's messages, the author being the sender's sub", async () => {
    server.use(
      http.get(url, () =>
        HttpResponse.json({
          messages: [
            { role: "user", content: "hi", author: { id: "sub-1", displayName: "Ada" }, attachments: ["a.md"] },
            { role: "assistant", content: "hello" },
          ],
        }),
      ),
    );
    await expect(getConversationMessages("p", "c")).resolves.toEqual([
      { role: "user", content: "hi", author: { id: "sub-1", displayName: "Ada" }, attachments: ["a.md"] },
      { role: "assistant", content: "hello" },
    ]);
  });

  it("answers [] for a known thread with no turns, null for an unknown one", async () => {
    server.use(http.get(url, () => HttpResponse.json({ messages: [] })));
    await expect(getConversationMessages("p", "c")).resolves.toEqual([]);
    server.use(http.get(url, () => HttpResponse.json(problem(404, "conversation_unknown", "no"), { status: 404 })));
    await expect(getConversationMessages("p", "c")).resolves.toBeNull();
  });

  // The rehydrate runs on mount, poll and refocus; while AE Studio restarts
  // (URLs dropped) or does not answer, it keeps the local log instead of
  // rejecting into a trigger that has no catch.
  it("answers null while AE Studio is not ready or does not answer", async () => {
    server.use(http.get(url, () => HttpResponse.error()));
    await expect(getConversationMessages("p", "c")).resolves.toBeNull();
    setAeStudioUrls(null);
    await expect(getConversationMessages("p", "c")).resolves.toBeNull();
  });
});

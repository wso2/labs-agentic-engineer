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

// Agent-chat turn endpoints (#130): start (202), rehydrate (empty), active
// (204), and a scripted SSE stream — narration, one tool result, terminal —
// so the panel is fully drivable in mock mode. Error scenario: instruction
// containing "fail" streams a turn-failed terminal. Grilling scenario (#270):
// an instruction asking for an interview ("grill" / the seeded "interview me"
// directive) streams an ask_question tool-call the panel renders as a
// question card; the answer turn (instruction starting `Answer to "`) streams
// the normal generation flow.

import { http, HttpResponse } from "msw";
import { ANSWER_PREFIX } from "@aep/agent-stream";
import { GRILLING_DIRECTIVE } from "@aep/contracts/prompts";

let turnCounter = 0;
const turnInstruction = new Map<string, string>();

function sse(frames: unknown[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    async start(controller) {
      let id = 0;
      for (const frame of frames) {
        controller.enqueue(
          encoder.encode(`id: ${id}\ndata: ${JSON.stringify(frame)}\n\n`),
        );
        id += 1;
        await new Promise((r) => setTimeout(r, 250)); // visible streaming
      }
      controller.enqueue(encoder.encode("data: [DONE]\n\n"));
      controller.close();
    },
  });
  return new HttpResponse(stream, {
    headers: { "Content-Type": "text/event-stream" },
  });
}

export const agentChatHandlers = [
  http.post("*/api/v1/projects/:projectName/agents/:conversationId/messages", async ({ request }) => {
    const body = (await request.json()) as { instruction?: string };
    turnCounter += 1;
    const turnId = `mock-turn-${turnCounter}`;
    turnInstruction.set(turnId, body.instruction ?? "");
    return HttpResponse.json({ turnId }, { status: 202 });
  }),

  http.get("*/api/v1/projects/:projectName/agents/:conversationId/messages", () =>
    HttpResponse.json({ status: "done", messages: [] }),
  ),

  http.get("*/api/v1/projects/:projectName/turns/active", () =>
    new HttpResponse(null, { status: 204 }),
  ),

  http.get("*/api/v1/projects/:projectName/turns/:turnId/stream", ({ params }) => {
    const turnId = String(params.turnId);
    const instruction = turnInstruction.get(turnId) ?? "";
    const failing = instruction.includes("fail");
    if (failing) {
      return sse([
        { type: "text-delta", delta: "Let me try that…" },
        { type: "turn-failed", message: "Mock turn failure (instruction contained 'fail')." },
      ]);
    }
    // Grilling scenario detection keys on the SHARED contract strings — the
    // seeded directive itself and the answer prefix — plus an explicit typed
    // "grill me"; a mere mention of grilling in an edit instruction must not
    // hijack the file-generation script.
    const grilling =
      !instruction.startsWith(ANSWER_PREFIX) &&
      (instruction.includes(GRILLING_DIRECTIVE) || /\bgrill me\b/i.test(instruction));
    if (grilling) {
      const input = {
        question: "Who is the primary user of this app?",
        options: [
          {
            label: "Individual consumers",
            description: "Self-serve signup, personal workspaces",
            recommended: true,
          },
          { label: "Internal teams", description: "SSO, org-managed access" },
          { label: "Both from day one", description: "Two onboarding paths — more scope" },
        ],
        multiSelect: false,
      };
      return sse([
        { type: "text-delta", delta: "Before I write anything, let me pin the idea down. " },
        { type: "tool-call", toolCallId: "mock-q-1", toolName: "ask_question", input },
        {
          type: "tool-result",
          toolName: "ask_question",
          toolCallId: "mock-q-1",
          input,
          output: { status: "awaiting_user_response", question: input.question },
        },
        { type: "turn-committed", noChanges: true },
      ]);
    }
    return sse([
      { type: "text-delta", delta: "Joining the spec workspace… " },
      { type: "text-delta", delta: "I'll create the requirements now." },
      // Streamed tool input: the panel shows "Creating requirements.md" as soon
      // as the path resolves, then flips to "Created" on the tool-result.
      { type: "tool-input-start", id: "tc-1", toolName: "addFile" },
      {
        type: "tool-input-delta",
        id: "tc-1",
        delta: '{"path":"specs/requirements/requirements.md","content":"# Requirements',
      },
      { type: "tool-input-delta", id: "tc-1", delta: '\\n\\n## Overview\\nA simple todo app.' },
      { type: "tool-input-end", id: "tc-1" },
      {
        type: "tool-result",
        toolName: "addFile",
        toolCallId: "tc-1",
        input: { path: "specs/requirements/requirements.md" },
        output: { ok: true, op: "add", path: "specs/requirements/requirements.md", status: "applied" },
      },
      { type: "text-delta", delta: "\n\nDone — the change is live in the shared doc." },
      { type: "turn-committed", noChanges: true },
    ]);
  }),

  http.get("*/api/v1/projects/:projectName/turns/:turnId", ({ params }) =>
    HttpResponse.json({
      turnId: String(params.turnId),
      conversationId: "mock-conv",
      useCase: "general",
      status: "completed",
      noChanges: true,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }),
  ),
];

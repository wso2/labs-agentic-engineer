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
import { SSE_DONE, isPrototypeFeedback } from "@aep/agent-stream";
import { parsePrototypeCommand } from "@aep/contracts/commands";
import type { ChatView } from "../../features/agent-chat/chatView";
import { scopeOfBody, type TurnBody } from "../../features/agent-chat/turnScope";
import { webApplications } from "../../features/prototype/model/prototypes";
import { projectSpecDoc } from "../../features/spec/collab/specDoc";
import { readSpecLines } from "../../features/spec/collab/useSpecLines";
import type { components } from "../../generated/aep-api";
import { MOCK_USER } from "../../auth/mockSession";
import {
  conversationIdFor,
  finishedTurns,
  findTurn,
  interviewProgress,
  isRunning,
  recordTurn,
  runningTurn,
  setInterviewProgress,
  turnUseCase,
  type MockTurn,
} from "../chatServer";
import { createdProjects } from "../createdProjects";
import { liveDesign } from "../designState";
import { acmeExpensesHistory } from "../fixtures/conversation";
import { scriptDesignTurn } from "../fixtures/designTurns";
import { scriptIssueTurn } from "../fixtures/issueAgent";
import { scriptIssuesTurn } from "../fixtures/issuesAgent";
import { scriptTurn } from "../fixtures/interview";
import { scriptPrototypeTurn } from "../fixtures/prototype";
import { specView } from "../specState";
import { changeMockIssue, fileMockIssue, issueThreadOpen, issuesOf, nextIssueNumber } from "./issues";

type ProjectConversationList = components["schemas"]["ProjectConversationList"];
type GetConversationOutputBody = components["schemas"]["GetConversationOutputBody"];
type ConversationMessage = components["schemas"]["ConversationMessage"];
type TurnOutputBody = components["schemas"]["TurnOutputBody"];
type TurnConflict = components["schemas"]["TurnConflict"];
type TurnStatus = components["schemas"]["TurnStatus"];
type ApiError = components["schemas"]["Error"];

// The project conversation: one stable thread per project, its history, and
// its turns. A turn is started (202), found running (turns/active), read
// (turns/{id}) and streamed as SSE, replayed from its start and then live, so
// a reload mid-turn attaches to it again. One turn at a time per project: a
// second start is the server's 409 `turn_in_progress`. What each turn says
// and does is scripted in fixtures/interview.ts (the design review's in
// fixtures/designTurns.ts, `/prototype` in fixtures/prototype.ts); the turns
// themselves live in chatServer.ts. A turn's `prototypeFeedback` is refused
// as aep-api refuses it.
//
// Acme Expenses starts with a conversation; a project made through New
// project starts with the platform's kickoff (handlers/projects.ts); every
// other project's is empty, which the real server answers the same way.
//
// The Issues Page's chat is a second conversation (`view=issues`) with its own
// agent (fixtures/issuesAgent.ts): a turn with `view: "issues"` goes to it, and
// the running turn is looked up per view, so the two chats never block each other.
// Each open issue has one more (`view=issue` with `issueNumber`), with the
// issue's own agent (fixtures/issueAgent.ts), as use case `issue-<n>`. A
// closed issue has none: resolving or sending to its thread is refused as
// aep-api refuses it (409 `issue_closed`), so closing an issue removes its
// thread; reading its running turn is not refused.

const AUTHOR = { id: "u-developer", displayName: MOCK_USER.name };

const seeded: Record<string, ConversationMessage[]> = {
  [conversationIdFor("acme-expenses")]: acmeExpensesHistory,
};

/** The history: the seed, then each finished turn's message and reply. A running turn is not in it yet. */
function historyFor(conversationId: string): ConversationMessage[] {
  const turns = finishedTurns(conversationId);
  return [
    ...(seeded[conversationId] ?? []),
    ...turns.flatMap((t): ConversationMessage[] => [
      {
        role: "user",
        author: AUTHOR,
        content: t.instruction,
        ...(t.scope ? { scope: t.scope } : {}),
        ...(t.prototypeFeedback ? { prototypeFeedback: t.prototypeFeedback } : {}),
      },
      ...t.reply,
    ]),
  ];
}

const viewOf = (view: string | null | undefined): ChatView => (view === "issues" || view === "issue" ? view : "main");

/** The issue number a request names, from its query (`issueNumber`). */
const issueNumberOf = (url: string): number | undefined => {
  const n = Number(new URL(url).searchParams.get("issueNumber"));
  return Number.isInteger(n) && n > 0 ? n : undefined;
};

const issueClosed = (): Response =>
  HttpResponse.json<ApiError>({ code: "issue_closed", message: "the issue is closed" }, { status: 409 });

function statusOf(turn: MockTurn): TurnStatus {
  const running = isRunning(turn);
  return {
    turnId: turn.turnId,
    conversationId: turn.conversationId,
    useCase: turnUseCase(turn),
    status: running ? "running" : turn.failure ? "failed" : "completed",
    instruction: turn.instruction,
    authorId: AUTHOR.id,
    authorDisplayName: AUTHOR.displayName,
    createdAt: new Date(turn.startedAt).toISOString(),
    updatedAt: new Date().toISOString(),
    ...(running ? {} : { noChanges: !turn.effect?.file }),
    ...(!running && turn.failure ? { message: turn.failure } : {}),
  };
}

/**
 * Why a turn's `prototypeFeedback` is refused, as aep-api refuses it (400,
 * before any turn): a malformed batch, or one on anything but a room turn of
 * `/prototype` (bare, or naming the batch's component) without an anchor.
 */
export function prototypeFeedbackProblem(body: TurnBody): string | null {
  if (body.prototypeFeedback === undefined) return null;
  if (!isPrototypeFeedback(body.prototypeFeedback)) return "prototypeFeedback is malformed";
  const command = parsePrototypeCommand(body.instruction);
  if (!command || (command.component !== null && command.component !== body.prototypeFeedback.component)) {
    return "prototypeFeedback goes only with /prototype for its component";
  }
  if (body.collab !== true || body.anchor || body.intent) return "prototypeFeedback goes only on a room turn without an anchor";
  return null;
}

/** A turn for the Issues view: the Issues agent's, filing the issue it drafted once the user says so. */
function startIssuesTurn(projectName: string, body: TurnBody): MockTurn {
  const conversationId = conversationIdFor(projectName, "issues");
  const history = finishedTurns(conversationId).map((t) => t.instruction);
  const scripted = scriptIssuesTurn(body.instruction, nextIssueNumber(projectName), history, projectName);
  const frames = scripted.frames;
  if (scripted.filed) fileMockIssue(projectName, scripted.filed, Date.now() + (frames.at(-1)?.at ?? 0));
  return recordTurn({ projectName, conversationId, view: "issues", instruction: scripted.display, frames, reply: scripted.reply });
}

/** A turn in an issue's own chat: its agent's, making a confirmed change once the turn ends. */
function startIssueTurn(projectName: string, issueNumber: number, body: TurnBody): MockTurn {
  const conversationId = conversationIdFor(projectName, "issue", issueNumber);
  const history = finishedTurns(conversationId).map((t) => t.instruction);
  const info = issuesOf(projectName).find((i) => i.Number === issueNumber);
  const issue = {
    number: issueNumber,
    title: info?.Title ?? `Issue #${issueNumber}`,
    body: info?.Body ?? "",
    state: info?.State === "closed" ? ("closed" as const) : ("open" as const),
  };
  const scripted = scriptIssueTurn(body.instruction, issue, history);
  const frames = scripted.frames;
  if (scripted.change) changeMockIssue(projectName, issueNumber, scripted.change, Date.now() + (frames.at(-1)?.at ?? 0));
  return recordTurn({ projectName, conversationId, view: "issue", issueNumber, instruction: scripted.display, frames, reply: scripted.reply });
}

/** Start a turn: what the mock agent does with the message, scheduled from now. */
export function startMockTurn(projectName: string, body: TurnBody): MockTurn {
  if (body.view === "issues") return startIssuesTurn(projectName, body);
  if (body.view === "issue" && body.issueNumber) return startIssueTurn(projectName, body.issueNumber, body);
  const scope = scopeOfBody(body);
  const turnKey = `t${Date.now().toString(36)}`;
  const prototypeTurn = parsePrototypeCommand(body.instruction)
    ? scriptPrototypeTurn({
        instruction: body.instruction,
        feedback: body.prototypeFeedback,
        webApps: webApplications(liveDesign(projectName).artifacts),
        doc: projectSpecDoc(projectName, specView(projectName)),
        turnKey,
      })
    : null;
  if (prototypeTurn) {
    return recordTurn({
      projectName,
      conversationId: conversationIdFor(projectName),
      instruction: prototypeTurn.display,
      frames: prototypeTurn.frames,
      reply: prototypeTurn.reply,
      ...(prototypeTurn.files ? { prototype: prototypeTurn.files } : {}),
      ...(body.prototypeFeedback ? { prototypeFeedback: body.prototypeFeedback } : {}),
      ...(prototypeTurn.failure ? { failure: prototypeTurn.failure } : {}),
    });
  }
  const designTurn =
    scope.kind === "design"
      ? scriptDesignTurn({ projectName, instruction: body.instruction, model: specView(projectName), turnKey })
      : null;
  if (designTurn) {
    return recordTurn({
      projectName,
      conversationId: conversationIdFor(projectName),
      instruction: designTurn.display,
      frames: designTurn.frames,
      reply: designTurn.reply,
      ...(designTurn.design ? { design: designTurn.design } : {}),
    });
  }
  const created = createdProjects().find((c) => c.project.name === projectName);
  const model = specView(projectName);
  const scripted = scriptTurn({
    instruction: body.instruction,
    scope,
    model,
    lines: readSpecLines(projectSpecDoc(projectName, model)),
    progress: interviewProgress(projectName),
    prompt: created?.prompt,
    turnKey,
  });
  setInterviewProgress(projectName, scripted.progress);
  return recordTurn({
    projectName,
    conversationId: conversationIdFor(projectName),
    instruction: scripted.display,
    frames: scripted.frames,
    reply: scripted.reply,
    ...(body.scope ? { scope: body.scope } : {}),
    ...(scripted.effect ? { effect: scripted.effect } : {}),
  });
}

/** A turn's frames from `from` on, each sent when its time comes: past ones at once, as a replay. */
function streamOf(turn: MockTurn, from: number): Response {
  const encoder = new TextEncoder();
  let cancelled = false;
  const stream = new ReadableStream<Uint8Array>({
    async start(controller) {
      for (let i = from; i < turn.frames.length; i++) {
        const frame = turn.frames[i]!;
        const wait = turn.startedAt + frame.at - Date.now();
        if (wait > 0) await new Promise((r) => setTimeout(r, wait));
        if (cancelled) return;
        controller.enqueue(encoder.encode(`id: ${i}\ndata: ${JSON.stringify(frame.part)}\n\n`));
      }
      controller.enqueue(encoder.encode(`data: ${SSE_DONE}\n\n`));
      controller.close();
    },
    cancel() {
      cancelled = true;
    },
  });
  return new HttpResponse(stream, { headers: { "Content-Type": "text/event-stream" } });
}

function notFound(what: string): Response {
  return HttpResponse.json<ApiError>({ code: "not_found", message: `${what} not found` }, { status: 404 });
}

export const conversationHandlers = [
  http.get("*/api/v1/projects/:projectName/agents/conversations", ({ params, request }): Response => {
    const projectName = String(params.projectName);
    const view = viewOf(new URL(request.url).searchParams.get("view"));
    const issueNumber = issueNumberOf(request.url);
    if (view === "issue" && (issueNumber === undefined || !issueThreadOpen(projectName, issueNumber))) return issueClosed();
    return HttpResponse.json<ProjectConversationList>({
      conversations: [
        {
          conversationId: conversationIdFor(projectName, view, issueNumber),
          createdAt: "2026-09-29T09:00:00Z",
          createdBy: "Developer",
          current: true,
        },
      ],
    });
  }),

  http.get("*/api/v1/projects/:projectName/agents/:conversationId/messages", ({ params }) =>
    HttpResponse.json<GetConversationOutputBody>({ messages: historyFor(String(params.conversationId)) }),
  ),

  http.post("*/api/v1/projects/:projectName/agents/:conversationId/messages", async ({ params, request }): Promise<Response> => {
    const projectName = String(params.projectName);
    const body = (await request.json()) as TurnBody;
    if (!body.instruction?.trim()) {
      return HttpResponse.json<ApiError>({ code: "invalid_request", message: "instruction is required" }, { status: 400 });
    }
    const feedbackProblem = prototypeFeedbackProblem(body);
    if (feedbackProblem) {
      return HttpResponse.json<ApiError>({ code: "invalid_request", message: feedbackProblem }, { status: 400 });
    }
    const view = viewOf(body.view);
    const issueNumber = view === "issue" ? (body.issueNumber ?? undefined) : undefined;
    if (view === "issue" && (issueNumber === undefined || !issueThreadOpen(projectName, issueNumber))) {
      return HttpResponse.json<TurnConflict>({ code: "issue_closed" }, { status: 409 });
    }
    if (String(params.conversationId) !== conversationIdFor(projectName, view, issueNumber)) {
      return HttpResponse.json<TurnConflict>({ code: "conversation_rotated" }, { status: 409 });
    }
    const running = runningTurn(projectName, view, issueNumber);
    if (running) {
      return HttpResponse.json<TurnConflict>({ code: "turn_in_progress", activeTurnId: running.turnId }, { status: 409 });
    }
    const turn = startMockTurn(projectName, body);
    return HttpResponse.json<TurnOutputBody>({ turnId: turn.turnId }, { status: 202 });
  }),

  http.get("*/api/v1/projects/:projectName/turns/active", ({ params, request }): Response => {
    const running = runningTurn(
      String(params.projectName),
      viewOf(new URL(request.url).searchParams.get("view")),
      issueNumberOf(request.url),
    );
    return running ? HttpResponse.json<TurnStatus>(statusOf(running)) : new HttpResponse(null, { status: 204 });
  }),

  http.get("*/api/v1/projects/:projectName/turns/:turnId/stream", ({ params, request }): Response => {
    const turn = findTurn(String(params.turnId));
    if (!turn) return notFound("Turn");
    const from = Number(new URL(request.url).searchParams.get("from") ?? 0) || 0;
    return streamOf(turn, from);
  }),

  http.get("*/api/v1/projects/:projectName/turns/:turnId", ({ params }): Response => {
    const turn = findTurn(String(params.turnId));
    return turn ? HttpResponse.json<TurnStatus>(statusOf(turn)) : notFound("Turn");
  }),
];

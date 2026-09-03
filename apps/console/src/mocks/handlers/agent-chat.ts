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
// containing "fail" streams a turn-failed terminal.
//
// Multi-user scenarios (task 2, `aep:mock:chat` — see fixtures/chat.ts):
//   "multiuser"      — settled history, two authors, no running turn.
//   "teammate-turn"  — same history, plus a running turn a teammate started
//                      (its triggering message is in the history; the reply
//                      streams from the same scripted SSE builder below).
//   unset            — unchanged: empty rehydrate, no active turn, EXCEPT that
//                      messages sent in this session are echoed back on
//                      rehydrate so chat attachments (#428) can be verified
//                      surviving a reload — the real journal does this
//                      server-side.

import { http, HttpResponse } from "msw";
import { ANSWER_PREFIX, ANSWERS_PREFIX } from "@aep/agent-stream";
import { designPlanFrames } from "./designPlanFrames";
import { registerChatFrames } from "./registerChatFrames";
import {
  MAX_ATTACHMENT_FILES,
  MAX_ATTACHMENT_FILE_BYTES,
  MAX_CHAT_ATTACHMENT_TOTAL_BYTES,
} from "../../features/agent-chat/lib/chatAttachments";
import { isAcceptedAttachment } from "../../lib/attachments";
import {
  activeTeammateTurn,
  multiuserHistory,
  teammateTurnHistory,
  type ChatScenario,
} from "../fixtures/chat";

let turnCounter = 0;
// Distinguishes this page instance's turns from another client's in the same
// collab room: real turn ids are server-minted uuids, but this counter starts
// at 1 in every tab — colliding ids would merge distinct questions in the
// shared Yjs map (and a closed one would suppress a fresh ask).
const instanceId = Math.random().toString(36).slice(2, 8);
const turnInstruction = new Map<string, string>();

/**
 * Messages sent in this browser, per conversation — the mock stand-in for the
 * server's turn journal (#428/#463). It exists so the rehydrate GET can echo a
 * sent message back with its attachment NAMES. Names only, never bytes: that is
 * exactly the journal's contract (ADR-0019).
 *
 * PERSISTED to localStorage, for the same reason handlers/settings.ts persists
 * the org connection: the worker's memory dies with a reload, and the one thing
 * this map exists to demonstrate is that attachment chips SURVIVE a reload. An
 * in-memory map makes the feature look broken in mock mode while it is correct
 * in production — the worst possible mock.
 */
interface JournalMessage {
  role: string;
  content: string;
  attachments?: string[];
}

const JOURNAL_KEY = "aep:mock:chat-journal";

function readJournal(): Record<string, JournalMessage[]> {
  try {
    const raw = localStorage.getItem(JOURNAL_KEY);
    return raw ? (JSON.parse(raw) as Record<string, JournalMessage[]>) : {};
  } catch {
    return {};
  }
}

function appendToJournal(conversationId: string, message: JournalMessage): void {
  const journal = readJournal();
  journal[conversationId] = [...(journal[conversationId] ?? []), message];
  try {
    localStorage.setItem(JOURNAL_KEY, JSON.stringify(journal));
  } catch {
    /* quota — non-fatal in mock mode */
  }
}

/**
 * The server's attachment guard, mirrored for mock mode. Returns the rejection
 * message, or null when the set is admissible. Order matches the real handler:
 * count, then per-file type and size, then the whole message's total.
 */
function rejectAttachments(files: File[]): string | null {
  if (files.length === 0) return "no files in the multipart request";
  if (files.length > MAX_ATTACHMENT_FILES) {
    return `at most ${MAX_ATTACHMENT_FILES} files per message`;
  }
  let total = 0;
  for (const file of files) {
    if (!isAcceptedAttachment(file.name)) {
      return `${file.name}: unsupported type`;
    }
    if (file.size > MAX_ATTACHMENT_FILE_BYTES) {
      return `${file.name} exceeds the 5 MiB per-file limit`;
    }
    total += file.size;
  }
  if (total > MAX_CHAT_ATTACHMENT_TOTAL_BYTES) {
    return `attachments total ${total} bytes, over the 15 MiB per-message limit`;
  }
  return null;
}

function chatScenario(): ChatScenario | null {
  return localStorage.getItem("aep:mock:chat") as ChatScenario | null;
}

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

// Project-scoped threads (#430): the console resolves the current thread
// before it can send or rehydrate ANYTHING, so without these two handlers the
// whole mock-mode chat surface is dead (composer disabled forever).
//
// One stable id per project, PERSISTED — rotation mints a fresh one. Persisted
// because the real BFF owns thread existence in `project_conversations`, so a
// reload lands on the SAME thread; an id minted per page instance meant the
// rehydrate path could not be exercised in mock mode AT ALL, since every reload
// addressed a thread nothing had ever been sent to. (`instanceId` still varies
// per instance for TURN ids, which is deliberate — see its comment above.)
const THREADS_KEY = "aep:mock:chat-threads";

function readThreads(): Record<string, string> {
  try {
    const raw = localStorage.getItem(THREADS_KEY);
    return raw ? (JSON.parse(raw) as Record<string, string>) : {};
  } catch {
    return {};
  }
}

function writeThread(projectName: string, id: string): void {
  try {
    localStorage.setItem(THREADS_KEY, JSON.stringify({ ...readThreads(), [projectName]: id }));
  } catch {
    /* quota — non-fatal in mock mode */
  }
}

let threadCounter = 0;
function threadFor(projectName: string): string {
  const existing = readThreads()[projectName];
  if (existing) return existing;
  threadCounter += 1;
  const id = `mock-thread-${instanceId}-${threadCounter}`;
  writeThread(projectName, id);
  return id;
}
function threadView(id: string) {
  return {
    conversationId: id,
    createdAt: new Date().toISOString(),
    createdBy: "You",
    current: true,
  };
}

export const agentChatHandlers = [
  http.get("*/api/v1/projects/:projectName/agents/conversations", ({ params }) => {
    return HttpResponse.json({
      conversations: [threadView(threadFor(params.projectName as string))],
    });
  }),

  http.post("*/api/v1/projects/:projectName/agents/conversations", ({ params }) => {
    const projectName = params.projectName as string;
    threadCounter += 1;
    const fresh = `mock-thread-${instanceId}-${threadCounter}`;
    // Persisted like the initial mint, so the rotation survives a reload the way
    // a server-side rotation does. The journal is keyed by conversation id, so
    // the fresh thread starts empty with no separate clear — which is also the
    // real guarantee: attachments are conversation-scoped (ADR-0019).
    writeThread(projectName, fresh);
    return HttpResponse.json(threadView(fresh), { status: 201 });
  }),

  http.post("*/api/v1/projects/:projectName/agents/:conversationId/messages", async ({ request, params }) => {
    // Two content types (#428): JSON as before, multipart when the message
    // carries attachments. Reading `request.json()` unconditionally would throw
    // on the multipart body, so the branch is on the header, not a try/catch.
    const isMultipart = (request.headers.get("content-type") ?? "").includes("multipart/form-data");
    let instruction = "";
    let attachments: string[] = [];
    // What the message was aimed at (#666), if anything. Recorded into the
    // journal so a reload paints the tag again — which is the whole reason the
    // anchor is journaled rather than being a live-session nicety.
    let anchor: unknown;
    if (isMultipart) {
      const form = await request.formData();
      instruction = String(form.get("instruction") ?? "");
      const anchorPart = form.get("anchor");
      try {
        if (anchorPart instanceof Blob) anchor = JSON.parse(await anchorPart.text());
        else if (typeof anchorPart === "string" && anchorPart) anchor = JSON.parse(anchorPart);
      } catch {
        // The real server answers a malformed part with its structured 400; a
        // mock that throws instead fails the request with no response at all.
        return HttpResponse.json(
          { code: "invalid_request", message: "anchor must be valid JSON" },
          { status: 400 },
        );
      }
      const files = form.getAll("files").filter((f): f is File => f instanceof File);
      // The server's own guard, mirrored: the console screens first, so a
      // rejection reaching here means a hostile or buggy client. Modelled so the
      // 400 path is exercisable in mock mode rather than only in production.
      const rejection = rejectAttachments(files);
      if (rejection) {
        return HttpResponse.json({ code: "invalid_request", message: rejection }, { status: 400 });
      }
      attachments = files.map((f) => f.name);
    } else {
      const body = (await request.json()) as { instruction?: string; anchor?: unknown };
      instruction = body.instruction ?? "";
      anchor = body.anchor;
    }
    // The real server refuses a blank instruction BEFORE the turn row exists
    // (the shared TurnSpec validator rejects an empty chat turn), so mock mode
    // must too — otherwise an attachment-only send looks supported here and
    // 400s in production.
    if (instruction.trim() === "") {
      return HttpResponse.json(
        { code: "invalid_request", message: "instruction is required" },
        { status: 400 },
      );
    }
    turnCounter += 1;
    const turnId = `mock-turn-${instanceId}-${turnCounter}`;
    turnInstruction.set(turnId, instruction);
    // Record it the way the journal would, so a reload shows the chips again.
    appendToJournal(params.conversationId as string, {
      role: "user",
      content: instruction,
      ...(attachments.length > 0 ? { attachments } : {}),
      ...(anchor ? { anchor } : {}),
    });
    return HttpResponse.json({ turnId }, { status: 202 });
  }),

  http.get("*/api/v1/projects/:projectName/agents/:conversationId/messages", ({ params }) => {
    const scenario = chatScenario();
    if (scenario === "multiuser") {
      return HttpResponse.json({ status: "done", messages: multiuserHistory });
    }
    if (scenario === "teammate-turn") {
      return HttpResponse.json({ status: "done", messages: teammateTurnHistory });
    }
    // Echo this session's own sends, attachment names included — the journal's
    // job in production. Empty for a conversation nothing was sent to, which is
    // the pre-#428 behaviour unchanged.
    return HttpResponse.json({
      status: "done",
      messages: readJournal()[params.conversationId as string] ?? [],
    });
  }),

  http.get("*/api/v1/projects/:projectName/turns/active", () => {
    if (chatScenario() === "teammate-turn") {
      return HttpResponse.json(activeTeammateTurn());
    }
    return new HttpResponse(null, { status: 204 });
  }),

  http.get("*/api/v1/projects/:projectName/turns/:turnId/stream", ({ params }) => {
    const turnId = String(params.turnId);
    const instruction = turnInstruction.get(turnId) ?? "";
    const failing = instruction.includes("fail");
    // The /design run declares its plan (#576) — and owns its own failure
    // variant ("/design fail" dies mid-write, leaving the wreckage), so it is
    // checked before the generic failing stream.
    if (instruction.trim().startsWith("/design")) {
      return sse(designPlanFrames(turnId, failing));
    }
    if (failing) {
      return sse([
        { type: "text-delta", delta: "Let me try that…" },
        { type: "turn-failed", message: "Mock turn failure (instruction contained 'fail')." },
      ]);
    }
    const registerFrames = registerChatFrames(
      instruction,
      String(params.projectName),
      turnId,
    );
    if (registerFrames) {
      return sse(registerFrames);
    }
    // Grilling scenarios (ADR-0012 / #270) — keyed on the /start flow command
    // or a typed trigger, never on a mere mention of "grill" in an edit
    // instruction (the retired GRILLING_DIRECTIVE died with #373).
    // An answer turn (instruction begins with the shared answer prefix) falls
    // through to the normal generation stream below.
    const isAnswer =
      instruction.startsWith(ANSWER_PREFIX) || instruction.startsWith(ANSWERS_PREFIX);
    if (!isAnswer) {
      const grillSingle = instruction.trim().startsWith("/start") || /\bgrill me\b/i.test(instruction);
      const grillBatch = /\ball at once\b|\bask me everything\b/i.test(instruction);
      if (grillBatch) {
        // A full interview — long enough to exercise the form's scrolling.
        const input = {
          questions: [
            {
              question: "Who is the primary user of this app?",
              detail:
                "This shapes onboarding, permissions, and how much admin tooling the spec needs — it's the single biggest scope decision, so I'm asking it first.",
              options: [
                {
                  label: "Individual consumers",
                  description:
                    "People sign themselves up and get a personal workspace. Onboarding stays lightweight (email or social login, no org setup), but every account manages itself — there is no central admin who can provision or remove users.",
                  recommended: true,
                },
                {
                  label: "Internal teams",
                  description:
                    "An organization rolls the app out to its staff. That means SSO, org-managed access, and an admin who controls membership — more setup work up front, but access control and offboarding come for free.",
                },
                {
                  label: "Both from day one",
                  description:
                    "Two onboarding paths, two permission models, and pricing that has to serve both. Roughly doubles the v1 scope; usually only worth it when both audiences are already committed.",
                },
                {
                  label: "Something else",
                  description: "Describe a different primary user in the text field below.",
                  freeText: true,
                },
              ],
            },
            {
              question: "Which platform matters most first?",
              detail:
                "The first platform decides the UI stack and review/release process; the others can follow later without rework if we pick one now.",
              options: [
                {
                  label: "Web",
                  description:
                    "Ships fastest: one responsive app, instant updates, no store review. Works on phones through the browser, though without push notifications or offline polish.",
                  recommended: true,
                },
                {
                  label: "Mobile",
                  description:
                    "Native iOS/Android first. Best for push notifications, camera/location use, and on-the-go sessions — but store review slows iteration and it needs its own release pipeline.",
                },
                {
                  label: "Both",
                  description:
                    "Web and mobile in parallel. Every feature is designed, built, and tested twice, so v1 takes noticeably longer — choose this only if mobile-only users are core to launch.",
                },
              ],
            },
            {
              question: "Which capabilities are in scope for v1?",
              detail:
                "Everything selected here becomes a section of the requirements spec; anything unselected is explicitly deferred so the first release stays small.",
              multiSelect: true,
              options: [
                {
                  label: "Accounts",
                  description:
                    "User registration, profiles, and session handling. Almost every other capability builds on this, so leaving it out only makes sense for a fully anonymous tool.",
                },
                {
                  label: "Payments",
                  description:
                    "Checkout, receipts, and a payment provider integration (e.g. Stripe). Brings compliance and refund flows with it — the most expensive item on this list.",
                },
                {
                  label: "Notifications",
                  description:
                    "Email or in-app alerts when something needs the user's attention. Cheap to add once accounts exist; pointless before there are events worth notifying about.",
                },
                {
                  label: "Search",
                  description:
                    "Full-text search across the app's content. Valuable once data volume grows, but v1 can usually ship with simple filtering instead.",
                },
              ],
            },
            {
              question: "How should users sign in?",
              detail:
                "Sign-in method affects both the signup conversion rate and how much credential handling the backend has to own.",
              options: [
                {
                  label: "Email + password",
                  description:
                    "Works for everyone with no third-party dependency, but we own password reset, breach protection, and rate limiting ourselves.",
                },
                {
                  label: "Social login",
                  description:
                    "Sign in with Google/GitHub — no passwords to store and the fastest signup flow. Users without those accounts are locked out unless we add email later.",
                  recommended: true,
                },
                {
                  label: "SSO only",
                  description:
                    "Enterprise identity providers (SAML/OIDC) only. Right when an organization mandates it; wrong for self-serve consumers, who can't sign up at all.",
                },
              ],
            },
            {
              question: "What is the pricing model?",
              detail:
                "Pricing decides whether billing infrastructure is in the v1 spec at all, and how accounts and limits are modeled.",
              options: [
                {
                  label: "Free",
                  description:
                    "No billing code in v1 — the whole payments surface disappears from scope. Monetization can be layered on later once usage proves out.",
                  recommended: true,
                },
                {
                  label: "Subscription",
                  description:
                    "Recurring plans with trials, upgrades, and dunning. Predictable revenue, but it drags billing, plan gating, and invoicing into the first release.",
                },
                {
                  label: "One-off purchase",
                  description:
                    "Pay once per item or unlock. Simpler than subscriptions, but still needs checkout, receipts, and refund handling in v1.",
                },
              ],
            },
            {
              question: "Where does the data live?",
              detail:
                "The storage choice fixes the backup, migration, and multi-tenancy story — it's hard to reverse once real data exists.",
              options: [
                {
                  label: "Managed Postgres",
                  description:
                    "One relational database run by a cloud provider. Boring, well-understood, easy to hire for, and scales past v1 without a rewrite.",
                  recommended: true,
                },
                {
                  label: "SQLite per tenant",
                  description:
                    "Each customer gets an isolated file database. Great isolation and trivially cheap at small scale, but cross-tenant reporting and migrations get harder.",
                },
                {
                  label: "Third-party BaaS",
                  description:
                    "Firebase/Supabase-style hosted backend. Fastest to a demo, but data model and auth become coupled to the vendor — migrating away later is real work.",
                },
              ],
            },
            {
              question: "Which integrations matter?",
              detail:
                "Each integration adds an external dependency to the spec — credentials, webhooks, and failure handling — so only pick the ones launch actually needs.",
              multiSelect: true,
              options: [
                {
                  label: "Slack",
                  description: "Post updates into channels. Needs a Slack app + OAuth install flow.",
                },
                {
                  label: "Email",
                  description: "Transactional mail (invites, digests) through a provider like SES or Resend.",
                },
                {
                  label: "Calendar",
                  description: "Create/read Google or Outlook events; the heaviest of the three to get right.",
                },
                {
                  label: "None yet",
                  description:
                    "Ship self-contained and add integrations when users ask — keeps v1 free of external moving parts.",
                  recommended: true,
                },
              ],
            },
            {
              question: "What is the launch timeline?",
              detail:
                "The timeline calibrates how aggressively the spec cuts scope — a shorter runway means fewer capabilities make the v1 list.",
              options: [
                {
                  label: "2 weeks",
                  description: "A demo-quality slice: one happy path, mocked edges, no polish. Good for validating interest only.",
                },
                {
                  label: "1 month",
                  description:
                    "A real but minimal product: the core flow production-ready, secondary features stubbed or deferred.",
                  recommended: true,
                },
                {
                  label: "A quarter",
                  description:
                    "Room for the full selected scope plus polish — at the cost of three months before any user feedback arrives.",
                },
              ],
            },
            {
              question: "How important is offline support?",
              detail:
                "Offline changes the data architecture fundamentally (local store + sync + conflict resolution), so it must be decided before the design phase, not after.",
              options: [
                {
                  label: "Not needed",
                  description: "The app assumes a connection; a dropped network just shows a retry state. Simplest by far.",
                  recommended: true,
                },
                {
                  label: "Nice to have",
                  description:
                    "Read-only caching of recently viewed data — useful on flaky connections without full sync complexity.",
                },
                {
                  label: "Critical",
                  description:
                    "Full offline editing with background sync and conflict resolution. Roughly doubles the data-layer work; only pick if users routinely work disconnected.",
                },
              ],
            },
            {
              question: "What should the app be called?",
              detail:
                "The name lands in the spec title, the repo, and the UI shell — there are no sensible presets, so type whatever you have in mind.",
              options: [],
            },
            {
              question: "Who administers the workspace?",
              detail:
                "The admin model decides whether v1 needs a roles/permissions system or can treat every user the same.",
              options: [
                {
                  label: "A single owner",
                  description:
                    "The creator holds all admin rights — invite, remove, configure. No roles UI needed in v1.",
                  recommended: true,
                },
                {
                  label: "Multiple admins",
                  description:
                    "Owners can grant admin to others, which means a roles model, permission checks, and an audit story.",
                },
                {
                  label: "No admin concept",
                  description:
                    "Every member is equal. Fine for small trusted groups; risky once anyone can delete shared data.",
                },
              ],
            },
          ],
        };
        // Stream the batch as chunked tool-input deltas (matching the real
        // provider) so mock mode exercises the progressive question rendering
        // (#270 latency): questions appear one by one, ~250ms per chunk.
        const inputJson = JSON.stringify(input);
        const CHUNK = 180;
        const deltas = [];
        for (let i = 0; i < inputJson.length; i += CHUNK) {
          deltas.push({ type: "tool-input-delta", id: `qs-${turnId}`, delta: inputJson.slice(i, i + CHUNK) });
        }
        return sse([
          { type: "text-delta", delta: "Let me pin the idea down — a few questions:" },
          { type: "tool-input-start", id: `qs-${turnId}`, toolName: "ask_questions" },
          ...deltas,
          { type: "tool-input-end", id: `qs-${turnId}` },
          { type: "tool-call", toolCallId: `qs-${turnId}`, toolName: "ask_questions", input },
          {
            type: "tool-result",
            toolName: "ask_questions",
            toolCallId: `qs-${turnId}`,
            input,
            output: { status: "awaiting_user_response" },
          },
          { type: "turn-committed", noChanges: true },
        ]);
      }
      if (grillSingle) {
        const input = {
          question: "Who is the primary user of this app?",
          detail:
            "This shapes onboarding, permissions, and how much admin tooling the spec needs — it's the single biggest scope decision, so I'm asking it first.",
          options: [
            {
              label: "Individual consumers",
              description:
                "People sign themselves up and get a personal workspace. Onboarding stays lightweight (email or social login, no org setup), but every account manages itself — there is no central admin who can provision or remove users.",
              recommended: true,
            },
            {
              label: "Internal teams",
              description:
                "An organization rolls the app out to its staff. That means SSO, org-managed access, and an admin who controls membership — more setup work up front, but access control and offboarding come for free.",
            },
            {
              label: "Both from day one",
              description:
                "Two onboarding paths, two permission models, and pricing that has to serve both. Roughly doubles the v1 scope; usually only worth it when both audiences are already committed.",
            },
          ],
          multiSelect: false,
        };
        return sse([
          { type: "text-delta", delta: "Before I write anything, let me pin the idea down." },
          { type: "tool-call", toolCallId: `q-${turnId}`, toolName: "ask_question", input },
          {
            type: "tool-result",
            toolName: "ask_question",
            toolCallId: `q-${turnId}`,
            input,
            output: { status: "awaiting_user_response", question: input.question },
          },
          { type: "turn-committed", noChanges: true },
        ]);
      }
    }
    return sse([
      { type: "text-delta", delta: "Joining the spec workspace… " },
      { type: "text-delta", delta: "I'll create the requirements now." },
      // Streamed tool input: the panel shows "Creating prd.md" as soon
      // as the path resolves, then flips to "Created" on the tool-result.
      { type: "tool-input-start", id: "tc-1", toolName: "addFile" },
      {
        type: "tool-input-delta",
        id: "tc-1",
        delta: '{"path":"specs/requirements/prd.md","content":"# Requirements',
      },
      { type: "tool-input-delta", id: "tc-1", delta: '\\n\\n## Overview\\nA simple todo app.' },
      { type: "tool-input-end", id: "tc-1" },
      {
        type: "tool-result",
        toolName: "addFile",
        toolCallId: "tc-1",
        input: { path: "specs/requirements/prd.md" },
        output: { ok: true, op: "add", path: "specs/requirements/prd.md", status: "applied" },
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

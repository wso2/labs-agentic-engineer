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

/**
 * A turn start's body: JSON (`TurnInputBody`) or, when the message
 * carries chat attachments, multipart (`TurnInputMultipart`): `instruction`,
 * `scope?` (JSON), `anchor?` (JSON), `intent?`, `files[]`; the JSON form also
 * takes `prototypeFeedback?`. The caps are aep-api's
 * (`A/spec/genaiturns/attachments.go`): at most 10 files, 5 MiB each, 15 MiB
 * in total, and the same accepted extensions and the media type the model
 * reads each as.
 *
 * The retired `target` is refused in both forms, so a stale client learns to
 * send `scope` instead of running a turn that ignores what it asked for. A
 * review batch is checked here for what the body alone can say (its shape,
 * its `/prototype` instruction, no aim); that it rides a Room turn is the
 * start path's check (`turns/start-turn.ts`).
 *
 * Nothing is buffered past a cap: the body is counted as it streams and
 * the read stops at the cap with 413, so a multipart body holds at most
 * `MAX_MULTIPART_BODY_BYTES` in memory before it is parsed; the per-file cap
 * is then checked on each part. Every refusal is an `InputError`.
 */

import type { IncomingMessage } from "node:http";
import { basename, extname } from "node:path";
import {
  isPrototypeFeedback,
  isTurnAim,
  isTurnScope,
  type PrototypeFeedback,
  type TurnAim,
  type TurnAttachment,
  type TurnScope,
} from "@aep/agent-stream";
import { parseFeedbackSubmission } from "@wso2/prototype-kit/feedback";
import { MAX_INSTRUCTION_BYTES, type TurnInput } from "../turns/start-turn.js";

export const MAX_ATTACHMENT_COUNT = 10;
export const MAX_ATTACHMENT_BYTES = 5 << 20;
export const MAX_ATTACHMENTS_TOTAL_BYTES = 15 << 20;
/** A JSON body carries no file content: the instruction plus a small anchor. */
const MAX_JSON_BODY_BYTES = 256 << 10;
/** The files' total plus room for the text fields and the multipart framing. */
const MAX_MULTIPART_BODY_BYTES = MAX_ATTACHMENTS_TOTAL_BYTES + (1 << 20);

/**
 * Accepted extensions → the media type the MODEL reads the file as. Every
 * text format is `text/plain`: the providers map PDF, plain text and four
 * image types, nothing else.
 */
const ATTACHMENT_MEDIA_TYPES: Record<string, string> = {
  ".pdf": "application/pdf",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".md": "text/plain",
  ".txt": "text/plain",
  ".csv": "text/plain",
  ".tsv": "text/plain",
  ".json": "text/plain",
  ".yaml": "text/plain",
  ".yml": "text/plain",
  ".xml": "text/plain",
  ".html": "text/plain",
  ".rst": "text/plain",
};

/** A body the turn cannot start from. */
export class InputError extends Error {
  constructor(
    readonly status: 400 | 413,
    readonly code: "invalid_turn" | "attachment_rejected" | "payload_too_large",
    message: string,
  ) {
    super(message);
    this.name = "InputError";
  }
}

const tooLarge = (what: string) => new InputError(413, "payload_too_large", `${what} exceeds the size limit`);
const invalid = (message: string) => new InputError(400, "invalid_turn", message);
const rejected = (message: string) => new InputError(400, "attachment_rejected", message);

/** The JSON body's fields; anything else is refused by name. */
const JSON_FIELDS = ["instruction", "scope", "anchor", "intent", "prototypeFeedback"];
const TARGET_RETIRED = "target is no longer accepted — send scope";
/** The one command a review batch may ride. */
const PROTOTYPE_COMMAND = "/prototype";

/** Read and validate the turn body by its content type. */
export async function readTurnInput(req: IncomingMessage): Promise<TurnInput> {
  const type = (req.headers["content-type"] ?? "").split(";")[0]!.trim().toLowerCase();
  if (type === "application/json") return jsonInput(await readCapped(req, MAX_JSON_BODY_BYTES, "the request body"));
  if (type === "multipart/form-data") {
    return multipartInput(await readCapped(req, MAX_MULTIPART_BODY_BYTES, "the attachments"), req.headers["content-type"]!);
  }
  throw invalid("the body must be application/json or multipart/form-data");
}

/**
 * The whole body, counted as it streams: the read stops at `cap` (the rest
 * is drained, never kept) and fails with 413.
 */
function readCapped(req: IncomingMessage, cap: number, what: string): Promise<Buffer> {
  const declared = Number(req.headers["content-length"]);
  return new Promise((resolve, reject) => {
    if (Number.isFinite(declared) && declared > cap) {
      req.resume();
      reject(tooLarge(what));
      return;
    }
    const chunks: Buffer[] = [];
    let size = 0;
    let over = false;
    req.on("data", (chunk: Buffer) => {
      if (over) return;
      size += chunk.length;
      if (size > cap) {
        over = true;
        chunks.length = 0;
        reject(tooLarge(what));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => {
      if (!over) resolve(Buffer.concat(chunks));
    });
    req.on("error", reject);
  });
}

function jsonInput(raw: Buffer): TurnInput {
  let body: unknown;
  try {
    body = JSON.parse(raw.toString("utf8"));
  } catch {
    throw invalid("the body is not valid JSON");
  }
  if (typeof body !== "object" || body === null || Array.isArray(body)) throw invalid("the body must be an object");
  const b = body as Record<string, unknown>;
  if (b.target !== undefined) throw invalid(TARGET_RETIRED);
  const unknown = Object.keys(b).filter((k) => !JSON_FIELDS.includes(k));
  if (unknown.length > 0) throw invalid(`unknown field: ${unknown[0]}`);
  if (typeof b.instruction !== "string") throw invalid("instruction must be a string");
  const aim = aimOf(b.anchor, b.intent);
  return {
    instruction: b.instruction,
    ...scopeOf(b.scope),
    ...aim,
    ...prototypeFeedbackOf(b.instruction, aim.aim !== undefined, b.prototypeFeedback),
    attachments: [],
  };
}

/** The scope (S6): absent, or what `isTurnScope` accepts. */
function scopeOf(scope: unknown): { scope?: TurnScope } {
  if (scope === undefined) return {};
  if (!isTurnScope(scope)) throw invalid('scope must be { kind: "feature", feature: "F<n>" } or { kind: "design-review" }');
  return { scope: scope.kind === "feature" ? { kind: "feature", feature: scope.feature } : { kind: "design-review" } };
}

/**
 * A review batch (#860), checked as aep-api's `prototype_feedback.go` did:
 * the instruction is `/prototype` alone or followed by the batch's own
 * component, no aim rides with it (an aim points at a document selection,
 * the batch at prototype ids), and the batch passes the kit's rules plus the
 * component (`isPrototypeFeedback`). The batch is rebuilt from the parsed
 * fields, so nothing but the contract's fields is kept.
 */
export function prototypeFeedbackOf(instruction: string, aimed: boolean, raw: unknown): { prototypeFeedback?: PrototypeFeedback } {
  if (raw === undefined) return {};
  const words = instruction.trim().split(/\s+/);
  const component = typeof raw === "object" && raw !== null ? (raw as Record<string, unknown>).component : undefined;
  if (words[0] !== PROTOTYPE_COMMAND || words.length > 2 || (words.length === 2 && words[1] !== component)) {
    throw invalid("prototypeFeedback is only valid on a /prototype instruction, bare or followed by its component");
  }
  if (aimed) throw invalid("prototypeFeedback cannot be combined with anchor and intent");
  const parsed = parseFeedbackSubmission(raw);
  if (!isPrototypeFeedback(raw) || !("submission" in parsed)) {
    const reason = "reason" in parsed ? parsed.reason : "component must name a web-application component";
    throw invalid(`prototypeFeedback: ${reason}`);
  }
  return { prototypeFeedback: { prototypeHash: parsed.submission.prototypeHash, component: raw.component, requests: parsed.submission.requests } };
}

/**
 * The aim (#666): both `anchor` and `intent`, or neither; checked by the
 * shared rule (`isTurnAim`), so the two body forms accept the same aims.
 */
function aimOf(anchor: unknown, intent: unknown): { aim?: TurnAim } {
  const hasAnchor = anchor !== undefined && anchor !== null;
  const hasIntent = intent !== undefined && intent !== null && intent !== "";
  if (!hasAnchor && !hasIntent) return {};
  if (!hasAnchor || !hasIntent) throw invalid("anchor and intent must be sent together");
  const aim = { anchor, intent };
  if (!isTurnAim(aim)) throw invalid("anchor must be { file, nodes: [{ name, kind, context? }] } and intent change or discuss");
  return { aim: { anchor: aim.anchor, intent: aim.intent } };
}

async function multipartInput(raw: Buffer, contentType: string): Promise<TurnInput> {
  let form: FormData;
  try {
    form = await new Response(raw, { headers: { "content-type": contentType } }).formData();
  } catch {
    throw invalid("the multipart body cannot be decoded");
  }
  const text = (field: string): string | undefined => {
    const v = form.get(field);
    if (v === null) return undefined;
    if (typeof v !== "string") throw invalid(`${field} must be a text field`);
    if (Buffer.byteLength(v) > MAX_INSTRUCTION_BYTES) throw tooLarge(field);
    return v;
  };
  const instruction = text("instruction");
  if (instruction === undefined) throw invalid("instruction is required");
  // Named refusals for the two parts the JSON form knows and this form does
  // not; other parts are ignored, as aep-api did: a console that ships a
  // field first must not have every send refused during a roll.
  if (form.has("target")) throw invalid(TARGET_RETIRED);
  if (form.has("prototypeFeedback")) throw invalid("prototypeFeedback is JSON only: send the batch without attachments");
  const anchor = jsonPart(text("anchor"), "anchor must be valid JSON");
  const scope = jsonPart(text("scope"), "scope must be valid JSON: {kind, feature?}");
  return {
    instruction,
    ...scopeOf(scope),
    ...aimOf(anchor, text("intent")),
    attachments: await attachmentsOf(form.getAll("files")),
  };
}

/** A multipart part the contract declares `application/json`; blank is absent. */
function jsonPart(raw: string | undefined, refusal: string): unknown {
  if (raw === undefined || raw.trim() === "") return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    throw invalid(refusal);
  }
}

async function attachmentsOf(parts: ReturnType<FormData["getAll"]>): Promise<TurnAttachment[]> {
  if (parts.length > MAX_ATTACHMENT_COUNT) throw rejected(`at most ${MAX_ATTACHMENT_COUNT} attachments per message`);
  const seen = new Set<string>();
  const out: TurnAttachment[] = [];
  let total = 0;
  for (const part of parts) {
    if (typeof part === "string") throw rejected("files must be file parts");
    const name = basename(part.name.trim().replaceAll("\\", "/"));
    if (name === "" || name === "." || name === "..") throw rejected("an attachment has no usable file name");
    const mediaType = ATTACHMENT_MEDIA_TYPES[extname(name).toLowerCase()];
    if (!mediaType) throw rejected(`${JSON.stringify(name)}: unsupported attachment type`);
    // The agent dedupes attachments by name, so a second one would vanish.
    if (seen.has(name)) throw rejected(`${JSON.stringify(name)} is attached twice; rename one`);
    seen.add(name);
    if (part.size > MAX_ATTACHMENT_BYTES) throw tooLarge(`${JSON.stringify(name)}`);
    total += part.size;
    if (total > MAX_ATTACHMENTS_TOTAL_BYTES) throw tooLarge("the attachments");
    out.push({ name, mediaType, data: Buffer.from(await part.arrayBuffer()).toString("base64") });
  }
  return out;
}

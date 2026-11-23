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

import { ADDRESS_COMMENTS_COMMAND, parseDesignCommand } from "@aep/contracts/commands";
import type * as Y from "yjs";
import { readDocFile } from "@aep/collab-doc";
import { DESIGN_REVIEW_PATH, type ReviewComment } from "@aep/agent-stream";
import type { MockSpecModel } from "./spec";
import { DECISIONS_PATH } from "../../features/design/model/decisions";
import { readReview, reviewText, splitQuote } from "../../features/design/model/review";
import { manifestPath, sourcePath } from "../../features/prototype/model/prototypes";
import { projectSpecDoc } from "../../features/spec/collab/specDoc";
import { readSpecLines } from "../../features/spec/collab/useSpecLines";
import { designBasis, designWork } from "../../features/spec/model/designWork";
import { productWideItems, readRequirements } from "../../features/spec/model/requirements";
import type { components } from "../../generated/ae-design-agent";
import type { ScriptFrame } from "../chatServer";
import { liveDesign, type DesignEffect } from "../designState";
import { dependencyOf, designCatalog, replyTo } from "./design";
import { designRoomFiles, reviseDecision } from "./designFiles";
import { Script } from "./interview";
import { SAMPLE_COMPONENT, tweakSource } from "./prototype";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// PROVISIONAL — mock-only until E1/E4; see features/design/api/designModel.ts.
//
// The mock agent's design-review turns, scoped to the design review:
//  - "/design F1 F2": designs every interviewed, unblocked feature that is new
//    or out of date, worked out from the spec as the user has it now (the
//    local doc stands in for the room), finds what a feature needs from
//    outside, and writes the design's files into the room one by one, as the
//    design agent does (fixtures/designFiles.ts);
//  - "/address-comments": answers every comment the console sent in
//    specs/design/review.json, marking each addressed with a one-line reply,
//    and revises the prototype where a comment on it asks for a change the
//    sample knows (fixtures/prototype.ts), and rewrites a technical decision a
//    Change… comment quotes in the comment's words, still assumed for the
//    user to keep (fixtures/designFiles.ts).
//    A comment that offers two readings
//    ("… or …") and has no answer yet is not guessed: the mock writes its
//    question on it and leaves it sent; once answered, it is addressed with
//    the answer. Each run closes with what is next on the Design card.
// What each does lands when the turn has finished (designState.ts).

const DESIGN_NOTE: Record<string, string> = {
  "Submit expenses": "the claim flow, 1 endpoint, 2 screens",
  Approvals: "the approval flow, 3 endpoints, 3 screens",
};

export interface DesignTurn {
  display: string;
  frames: ScriptFrame[];
  reply: ConversationMessage[];
  design?: DesignEffect;
}

function names(model: MockSpecModel, ids: string[]): string {
  const list = ids.map((id) => model.features.find((f) => f.id === id)?.name ?? id);
  return list.length <= 1 ? (list[0] ?? "") : `${list.slice(0, -1).join(", ")} and ${list.at(-1)}`;
}

/** Files the design never writes over once they exist: the user's review, and a prototype comments may have revised. */
const KEPT = new Set([DESIGN_REVIEW_PATH, manifestPath(SAMPLE_COMPONENT), sourcePath(SAMPLE_COMPONENT)]);

/** Stream each file into the room: added when new, rewritten whole when it changed; what it leaves, by path. */
function writeFiles(s: Script, doc: Y.Doc, turnKey: string, files: Record<string, string>): Record<string, string> {
  const written: Record<string, string> = {};
  for (const [i, [path, content]] of Object.entries(files).entries()) {
    const current = readDocFile(doc, path);
    if (current === content || (current !== undefined && KEPT.has(path))) continue;
    if (current === undefined) s.pause(150).add(`${turnKey}-f${i}`, path, content);
    else s.pause(150).edit(`${turnKey}-f${i}`, path, current, content);
    written[path] = content;
  }
  return written;
}

function scriptDesign(projectName: string, text: string, model: MockSpecModel, turnKey: string): DesignTurn {
  const design = liveDesign(projectName);
  const doc = projectSpecDoc(projectName, model);
  const lines = readSpecLines(doc);
  const productWide = productWideItems(readRequirements(lines));
  const { toDesign, outOfDate } = designWork(model.features, design.designedFrom, lines, productWide);
  if (toDesign.length === 0) {
    const s = new Script().pause(500).say("The design is up to date with the spec. Nothing to design.");
    return { display: text, ...s.end() };
  }
  const designedFrom = Object.fromEntries(
    toDesign.map((id) => [id, designBasis(model.features.find((f) => f.id === id)!, lines, productWide)]),
  );
  const designed = model.features.filter((f) => design.designedFrom[f.id] !== undefined || toDesign.includes(f.id));
  const drafts = designCatalog(projectName, designed, lines);
  const found = model.features
    .filter((f) => f.stage !== "Not interviewed")
    .map((f) => dependencyOf(projectName, f))
    .filter((d) => d !== null)
    .filter((d) => !design.dependencies.some((known) => known.featureId === d.featureId));
  const updating = toDesign.every((id) => outOfDate.includes(id));

  const s = new Script()
    .pause(600)
    .say(`${updating ? "Updating the design for" : "Designing"} ${names(model, toDesign)}. Lines still marked assumed are designed as written.`);
  for (const id of toDesign) {
    const name = names(model, [id]);
    s.pause(700).say(`Designed ${name}: ${DESIGN_NOTE[name] ?? "flows, contracts and acceptance"}.`);
  }
  const files = writeFiles(s, doc, turnKey, designRoomFiles(projectName, drafts, [...design.dependencies, ...found]));
  const blocker = found[0];
  s.pause(600).say(
    `The design is ready: ${plural(Object.keys(files).length, "file")} written. Next on the Design card: check each file, and comment from its 💬.` +
      (blocker ? ` ${names(model, [blocker.featureId])} waits on ${blocker.needs}; it blocks nothing else.` : ""),
  );
  return {
    display: text,
    ...s.end(),
    design: { kind: "design", features: toDesign, designedFrom, drafts, dependencies: found, files },
  };
}

/** The revision an Address comments turn makes: one past the latest any comment names. */
function nextRevision(comments: readonly ReviewComment[]): number {
  return Math.max(1, ...comments.map((c) => c.revision ?? 1)) + 1;
}

/** A comment the mock treats as ambiguous: its own words, not a line it quotes, offer two readings. */
const TWO_READINGS = /\bor\b/i;

function questionFor(comment: ReviewComment): string {
  return `“${comment.text}” can be read two ways. Which one should the design follow?`;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

function scriptAddressComments(projectName: string, text: string, model: MockSpecModel, turnKey: string): DesignTurn {
  const doc = projectSpecDoc(projectName, model);
  const before = readDocFile(doc, DESIGN_REVIEW_PATH);
  const { review, problem } = readReview(before);
  const all = review.artifacts.flatMap((a) => (a.comments ?? []).map((c) => ({ path: a.path, comment: c })));
  const sent = all.filter((c) => c.comment.status === "sent");
  if (problem || before === undefined || sent.length === 0) {
    const why = problem ? `I can't read specs/design/review.json: ${problem}` : "There are no comments waiting for me in the review.";
    return { display: text, ...new Script().pause(500).say(why).end() };
  }
  const s = new Script().pause(600).say(`Working through ${sent.length} comment${sent.length === 1 ? "" : "s"} together.`);
  const revision = nextRevision(all.map((c) => c.comment));
  const at = new Date().toISOString();
  const files: Record<string, string> = {};
  // A comment on the prototype the sample knows how to take changes its source.
  let source = readDocFile(doc, sourcePath(SAMPLE_COMPONENT));
  let decisions = readDocFile(doc, DECISIONS_PATH);
  const replies = new Map<string, string>();
  const questions = new Map<string, string>();
  for (const { path, comment } of sent) {
    if (comment.answer === undefined && TWO_READINGS.test(splitQuote(comment.text).words)) {
      questions.set(comment.id, questionFor(comment));
      continue;
    }
    if (comment.answer !== undefined) {
      replies.set(comment.id, `Went with your answer: ${comment.answer}`);
      continue;
    }
    const tweak = path === manifestPath(SAMPLE_COMPONENT) && source !== undefined ? tweakSource(source, comment.text) : null;
    if (tweak && source !== undefined) {
      s.pause(400).edit(`${turnKey}-${comment.id}`, sourcePath(SAMPLE_COMPONENT), tweak.from, tweak.to);
      source = source.replace(tweak.from, tweak.to);
      files[sourcePath(SAMPLE_COMPONENT)] = source;
    }
    const revised = path === DECISIONS_PATH && decisions !== undefined ? reviseDecision(decisions, comment.text) : null;
    if (revised && decisions !== undefined) {
      s.pause(400).edit(`${turnKey}-${comment.id}`, DECISIONS_PATH, revised.from, revised.to);
      decisions = decisions.replace(revised.from, revised.to);
      files[DECISIONS_PATH] = decisions;
    }
    replies.set(comment.id, tweak?.applied ?? (revised ? `Changed the decision: ${revised.words}` : replyTo(projectName, comment.text)));
  }
  const answered = {
    version: 1 as const,
    artifacts: review.artifacts.map((a) => ({
      ...a,
      ...(a.comments
        ? {
            comments: a.comments.map((c) => {
              const question = questions.get(c.id);
              if (question) return { ...c, question };
              const reply = replies.get(c.id);
              return reply ? { ...c, status: "addressed" as const, reply, revision, addressedAt: at } : c;
            }),
          }
        : {}),
    })),
  };
  const after = reviewText(answered);
  s.pause(400).edit(`${turnKey}-review`, DESIGN_REVIEW_PATH, before, after);
  files[DESIGN_REVIEW_PATH] = after;
  const asked = questions.size;
  s.pause(400).say(
    (replies.size > 0 ? `Addressed ${plural(replies.size, "comment")}; each one now says what I did.` : "I addressed nothing yet.") +
      (asked > 0 ? ` ${plural(asked, "question")} for you on the Design card.` : "") +
      ` Next on the Design card: ${asked > 0 ? `answer ${asked === 1 ? "it" : "them"}, then ` : ""}check the changed files, or reopen a comment that isn't right yet.`,
  );
  return { display: text, ...s.end(), design: { kind: "address", files } };
}

/** A design-review turn, or null when the message is not one (the caller answers it). */
export function scriptDesignTurn(req: {
  projectName: string;
  instruction: string;
  model: MockSpecModel;
  turnKey: string;
}): DesignTurn | null {
  const text = req.instruction.trim();
  if (parseDesignCommand(text)) return scriptDesign(req.projectName, text, req.model, req.turnKey);
  if (text === ADDRESS_COMMENTS_COMMAND) return scriptAddressComments(req.projectName, text, req.model, req.turnKey);
  return null;
}

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

import { parseDesignCommand } from "@aep/contracts/commands";
import * as Y from "yjs";
import { readDocFile } from "@aep/collab-doc";
import type { MockSpecModel } from "./spec";
import { applyAgentToolCall } from "../../features/spec/collab/agentWrites";
import { projectSpecDoc } from "../../features/spec/collab/specDoc";
import { readSpecLines } from "../../features/spec/collab/useSpecLines";
import { designBasis, designWork } from "../../features/spec/model/designWork";
import { productWideItems, readRequirements } from "../../features/spec/model/requirements";
import type { components } from "../../generated/aep-api";
import type { ScriptFrame } from "../chatServer";
import { liveDesign, teachingSent, type DesignEffect } from "../designState";
import { dependencyOf, designCatalog, feedbackFor, type DesignTweak } from "./design";
import { Script } from "./interview";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// PROVISIONAL — mock-only until E1/E4/E5; see features/design/api/designModel.ts.
//
// The mock agent's design-review turns, scoped to the design review:
//  - "Design 2 features" / "Update design · 1 feature": designs every
//    interviewed, unblocked feature that is new or out of date, worked out
//    from the spec as the user has it now (the local doc stands in for the
//    room), finds what a feature needs from outside, and the first time
//    teaches the user how feedback works;
//  - "Address comments · 3": works through every open comment in one turn,
//    changes the design, rewrites a spec line when a comment is really a
//    requirement (an editFile the client applies to its doc, as an
//    interview's write), and replies on each comment.
// What each does lands when the turn has finished (designState.ts).

const ADDRESS = /^address comments\b/i;

const DESIGN_NOTE: Record<string, string> = {
  "Submit expenses": "the claim flow, 1 endpoint, 2 screens",
  Approvals: "the approval flow, 3 endpoints, 3 screens",
};

export const TEACHING = [
  "How feedback works here:",
  "1. Click anything in an artifact to pin a comment. In the prototype and flows, switch to Comment first.",
  "2. Pin as many as you like, then press Address comments. I'll work through them together.",
  "3. Check each change I make, then resolve the comment, or reply if it isn't right yet.",
  "You can also just tell me here.",
].join("\n");

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

function scriptDesign(projectName: string, text: string, model: MockSpecModel): DesignTurn {
  const design = liveDesign(projectName);
  const lines = readSpecLines(projectSpecDoc(projectName, model));
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
  const drafts = designCatalog(projectName, designed, lines, new Set(design.tweaks));
  const found = model.features
    .filter((f) => f.stage !== "Not interviewed")
    .map((f) => dependencyOf(projectName, f))
    .filter((d) => d !== null)
    .filter((d) => !design.dependencies.some((known) => known.featureId === d.featureId));
  const teach = !design.taught && !teachingSent(projectName);
  const updating = toDesign.every((id) => outOfDate.includes(id));

  const s = new Script()
    .pause(600)
    .say(`${updating ? "Updating the design for" : "Designing"} ${names(model, toDesign)}. Lines still marked assumed are designed as written.`);
  for (const id of toDesign) {
    const name = names(model, [id]);
    s.pause(700).say(`Designed ${name}: ${DESIGN_NOTE[name] ?? "flows, contracts and acceptance"}.`);
  }
  const blocker = found[0];
  s.pause(600).say(
    `The design is ready: ${drafts.length} artifacts. Use the filter to see the business or the technical ones.` +
      (blocker
        ? ` One thing blocks a build: ${names(model, [blocker.featureId])} waits on ${blocker.needs}. It's at the top of the list, and it blocks nothing else.`
        : ""),
  );
  s.pause(300).say(teach ? TEACHING : "Comment on anything, or tell me here. I'd start with the prototype.");
  return {
    display: text,
    ...s.end(),
    design: { kind: "design", features: toDesign, designedFrom, drafts, dependencies: found, teach },
  };
}

function scriptAddress(projectName: string, text: string, model: MockSpecModel, turnKey: string): DesignTurn {
  const design = liveDesign(projectName);
  const open = design.comments.filter((c) => c.status === "open");
  if (open.length === 0) {
    return { display: text, ...new Script().pause(500).say("There are no open comments to address.").end() };
  }
  // The agent works on a copy of the room: its spec edits, and the spec each
  // changed feature is then designed from, are read off the copy.
  const room = projectSpecDoc(projectName, model);
  const scratch = new Y.Doc();
  Y.applyUpdate(scratch, Y.encodeStateAsUpdate(room));
  const productWide = productWideItems(readRequirements(readSpecLines(scratch)));
  const before = designWork(model.features, design.designedFrom, readSpecLines(scratch), productWide);

  const s = new Script().pause(600).say(`Working through ${open.length} comment${open.length === 1 ? "" : "s"} together.`);
  const tweaks = new Set<DesignTweak>(design.tweaks);
  const replies: Extract<DesignEffect, { kind: "address" }>["replies"] = [];
  const specFeatures = new Set<string>();
  let file: { path: string; content: string } | undefined;
  for (const comment of open) {
    const feedback = feedbackFor(projectName, comment.text);
    if (feedback.tweak) tweaks.add(feedback.tweak);
    let specLine: string | null = null;
    const feature = feedback.spec && model.features.find((f) => f.id === feedback.spec!.featureId);
    const current = feature ? readDocFile(scratch, feature.path) : undefined;
    if (feedback.spec && feature && current?.includes(feedback.spec.from)) {
      const edit = { path: feature.path, oldString: feedback.spec.from, newString: feedback.spec.to };
      s.edit(`${turnKey}-c${comment.n}`, edit.path, edit.oldString, edit.newString);
      applyAgentToolCall(scratch, { type: "tool-result", toolCallId: `${turnKey}-c${comment.n}`, toolName: "editFile", input: edit });
      file = { path: feature.path, content: readDocFile(scratch, feature.path) ?? "" };
      specLine = feedback.spec.lineId;
      specFeatures.add(feature.id);
    }
    replies.push({ commentId: comment.id, reply: feedback.reply, specLine, artifacts: [comment.artifactId, ...feedback.artifacts] });
  }
  const lines = readSpecLines(scratch);
  // A feature whose spec the turn changed is designed from the new words,
  // unless the user's own edit had already put it out of date.
  const designedFrom = Object.fromEntries(
    [...specFeatures]
      .filter((id) => !before.outOfDate.includes(id))
      .map((id) => [id, designBasis(model.features.find((f) => f.id === id)!, lines, productWide)]),
  );
  const designed = model.features.filter((f) => design.designedFrom[f.id] !== undefined);
  const drafts = designCatalog(projectName, designed, lines, tweaks);
  const changedSpec = replies.filter((r) => r.specLine).length;
  s.pause(800).say(addressedReply(open.length - changedSpec, changedSpec));
  return {
    display: text,
    ...s.end(),
    design: { kind: "address", replies, tweaks: [...tweaks], drafts, designedFrom, ...(file ? { file } : {}) },
  };
}

/**
 * The Address comments turn's closing line: how many comments changed the
 * design only and how many the spec too, with no clause for a count of zero,
 * worded for one comment or several.
 */
export function addressedReply(designOnly: number, specToo: number): string {
  const total = designOnly + specToo;
  const all = total === 1 ? "It" : total === 2 ? "Both" : `All ${total}`;
  const dot = " (you'll see a dot on the Spec tab)";
  const what =
    specToo === 0
      ? `${all} changed the design only.`
      : designOnly === 0
        ? `${all} changed the spec as well as the design${dot}.`
        : `${designOnly} changed the design only, and ${specToo} also changed the spec${dot}.`;
  const check =
    total === 1
      ? "The comment now shows what I did: check it, then resolve it, or reply if it's not right yet."
      : "Each comment now shows what I did: check it, then resolve it, or reply if it's not right yet.";
  return `Done. ${what} ${check}`;
}

/** A design-review turn, or null when the message is not one (the caller answers it). */
export function scriptDesignTurn(req: {
  projectName: string;
  instruction: string;
  model: MockSpecModel;
  turnKey: string;
}): DesignTurn | null {
  const text = req.instruction.trim();
  if (parseDesignCommand(text)) return scriptDesign(req.projectName, text, req.model);
  if (ADDRESS.test(text)) return scriptAddress(req.projectName, text, req.model, req.turnKey);
  return null;
}

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

import type {
  DesignArtifact,
  DesignComment,
  DesignDependency,
  DesignModel,
} from "../features/design/api/designModel";
import { addressComment } from "../features/design/model/comments";
import type { DesignSummary } from "../features/spec/api/specModel";
import { isRunning, projectTurns } from "./chatServer";
import type { ArtifactDraft, DesignTweak } from "./fixtures/design";

// PROVISIONAL — mock-only until E1/E4/E5; see features/design/api/designModel.ts.
//
// Each project's design review, as the mock server keeps it: the artifacts,
// the dependencies found, the comments, and which features were designed from
// what spec.
//
// A design turn is scripted when it starts (fixtures/designTurns.ts) and
// carries its effect; the effect lands once the turn has finished, the first
// time the design is read after that. Every turn the conversation holds lands,
// as an interview's does (specState.ts): the conversation outlives a reload
// (chatServer.ts), and the design must say what the chat says the agent did,
// or a reload would offer the same design again and run it twice. A design
// made from spec edits a reload dropped then reads as out of date, which it is.
// The user's own moves (a pinned comment, a resolve, a reply, a settled
// dependency) change the state at once and, like the spec's, last for the
// page's life.

/** What a design-review turn does to the design once it has finished. */
export type DesignEffect =
  | {
      kind: "design";
      features: string[];
      /** The spec each of them was designed from (spec/model/designWork.ts). */
      designedFrom: Record<string, string>;
      /** The whole design as it now reads. */
      drafts: ArtifactDraft[];
      dependencies: DesignDependency[];
      /** This turn taught the user how feedback works, in the chat. */
      teach: boolean;
    }
  | {
      kind: "address";
      replies: { commentId: string; reply: string; specLine: string | null; artifacts: string[] }[];
      tweaks: DesignTweak[];
      drafts: ArtifactDraft[];
      /** Features whose spec the turn changed along with their design: designed from the new words. */
      designedFrom: Record<string, string>;
      /** The spec file it rewrote, as the turn left it. */
      file?: { path: string; content: string };
    };

interface StoredComment extends DesignComment {
  /** The user has opened the Spec tab since this comment changed a spec line. */
  specSeen: boolean;
}

export interface StoredDesign {
  revision: number;
  designedFrom: Record<string, string>;
  artifacts: DesignArtifact[];
  dependencies: DesignDependency[];
  comments: StoredComment[];
  tweaks: DesignTweak[];
  taught: boolean;
  /** Turns whose effect has landed. */
  applied: string[];
}

const designs = new Map<string, StoredDesign>();

function empty(): StoredDesign {
  return { revision: 0, designedFrom: {}, artifacts: [], dependencies: [], comments: [], tweaks: [], taught: false, applied: [] };
}

/** The drafts as artifacts of this revision: new ones added now, the rest changed now when the turn touched them. */
function stamp(
  previous: DesignArtifact[],
  drafts: ArtifactDraft[],
  revision: number,
  touched: (draft: ArtifactDraft, before: DesignArtifact) => boolean,
): DesignArtifact[] {
  return drafts.map((draft) => {
    const before = previous.find((a) => a.id === draft.id);
    if (!before) return { ...draft, addedIn: revision, changedIn: revision };
    const changed = touched(draft, before) || JSON.stringify(before.source) !== JSON.stringify(draft.source);
    return { ...draft, addedIn: before.addedIn, changedIn: changed ? revision : before.changedIn };
  });
}

function land(design: StoredDesign, effect: DesignEffect): void {
  const revision = design.revision + 1;
  design.revision = revision;
  design.designedFrom = { ...design.designedFrom, ...effect.designedFrom };
  if (effect.kind === "design") {
    design.artifacts = stamp(design.artifacts, effect.drafts, revision, (draft) =>
      draft.features.some((f) => effect.features.includes(f)),
    );
    for (const found of effect.dependencies) {
      if (!design.dependencies.some((d) => d.featureId === found.featureId)) design.dependencies.push(found);
    }
    return;
  }
  const touched = new Set(effect.replies.flatMap((r) => r.artifacts));
  design.artifacts = stamp(design.artifacts, effect.drafts, revision, (draft) => touched.has(draft.id));
  design.tweaks = [...new Set([...design.tweaks, ...effect.tweaks])];
  design.comments = design.comments.map((c) => {
    const reply = effect.replies.find((r) => r.commentId === c.id);
    if (!reply || c.status !== "open") return c;
    return { ...addressComment(c, reply.reply, reply.specLine), specSeen: reply.specLine === null };
  });
  design.taught = true;
}

/**
 * The project's design with every finished turn's effect landed. A caller
 * that changes it hands it back with `saveDesign`.
 */
export function liveDesign(projectName: string, now = Date.now()): StoredDesign {
  let design = designs.get(projectName);
  if (!design) {
    design = empty();
    designs.set(projectName, design);
  }
  for (const turn of projectTurns(projectName)) {
    if (!turn.design || isRunning(turn, now) || design.applied.includes(turn.turnId)) continue;
    land(design, turn.design);
    design.applied.push(turn.turnId);
  }
  return design;
}

export function saveDesign(projectName: string, design: StoredDesign): void {
  designs.set(projectName, design);
}

/** A design turn already taught this user how feedback works (running or not). */
export function teachingSent(projectName: string): boolean {
  return projectTurns(projectName).some((t) => t.design?.kind === "design" && t.design.teach);
}

function publicComment(stored: StoredComment): DesignComment {
  const comment: DesignComment & { specSeen?: boolean } = { ...stored };
  delete comment.specSeen;
  return comment;
}

/** The design as served. */
export function designView(projectName: string): DesignModel {
  const design = liveDesign(projectName);
  const running = projectTurns(projectName).find((t) => t.design && isRunning(t));
  return {
    revision: design.revision,
    running: running?.design
      ? { kind: running.design.kind, features: running.design.kind === "design" ? running.design.features : [] }
      : null,
    artifacts: design.artifacts,
    dependencies: design.dependencies,
    comments: design.comments.map(publicComment),
    taught: design.taught,
    commenting: true,
  };
}

/** What the spec model carries of the design (features/spec/api/specModel.ts). */
export function designSummary(projectName: string): DesignSummary {
  const design = liveDesign(projectName);
  return {
    designedFrom: design.designedFrom,
    openComments: design.comments.filter((c) => c.status === "open").length,
    specChanges: design.comments
      .filter((c) => c.status !== "resolved" && c.specLine)
      .map((c) => ({ lineId: c.specLine!, featureId: c.specLine!.split(".")[0]!, comment: c.n, seen: c.specSeen })),
  };
}

/** The spec files finished design turns rewrote, the latest write of each. */
export function designFileWrites(projectName: string, now = Date.now()): Map<string, string> {
  const files = new Map<string, string>();
  for (const turn of projectTurns(projectName)) {
    const effect = turn.design;
    if (effect?.kind !== "address" || !effect.file || isRunning(turn, now)) continue;
    files.set(effect.file.path, effect.file.content);
  }
  return files;
}

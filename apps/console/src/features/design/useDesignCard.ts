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

import { useCallback, useMemo } from "react";
import type { ReviewComment } from "@aep/agent-stream";
import { useProjectChat } from "../agent-chat/useProjectChat";
import { appPrototypes, revisingIn, type AppPrototype } from "../prototype/model/prototypes";
import { usePrototypeReviews, type PrototypeReviews } from "../prototype/usePrototypeReviews";
import { useSpecDoc } from "../spec/collab/specDoc";
import { answerOpenQuestion } from "../spec/collab/specEdits";
import { useRoomFiles } from "../spec/collab/useRoomFiles";
import { useSpecState } from "../spec/api/specModel";
import { useSpecWorkspace } from "../spec/useSpecWorkspace";
import type { LineBlock } from "../spec/model/ids";
import { keepDecision, readDecisionLines } from "./decisionsRoom";
import type { DesignDependency } from "./api/designModel";
import { blockingDependencies } from "./model/artifacts";
import { assumedCount } from "./model/decisions";
import { artifactHashes, DECISIONS_PATH, designItems, DESIGN_FILE_PREFIXES, reviewLists, type DesignItem, type ReviewList } from "./model/designFiles";
import type { DesignQuestion } from "./model/questions";
import { answerComment, changedSinceChecked, commentsOn, isChecked, isWaiting, primaryAction, waitingComments, type Primary } from "./model/review";
import { noteAnsweredHere } from "./useAnsweredHere";
import { useDesignModel } from "./useDesignModel";
import { useDesignQuestions } from "./useDesignQuestions";
import { useDesignReview, type DesignReviewActions } from "./useDesignReview";

// Everything the Design card shows, worked out once for the header and the
// body: the rail's items from the room's files (model/designFiles.ts), each
// one's hash, the review (review.json), the questions waiting on the user
// (model/questions.ts), the web applications' prototypes, the dependencies
// that block a build, the technical decisions' lines and the one primary
// action.

/** One row of an Overview review list. */
export interface ReviewRow {
  item: DesignItem;
  checked: boolean;
  changed: boolean;
  /** Comments on it waiting to be sent. */
  waiting: number;
  /** Decisions in it still assumed: the technical decisions only. */
  assumed: number;
}

export interface DesignCard {
  items: DesignItem[];
  hashes: Readonly<Record<string, string>>;
  review: DesignReviewActions;
  lists: Record<ReviewList, ReviewRow[]>;
  waiting: ReviewComment[];
  /** Each artifact's revision (the design runs that changed it, the first design 1), by review path. */
  revisions: Readonly<Record<string, number>>;
  /** The questions the design waits on the user for. */
  questions: DesignQuestion[];
  /** Answer one: a feature's goes into its file's Decisions, a comment's reopens the comment. False when it could not be written. */
  answer: (question: DesignQuestion, text: string) => boolean;
  primary: Primary;
  /** Each web application's prototype, by component. */
  prototypes: ReadonlyMap<string, AppPrototype>;
  reviews: PrototypeReviews;
  /** The dependencies that wait on a person before their feature builds. */
  blocking: DesignDependency[];
  /** The technical decisions' lines, read from the room. */
  decisions: LineBlock[];
  /** Keep an assumed decision, by its line's text: its tag comes off. False when nothing was written. */
  keepDecision: (text: string) => boolean;
}

const NO_PROTOTYPES: AppPrototype[] = [];
const NO_REVISIONS: Readonly<Record<string, number>> = {};
const NO_LINES: LineBlock[] = [];

export function useDesignCard(projectName: string): DesignCard {
  const doc = useSpecDoc(projectName);
  const files = useRoomFiles(doc, DESIGN_FILE_PREFIXES);
  const spec = useSpecWorkspace(projectName);
  const review = useDesignReview(projectName);
  const model = useDesignModel(projectName).data;
  const chat = useProjectChat(projectName);
  const items = useMemo(() => designItems(files), [files]);
  const hashes = useMemo(() => artifactHashes(items), [items]);
  // Re-read when the file's text changes: the lines come from its fragment, which Keep edits.
  const decisionsText = files[DECISIONS_PATH];
  const decisions = useMemo(() => (doc && decisionsText !== undefined ? readDecisionLines(doc) : NO_LINES), [doc, decisionsText]);
  const keep = useCallback((text: string) => doc !== null && keepDecision(doc, text), [doc]);

  // A design run (Design, or Address comments) may rewrite a prototype: it is revising until the run ends.
  const instruction = chat.turn.phase === "idle" ? undefined : chat.turn.instruction;
  const designRunning = model?.running != null;
  const webApps = useMemo(() => items.flatMap((i) => (i.body.kind === "prototype" ? [i.body.component] : [])), [items]);
  const list = useMemo(
    () => (webApps.length > 0 ? appPrototypes(webApps, files, designRunning ? { component: null } : revisingIn(instruction)) : NO_PROTOTYPES),
    [webApps, files, designRunning, instruction],
  );
  const reviews = usePrototypeReviews(projectName, list);

  const questions = useDesignQuestions(projectName);
  const revisions = useSpecState(projectName).data?.artifactRevisions ?? NO_REVISIONS;
  const toDesign = spec.workspace?.design.label != null;
  const designedFrom = spec.model.data?.design.designedFrom;
  const answer = useCallback(
    (question: DesignQuestion, text: string): boolean => {
      if (!text.trim()) return false;
      if (question.kind === "feature") {
        if (doc === null || !answerOpenQuestion(doc, question.path, question.question, text)) return false;
        noteAnsweredHere(projectName, question.featureId, designedFrom?.[question.featureId]);
        return true;
      }
      review.change((r) => answerComment(r, question.key, text));
      return review.ready;
    },
    [doc, review, projectName, designedFrom],
  );

  const card = useMemo(() => {
    const row = (item: DesignItem): ReviewRow => ({
      item,
      checked: isChecked(review.review, item.path, hashes[item.path]),
      changed: changedSinceChecked(review.review, item.path, hashes[item.path]),
      waiting: commentsOn(review.review, item.path).filter(isWaiting).length,
      assumed: item.body.kind === "decisions" ? assumedCount(decisions) : 0,
    });
    const byList = reviewLists(items);
    const lists = { business: byList.business.map(row), technical: byList.technical.map(row) };
    const waiting = waitingComments(review.review);
    const rows = [...lists.business, ...lists.technical].map((r) => ({ key: r.item.key, checked: r.checked }));
    return {
      items,
      hashes,
      review,
      lists,
      waiting,
      revisions,
      questions,
      answer,
      primary: primaryAction({ questions: questions.length, waiting: waiting.length, toDesign, rows }),
      prototypes: new Map(list.map((p) => [p.component, p])),
      blocking: blockingDependencies(model?.dependencies ?? []),
      decisions,
      keepDecision: keep,
    };
  }, [items, hashes, review, list, model?.dependencies, revisions, questions, answer, toDesign, decisions, keep]);
  return { ...card, reviews };
}

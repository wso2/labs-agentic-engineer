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

// The pending question entry for the spec body's question form (spike):
// observes the room's shared `questions` map, and mirrors this client's own
// chat log into it — LIVE (subscribed to the log), so a question surfaces no
// matter when it arrives relative to the doc's lifecycle: mid-stream on this
// route, while the user was on another page, or after a reload.

import { useEffect, useState } from "react";
import type { Doc } from "yjs";
import { getMessages, subscribe } from "./chatStore.js";
import { answerableQuestionIds } from "./questionCards.js";
import {
  closeStaleRoomQuestions,
  mirrorQuestion,
  observeRoomQuestions,
  readRoomQuestions,
  type RoomQuestion,
} from "./questionRoom.js";

/** The newest still-open question entry in the room, if any. */
function pendingQuestion(all: RoomQuestion[]): RoomQuestion | undefined {
  for (let i = all.length - 1; i >= 0; i--) {
    const e = all[i]!;
    if (!e.submitted) return e;
  }
  return undefined;
}

export function useRoomQuestion(doc: Doc | null, chatKey: string): RoomQuestion | undefined {
  const [entry, setEntry] = useState<RoomQuestion | undefined>(undefined);

  // Mirror this client's chat log into the room — initially AND on every log
  // change (subscribed), so the fold appending a question mid-stream reaches
  // the doc without racing the doc's publication. Idempotent by toolCallId.
  // Only questions the log still considers ANSWERABLE mirror (no later
  // delivered user message): the room's `submitted` flag lives in an ephemeral
  // doc (rooms unload when empty), but the submitted/skipped answer persists
  // in the log right after the question — that check is what stops a fresh doc
  // from resurrecting an already-answered form. No ownership claim (#430 D5):
  // the chat thread is project-scoped, every member's log converges on the
  // same questions, and any member may submit.
  useEffect(() => {
    if (!doc) return;
    // Close entries the log no longer backs: superseded (answered via the
    // card or a composer reply) and orphaned (no log message at all — a
    // persistent collab room outlives a cleared log). Runs from the log
    // subscription AND the doc observer (entries can sync in from the server
    // after mount); it only WRITES when something actually closes, so the
    // observer → close → observer path settles instead of looping.
    const closeStale = () => {
      const messages = getMessages(chatKey);
      const answerable = answerableQuestionIds(messages);
      const superseded = new Set<string>();
      const known = new Set<string>();
      for (const m of messages) {
        if (m.role !== "question" || !m.toolCallId) continue;
        known.add(m.toolCallId);
        if (!answerable.has(m.id)) superseded.add(m.toolCallId);
      }
      closeStaleRoomQuestions(doc, superseded, known);
    };
    const backfill = () => {
      const messages = getMessages(chatKey);
      const answerable = answerableQuestionIds(messages);
      for (const m of messages) {
        if (m.role !== "question" || !m.questions?.length) continue;
        if (!m.toolCallId || !answerable.has(m.id)) continue;
        mirrorQuestion(doc, {
          toolCallId: m.toolCallId,
          questions: m.questions,
          ...(m.streaming ? { streaming: true } : {}),
        });
      }
      closeStale();
    };
    backfill();
    const unsubscribe = subscribe(chatKey, backfill);
    const unobserve = observeRoomQuestions(doc, closeStale);
    return () => {
      unsubscribe();
      unobserve();
    };
  }, [doc, chatKey]);

  useEffect(() => {
    if (!doc) {
      setEntry(undefined);
      return;
    }
    const read = () => setEntry(pendingQuestion(readRoomQuestions(doc)));
    read();
    return observeRoomQuestions(doc, read);
  }, [doc]);

  return entry;
}

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
 * The open new comment's text as React state, following the review (the
 * kit's `followComment`): the one way both hosts keep a comment's text, so
 * typed text is never lost. The review going away (closed, sent, its
 * prototype gone) keeps the open comment's text as a draft too.
 */

import { useEffect, useRef, useState } from "react";
import type { FeedbackQueue } from "../feedback/queue.js";
import { followComment, keepOpenComment } from "./comment-draft.js";
import type { ReviewState } from "./review-state.js";

/** A change to the review's comment queue, applied to the latest queue (several can land in one event). */
export type QueueUpdate = (queue: FeedbackQueue) => FeedbackQueue;

export interface CommentDraft {
  /** The open new comment's text. */
  text: string;
  setText: (text: string) => void;
  /** The selection's next change is a Shift-click: the text goes along to the new selection. */
  carryNext: () => void;
}

export function useCommentDraft({ review, queue, onQueue }: { review: ReviewState; queue: FeedbackQueue; onQueue: (update: QueueUpdate) => void }): CommentDraft {
  const [text, setText] = useState("");
  const shown = useRef(review);
  const carry = useRef(false);

  useEffect(() => {
    const before = shown.current;
    if (before === review) return;
    shown.current = review;
    const along = carry.current;
    carry.current = false;
    const next = followComment(queue, before, text, review, along);
    if (next.queue !== queue) onQueue((q) => followComment(q, before, text, review, along).queue);
    setText(next.text);
  }, [review, queue, text, onQueue]);

  const latest = useRef({ review, text, onQueue });
  useEffect(() => {
    latest.current = { review, text, onQueue };
  });
  useEffect(
    () => () => {
      const { review, text, onQueue } = latest.current;
      onQueue((q) => keepOpenComment(q, review, text));
    },
    [],
  );

  return {
    text,
    setText,
    carryNext: () => {
      carry.current = true;
    },
  };
}

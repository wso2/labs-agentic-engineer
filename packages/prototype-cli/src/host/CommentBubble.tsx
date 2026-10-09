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
 * The comment bubble an Annotate click opens next to the element: drawn by
 * the host over the frame, so typed text stays out of the untrusted
 * prototype. It names what it points at; Add or Cmd/Ctrl+Enter queues the
 * comment. The comment's length limit holds here (counted near the limit); a
 * full queue disables Add (the bar says why).
 */

import type { KeyboardEvent } from "react";
import { AnchoredBubble, type BubbleAnchor } from "./AnchoredBubble.js";
import { CommentField } from "./CommentField.js";

export interface CommentBubbleProps {
  /** Where the commented elements are, or the comment bar for the whole screen; nothing is drawn until known. */
  anchor: BubbleAnchor | null;
  /** What the comment is on: the elements' labels in selection order, or the screen. */
  labels: readonly string[];
  /** The queue holds the most comments a submission takes. */
  full: boolean;
  /** The comment's text: the reviewer's, or the draft the bubble reopened with. */
  text: string;
  onText: (text: string) => void;
  onAdd: (text: string) => void;
  /** The reviewer clicked away (typed text is kept as a draft); `refocus` when the click left focus nowhere. Escape is the review's. */
  onClose: (refocus: boolean) => void;
}

export function CommentBubble({ anchor, labels, full, text, onText, onAdd, onClose }: CommentBubbleProps) {
  const empty = text.trim() === "";
  const add = () => {
    if (!full && !empty) onAdd(text.trim());
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      add();
    }
  };
  const name = labels.join(", ");
  return (
    <AnchoredBubble anchor={anchor} label={`Comment on ${name}`} onClickAway={onClose} onKeyDown={onKeyDown}>
      <p className="ph-bubble-on">{name}</p>
      <CommentField text={text} onText={onText} />
      <div className="ph-bubble-actions">
        <button type="button" className="ph-primary" onClick={add} disabled={full || empty}>
          Add
        </button>
      </div>
    </AnchoredBubble>
  );
}

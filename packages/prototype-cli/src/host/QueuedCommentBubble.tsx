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
 * A queued comment, opened where it was made (from the bar's list or its
 * pin): its number, what it is on, its text, Edit to change it in place (Save,
 * or Cmd/Ctrl+Enter), and Remove. An edit in progress holds the bubble open
 * against a click away; Escape (the review's) drops it.
 */

import { useState, type KeyboardEvent } from "react";
import type { FeedbackRequest } from "@wso2/prototype-kit/feedback";
import { AnchoredBubble, type BubbleAnchor } from "./AnchoredBubble.js";
import { CommentField } from "./CommentField.js";

export function QueuedCommentBubble({
  anchor,
  number,
  request,
  on,
  onEdit,
  onRemove,
  onClose,
}: {
  anchor: BubbleAnchor | null;
  /** The comment's 1-based number, as its pin and the bar's list show it. */
  number: number;
  request: FeedbackRequest;
  /** What it is on, as the reviewer reads it (element labels, or the whole screen). */
  on: string;
  onEdit: (text: string) => void;
  onRemove: () => void;
  /** A click away (not while editing); `refocus` when the click left focus nowhere. */
  onClose: (refocus: boolean) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const save = () => {
    if (draft === null || draft.trim() === "") return;
    onEdit(draft.trim());
    setDraft(null);
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (draft !== null && e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      save();
    }
  };
  return (
    <AnchoredBubble anchor={anchor} label={`Comment ${number}`} onClickAway={(refocus) => draft === null && onClose(refocus)} onKeyDown={onKeyDown}>
      <p className="ph-bubble-on">{`${number} · ${on}`}</p>
      {draft === null ? <p className="ph-bubble-text">{request.text}</p> : <CommentField text={draft} onText={setDraft} />}
      <div className="ph-bubble-actions">
        {draft === null ? (
          <>
            <button type="button" className="ph-danger" onClick={onRemove}>
              Remove
            </button>
            <button type="button" className="ph-primary" autoFocus onClick={() => setDraft(request.text)}>
              Edit
            </button>
          </>
        ) : (
          <>
            <button type="button" onClick={() => setDraft(null)}>
              Cancel
            </button>
            <button type="button" className="ph-primary" onClick={save} disabled={draft.trim() === ""}>
              Save
            </button>
          </>
        )}
      </div>
    </AnchoredBubble>
  );
}

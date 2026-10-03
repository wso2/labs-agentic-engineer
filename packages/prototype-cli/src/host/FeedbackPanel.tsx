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

/** Annotate's side panel: the selection, a request to attach to it, the queue, and Save feedback. */

import { useState } from "react";
import { MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT, type FeedbackRequest } from "@wso2/prototype-kit/feedback";

export interface FeedbackPanelProps {
  /** The selected elements' labels, in selection order. */
  selection: string[];
  queue: readonly FeedbackRequest[];
  /** The queue was started against a revision that has since been replaced. */
  stale: boolean;
  onAdd: (text: string) => void;
  onRemove: (index: number) => void;
  onSave: () => Promise<string>;
}

export function FeedbackPanel({ selection, queue, stale, onAdd, onRemove, onSave }: FeedbackPanelProps) {
  const [text, setText] = useState("");
  const [status, setStatus] = useState<string | null>(null);
  const full = queue.length >= MAX_FEEDBACK_REQUESTS;
  const add = () => {
    if (full || text.trim() === "") return;
    onAdd(text.trim());
    setText("");
  };
  const save = () => {
    setStatus("Saving…");
    onSave().then(setStatus, (e: unknown) => setStatus(`Not saved: ${e instanceof Error ? e.message : String(e)}`));
  };
  return (
    <aside className="ph-feedback" aria-label="Feedback">
      <h2>Feedback</h2>
      <p className="ph-selection">{selection.length === 0 ? "Click elements to select them, or write about the whole screen." : `Selected: ${selection.join(", ")}`}</p>
      <label>
        Request
        <textarea value={text} onChange={(e) => setText(e.target.value)} rows={3} maxLength={MAX_FEEDBACK_TEXT} />
      </label>
      <button type="button" onClick={add} disabled={full || text.trim() === ""}>
        Add request
      </button>
      {full && <p role="note">{`The queue is full (${MAX_FEEDBACK_REQUESTS} requests): save or remove one to add another.`}</p>}
      {stale && queue.length > 0 && <p role="note">Queued against an earlier version of the prototype.</p>}
      <ol className="ph-queue" aria-label="Queued requests">
        {queue.map((r, i) => (
          <li key={i}>
            <span>{r.text}</span>
            <small>
              {r.screenId}
              {r.elementIds.length > 0 ? ` · ${r.elementIds.join(", ")}` : ""}
            </small>
            <button type="button" aria-label={`Remove request ${i + 1}`} onClick={() => onRemove(i)}>
              Remove
            </button>
          </li>
        ))}
      </ol>
      <button type="button" onClick={save} disabled={queue.length === 0}>
        Save feedback
      </button>
      {status && <p role="status">{status}</p>}
    </aside>
  );
}

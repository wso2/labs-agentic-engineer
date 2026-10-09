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

/** A bubble's comment input: labelled "Comment", focused on opening, held to the comment's length limit with a counter near it. */

import { useId } from "react";
import { MAX_FEEDBACK_TEXT } from "@wso2/prototype-kit/feedback";

/** How close to the limit the counter shows. */
const COUNT_FROM = MAX_FEEDBACK_TEXT - 200;

export function CommentField({ text, onText }: { text: string; onText: (text: string) => void }) {
  const id = useId();
  const counter = `${id}-count`;
  const counting = text.length > COUNT_FROM;
  return (
    <div className="ph-field">
      {/* A label beside the input, not around it: one around it would take the typed text into the field's name. */}
      <label htmlFor={id}>Comment</label>
      <textarea
        id={id}
        rows={3}
        autoFocus
        value={text}
        maxLength={MAX_FEEDBACK_TEXT}
        aria-describedby={counting ? counter : undefined}
        onChange={(e) => onText(e.target.value)}
      />
      {counting && <small id={counter}>{`${text.length} / ${MAX_FEEDBACK_TEXT}`}</small>}
    </div>
  );
}

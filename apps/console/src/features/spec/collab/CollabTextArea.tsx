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

import { useEffect, useRef } from "react";
import { TextField } from "@wso2/oxygen-ui";
import type * as Y from "yjs";
import { applyTextareaValue, observeRemote } from "./textareaBinding";

// The collaborative flavor of the spec view's placeholder editor: same
// TextField, but its value is a shared Y.Text. Uncontrolled — Yjs owns the
// state; React only mounts it (keyed by path in the parent).
export function CollabTextArea({
  ytext,
  path,
  isLocalTransaction,
  readOnly,
}: {
  ytext: Y.Text;
  path: string;
  isLocalTransaction: (transaction: Y.Transaction) => boolean;
  /** Whether THIS caller lacks ae:design (read-only viewer). Blocks the
   *  native textarea's own input handling AND the onChange write-through —
   *  belt and suspenders, since a readOnly input can still receive a
   *  programmatic value change some browsers' autofill/extensions trigger. */
  readOnly: boolean;
}) {
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.value = ytext.toString();
    return observeRemote(ytext, el, isLocalTransaction);
  }, [ytext, isLocalTransaction]);

  return (
    <TextField
      fullWidth
      multiline
      minRows={20}
      inputRef={inputRef}
      defaultValue={ytext.toString()}
      onChange={(e) => {
        if (readOnly) return;
        applyTextareaValue(ytext, e.target.value);
      }}
      aria-label={`Content of ${path}`}
      helperText={
        readOnly
          ? `${path} — read-only (no design permission).`
          : `${path} — live collaborative draft; commits to GitHub arrive with the committer (#86 phase 3).`
      }
      slotProps={{
        input: {
          readOnly,
          sx: { fontFamily: "monospace", fontSize: "0.875rem" },
        },
      }}
    />
  );
}

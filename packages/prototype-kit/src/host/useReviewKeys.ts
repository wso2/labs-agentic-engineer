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
 * A review's keys on the host page, the same in every host: V returns to
 * Preview and C toggles Annotate (Comment mode), neither while typing, and
 * Escape undoes the nearest thing (the bubble, then the selection) before
 * anything else gets it. A key while focus
 * is inside the prototype's frame is the prototype's: the frame reports an
 * Escape it did not use itself (`PrototypeFrame.onEscape`). Headless.
 */

import { useEffect, useRef } from "react";

export interface ReviewKeys {
  /** Escape: undo the nearest thing; false when there was nothing to undo (the host may then close the review). */
  onEscape: () => boolean;
  /** C: toggle Annotate. */
  onToggleAnnotate: () => void;
  /** V: back to Preview. */
  onPreview: () => void;
}

export interface ReviewKeysOptions {
  /**
   * Listen on the way down and stop a consumed Escape there, for a host whose
   * review sits in something that closes on Escape itself (a dialog), so it
   * never sees the Escape the review used. Otherwise a consumed Escape is only
   * marked handled (`preventDefault`).
   */
  capture?: boolean;
}

/** Whether the key was typed into something that takes text (a bubble's input, a picker). */
function typing(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));
}

/** The review's keys while `keys` is given (none: the host has no review to key, e.g. no Annotate). */
export function useReviewKeys(keys: ReviewKeys | null, { capture = false }: ReviewKeysOptions = {}): void {
  const latest = useRef(keys);
  useEffect(() => {
    latest.current = keys;
  });
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const keys = latest.current;
      if (!keys || e.target instanceof HTMLIFrameElement || e.defaultPrevented) return;
      if (e.key === "Escape") {
        if (!keys.onEscape()) return;
        if (capture) e.stopPropagation();
        else e.preventDefault();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      const key = e.key.toLowerCase();
      if (key === "c") {
        e.preventDefault();
        keys.onToggleAnnotate();
      } else if (key === "v") {
        e.preventDefault();
        keys.onPreview();
      }
    };
    window.addEventListener("keydown", onKeyDown, { capture });
    return () => window.removeEventListener("keydown", onKeyDown, { capture });
  }, [capture]);
}

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

import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { chatWidth } from "./chatWidth";

// The dragged width is the user's, not the project's: one width kept in this
// browser for every project. Storage can be missing or refused (a private
// window); the chat then opens at the golden share and a drag lasts until the
// page goes.

const KEY = "aep:shell:chat-width";

function readSaved(): number | null {
  try {
    const n = Number.parseInt(localStorage.getItem(KEY) ?? "", 10);
    return Number.isFinite(n) ? n : null;
  } catch {
    return null;
  }
}

function subscribeToResize(onChange: () => void): () => void {
  window.addEventListener("resize", onChange);
  return () => window.removeEventListener("resize", onChange);
}

const readWindowWidth = () => window.innerWidth;

export interface ChatWidth {
  width: number;
  /** Pin the chat at `px`, held to what the room allows now. */
  resizeTo: (px: number) => void;
  /** Forget the pinned width: back to the golden share. */
  reset: () => void;
}

/** The chat's width beside the page, tracking the window, and the user's hold on it. */
export function useChatWidth(): ChatWidth {
  const windowWidth = useSyncExternalStore(subscribeToResize, readWindowWidth);
  const [saved, setSaved] = useState<number | null>(readSaved);

  useEffect(() => {
    try {
      if (saved === null) localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, String(saved));
    } catch {
      /* not kept: it still applies until the page goes */
    }
  }, [saved]);

  const resizeTo = useCallback((px: number) => setSaved(chatWidth(windowWidth, px)), [windowWidth]);
  const reset = useCallback(() => setSaved(null), []);
  return { width: chatWidth(windowWidth, saved), resizeTo, reset };
}

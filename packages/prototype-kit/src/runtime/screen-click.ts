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
 * Annotate's clicks on empty space: a click (or tap) on nothing that takes a
 * comment — no selectable element, no pin — is a whole-screen comment at that
 * spot. It is where the comment cursor shows its hollow bubble (`kit-css.ts`).
 */

import type { FramePoint } from "../host/bridge.js";

/** What takes a click of its own in Annotate: a selectable element (and what is inside it) or a pin. */
const OWN_CLICK = "[data-proto-annotating], .proto-pin";

/**
 * Calls `onClick` with where a pointer click on empty space hit: in the
 * viewport (`point`) and in the scrolled document (`at`). A click a key made
 * (no pointer, so no spot) is not one. Returns the stop.
 */
export function watchScreenClicks(onClick: (point: FramePoint, at: FramePoint) => void): () => void {
  const listener = (e: MouseEvent) => {
    if (e.detail === 0 || e.button !== 0) return;
    if (e.target instanceof Element && e.target.closest(OWN_CLICK)) return;
    onClick({ x: e.clientX, y: e.clientY }, { x: e.pageX, y: e.pageY });
  };
  document.addEventListener("click", listener);
  return () => document.removeEventListener("click", listener);
}

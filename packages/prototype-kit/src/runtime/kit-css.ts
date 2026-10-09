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
 * The kit's own styles: the Annotate layer (outline, label chip, the comment
 * cursor), the comment pins (a hollow one for a draft; a whole-screen
 * comment's at its spot of the document, over the prototype) and the scene's
 * pointer rule. Themes restyle them through the `--proto-*` custom
 * properties. Inline, because the frame loads nothing.
 */

/** The comment cursor's arrow: dark ink outlined in white, so one graphic reads on light and dark. */
const ARROW = `<path d="M3 2v17.5l4.6-4.3 3.1 7 3-1.3-3-6.8h6.3z" fill="#1b1d22" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/>`;
const BUBBLE = "M23 13.5a7.25 7.25 0 1 1-4.6 12.85L15.5 27.5l.9-3.6A7.25 7.25 0 0 1 23 13.5z";

/** A 32×32 cursor (hotspot at the arrow's tip), `fallback` where an image cursor is not drawn. */
function cursor(bubble: string, fallback: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">${ARROW}${bubble}</svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}") 3 2, ${fallback}`;
}

/** Over an element that takes a comment: a solid orange bubble with a white "+". */
const ADD_CURSOR = cursor(
  `<path d="${BUBBLE}" fill="#FF7300" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/><path d="M23 17.6v6.2M19.9 20.7h6.2" stroke="#fff" stroke-width="2" stroke-linecap="round"/>`,
  "crosshair",
);
/** Over empty space, where a click comments on the whole screen there: a hollow bubble. */
const EMPTY_CURSOR = cursor(`<path d="${BUBBLE}" fill="#fff" stroke="#FF7300" stroke-width="1.6" stroke-linejoin="round"/>`, "default");

export const KIT_CSS = `
.proto-scene{display:contents}
.proto-scene[data-proto-mode=annotate]{pointer-events:none}
html:has(.proto-scene[data-proto-mode=annotate]){cursor:${EMPTY_CURSOR}}
.proto-selectable{position:relative;display:block;min-width:0}
.proto-selectable.proto-inline{display:inline-block}
.proto-content{display:contents}
[data-proto-annotating]{pointer-events:auto;cursor:${ADD_CURSOR};outline:2px solid transparent;outline-offset:2px;border-radius:4px}
[data-proto-annotating]:hover:not(:has([data-proto-annotating]:hover)){outline-color:var(--proto-select-hover,#93c5fd)}
[data-proto-annotating][data-proto-selected]{outline-color:var(--proto-select,#2563eb)}
.proto-selectable[data-proto-annotating]>.proto-content{pointer-events:none}
.proto-corner{position:absolute;top:-12px;display:flex;gap:4px;z-index:3;pointer-events:none}
.proto-corner-left{left:-6px}
.proto-corner-right{right:-6px}
.proto-chip{font:600 11px/18px system-ui,sans-serif;padding:0 6px;border-radius:9px;white-space:nowrap;max-width:240px;overflow:hidden;text-overflow:ellipsis}
.proto-label{background:var(--proto-select,#2563eb);color:#fff}
.proto-pin{appearance:none;margin:0;border:1.5px solid var(--proto-pin,#f59e0b);background:var(--proto-pin,#f59e0b);color:#111;min-width:12px;text-align:center;pointer-events:auto;cursor:pointer}
.proto-pin:focus-visible{outline:2px solid var(--proto-select,#2563eb);outline-offset:1px}
.proto-pin-draft{background:var(--proto-pin-draft-bg,Canvas);color:var(--proto-pin-draft-fg,CanvasText);border-style:dashed}
.proto-root-pins{position:fixed;display:flex;gap:4px;z-index:3;pointer-events:none;transform:translateX(-100%)}
.proto-screen-pin{position:absolute;z-index:2147483000;transform:translate(-50%,-50%)}
`;

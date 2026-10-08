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
 * The kit's own styles: the Annotate layer (outline, label chip, pins) and the
 * scene's pointer rule. Themes restyle them through the `--proto-*` custom
 * properties. Inline, because the frame loads nothing.
 */

export const KIT_CSS = `
.proto-scene{display:contents}
.proto-scene[data-proto-mode=annotate]{pointer-events:none}
.proto-selectable{position:relative;display:block;min-width:0}
.proto-selectable.proto-inline{display:inline-block}
.proto-content{display:contents}
[data-proto-annotating]{pointer-events:auto;cursor:crosshair;outline:2px solid transparent;outline-offset:2px;border-radius:4px}
[data-proto-annotating]:hover{outline-color:var(--proto-select-hover,#93c5fd)}
[data-proto-annotating][data-proto-selected]{outline-color:var(--proto-select,#2563eb)}
.proto-selectable[data-proto-annotating]>.proto-content{pointer-events:none}
.proto-corner{position:absolute;top:-12px;display:flex;gap:4px;z-index:3;pointer-events:none}
.proto-corner-left{left:-6px}
.proto-corner-right{right:-6px}
.proto-chip{font:600 11px/18px system-ui,sans-serif;padding:0 6px;border-radius:9px;white-space:nowrap;max-width:240px;overflow:hidden;text-overflow:ellipsis}
.proto-label{background:var(--proto-select,#2563eb);color:#fff}
.proto-pin{background:var(--proto-pin,#f59e0b);color:#111;min-width:12px;text-align:center}
[data-proto-root][data-proto-pins]{position:relative}
[data-proto-root][data-proto-pins]::after{content:attr(data-proto-pins);position:absolute;top:2px;right:2px;font:600 11px/18px system-ui,sans-serif;padding:0 6px;border-radius:9px;background:var(--proto-pin,#f59e0b);color:#111;pointer-events:none;z-index:2}
`;

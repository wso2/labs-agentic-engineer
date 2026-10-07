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

/** The preview/export host page: the config as inert JSON, then the host app's script (by URL, or inline for export). */

import { HOST_CONFIG_ID, type HostConfig } from "./host-config.js";

/**
 * The exported page's CSP: inline script and styles only, nothing fetched.
 * The sandboxed frame's `srcdoc` document inherits it, so it allows what the
 * frame's module loader needs (`'unsafe-eval'`) and the frame narrows it
 * further with its own.
 */
export const EXPORT_CSP = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; form-action 'none'; base-uri 'none'";

/** JSON that is safe inside a `<script>` element: no `<` survives to close it. */
function scriptJson(value: unknown): string {
  return JSON.stringify(value).replace(/</g, "\\u003c");
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export function renderHostPage(title: string, config: HostConfig, script: { src: string } | { inline: string }): string {
  const scriptTag = "src" in script ? `<script type="module" src="${escapeHtml(script.src)}"></script>` : `<script>${script.inline.replace(/<\/script/gi, "<\\/script")}</script>`;
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    ...(config.mode === "export" ? [`<meta http-equiv="Content-Security-Policy" content="${EXPORT_CSP}">`] : []),
    `<title>${escapeHtml(title)}</title>`,
    "</head>",
    "<body>",
    '<div id="root"></div>',
    `<script type="application/json" id="${HOST_CONFIG_ID}">${scriptJson(config)}</script>`,
    scriptTag,
    "</body>",
    "</html>",
  ].join("\n");
}

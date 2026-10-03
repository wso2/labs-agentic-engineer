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
 * The sandboxed frame's whole document, for `srcdoc`, with its CSP: its own
 * inline runtime and the prototype it evaluates may run (`'unsafe-eval'` for
 * the module loader); nothing may be fetched, framed, posted or navigated to.
 * The runtime is inline because a sandboxed frame has an opaque origin and
 * could not load a script from the host's origin without CORS.
 */

export const PROTOTYPE_FRAME_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline' 'unsafe-eval'",
  "style-src 'unsafe-inline'",
  "img-src data:",
  "font-src data:",
  "connect-src 'none'",
  "frame-src 'none'",
  "worker-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
].join("; ");

/**
 * Runs before the runtime: anything that throws uncaught in the frame (the
 * runtime failing as it loads, a theme, the prototype's own handlers) is
 * reported to the host as `proto:error`, so the host never waits on a frame
 * that has died silently.
 */
const REPORT_ERRORS = [
  "(() => {",
  "const report = (e) => parent.postMessage({ type: 'proto:error', message: String((e && e.message) || e) }, '*');",
  "addEventListener('error', (e) => report(e.error || e.message));",
  "addEventListener('unhandledrejection', (e) => report(e.reason));",
  "})();",
].join("\n");

export function prototypeFrameDocument(runtime: string): string {
  // `</script` cannot appear inside the inline script; `<\/script` is the same JavaScript.
  const script = runtime.replace(/<\/script/gi, "<\\/script");
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${PROTOTYPE_FRAME_CSP}">`,
    "<style>html,body,#root{height:100%;margin:0}#root{display:flex;flex-direction:column}</style>",
    "</head>",
    "<body>",
    '<div id="root"></div>',
    `<script>${REPORT_ERRORS}</script>`,
    `<script>${script}</script>`,
    "</body>",
    "</html>",
  ].join("\n");
}

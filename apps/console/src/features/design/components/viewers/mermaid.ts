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

// Mermaid, loaded on the first diagram so a design without one pays nothing
// for the (large) library, and rendered one diagram at a time: mermaid keeps
// global state across a render, so concurrent renders deadlock. Copied from
// the old console's spec editor (mermaidRenderer.ts at the classic-console tag).

type RenderFn = (id: string, source: string) => Promise<{ svg: string }>;

let renderImpl: RenderFn | null = null;

async function load(): Promise<RenderFn> {
  if (!renderImpl) {
    const mermaid = (await import("mermaid")).default;
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "neutral",
      // A failed parse otherwise leaves mermaid's error drawing under <body>,
      // outside React, for good.
      suppressErrorRendering: true,
    });
    renderImpl = (id, src) => mermaid.render(id, src);
  }
  return renderImpl;
}

let seq = 0;
let queue: Promise<unknown> = Promise.resolve();

/** Render `source` to SVG markup, one render at a time. */
export function renderMermaid(source: string): Promise<string> {
  seq += 1;
  const id = `design-mermaid-${seq}`;
  const run = queue.then(async () => (await (await load())(id, source)).svg);
  queue = run.catch(() => undefined);
  return run;
}

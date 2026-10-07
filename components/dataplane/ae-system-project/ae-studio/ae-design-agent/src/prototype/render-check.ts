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
 * The prototype write gate's render check: draws every screen of a generated
 * prototype, for every role and display state, against the Oxygen theme's check
 * runtime. `@wso2/prototype-kit/check` runs it in an isolated Node child (the
 * permission model, no network, no filesystem, a timeout), because the module
 * being drawn is model output.
 *
 * Both packages are workspace dependencies whose `dist` the image builds; the
 * theme is resolved once, at import, so an image without its runtimes fails to
 * boot instead of failing every prototype write.
 *
 * Checks are bounded pod-wide (`MAX_RENDER_CHECKS`): each child has a 384 MiB
 * heap inside the container's 1 Gi, and one pod runs every project's turns.
 * A turn's own writes already queue behind its pending verdict (the write
 * ledger), so the waiters are at most one per running turn
 * (`design/pod-memory-bounds.md`).
 */

import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { PrototypeRenderCheck } from "@aep/agent-stream";
import { checkPrototypeFiles, resolveTheme } from "@wso2/prototype-kit/check";

const THEME = resolveTheme("@wso2/prototype-theme-oxygen", [dirname(fileURLToPath(import.meta.url))]);

/**
 * Render checks running at once in the pod. One: two children (768 MiB of
 * heap) beside the agent's own heap would leave too little of the 1 Gi.
 */
export const MAX_RENDER_CHECKS = 1;

/** `check`, letting at most `max` calls run at once; the rest wait in call order. */
export function boundedRenderCheck(check: PrototypeRenderCheck, max: number): PrototypeRenderCheck {
  let running = 0;
  const waiting: Array<() => void> = [];
  const release = (): void => {
    const next = waiting.shift();
    if (next) next();
    else running--;
  };
  return async (files) => {
    // A freed slot passes straight to the next waiter, so `running` never dips.
    if (running < max) running++;
    else await new Promise<void>((r) => waiting.push(r));
    try {
      return await check(files);
    } finally {
      release();
    }
  };
}

export const checkPrototypeRender: PrototypeRenderCheck = boundedRenderCheck(
  (files) => checkPrototypeFiles(files, { theme: THEME }),
  MAX_RENDER_CHECKS,
);

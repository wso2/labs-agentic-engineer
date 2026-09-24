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

// The plugin's half of the startup probe (startup.ts, assertion 4): proves
// `experimental.chat.system.transform` fires, without a model call.
//
// OpenCode runs, per model request, the system transform, then `chat.params`,
// then the provider call. For the probe session the transform writes a marker
// and `chat.params` throws, so the request ends before it leaves the process.

import { STARTUP_PROBE_PROMPT } from "./protocol.js";

export interface StartupProbe {
  /** `chat.message`: a session whose message is the probe prompt is the probe. */
  noteMessage(sessionID: string, parts: readonly unknown[]): void;
  isProbe(sessionID: string): boolean;
  /** `experimental.chat.system.transform` fired for the probe: say so. */
  markTransform(sessionID: string): void;
  /** `chat.params`: refuse the probe's model call. */
  refuseModelCall(sessionID: string): void;
}

export function createStartupProbe(mark: () => void): StartupProbe {
  const probes = new Set<string>();
  const isProbe = (sessionID: string): boolean => probes.has(sessionID);
  return {
    noteMessage(sessionID, parts) {
      const text = parts.map((p) => (p && typeof p === "object" ? (p as { text?: unknown }).text : undefined));
      if (sessionID && text.length === 1 && text[0] === STARTUP_PROBE_PROMPT) probes.add(sessionID);
    },
    isProbe,
    markTransform(sessionID) {
      if (isProbe(sessionID)) mark();
    },
    refuseModelCall(sessionID) {
      if (isProbe(sessionID)) throw new Error("aep-guard: startup probe, no model call");
    },
  };
}

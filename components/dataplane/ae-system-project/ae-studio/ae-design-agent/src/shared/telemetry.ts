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
 * Trace-capture seam — the single module that knows about DevTools. Capture
 * used to ride the MODEL seam (a `devToolsMiddleware` wrapped around the model
 * built for each turn), which is why every turn of one conversation showed up
 * as its own unrelated run: DevTools mints a run id when the capturing object
 * is constructed, and that object's lifetime was one turn. Identity was being
 * derived from an object's lifetime.
 *
 * Here it is derived from the TURN instead: the integration registers once at
 * the composition root, and each turn stamps its own `functionId`. Model
 * lifetimes are untouched — the per-request key still leaves with the response.
 *
 * DevTools does NOT merge runs by `functionId`; a run is still one SDK call.
 * What the label buys is attribution: the viewer renders `function_id` in its
 * run list, so two projects generating at the same time are told apart at a
 * glance instead of guessed at by timestamp.
 */

import type { Telemetry, TelemetryOptions } from "ai";
import { DevToolsTelemetry } from "@ai-sdk/devtools";
import { config } from "./config.js";

/**
 * Make a telemetry integration NON-FATAL. The SDK awaits integration callbacks
 * with no try/catch of its own (`mergeTelemetryCallback` → `await
 * mergedIntegrationCallback(event)`), so a throw inside one propagates into the
 * generation and kills the turn. DevTools reads and rewrites a JSON file on
 * these hooks, so a corrupt or unwritable capture file is enough — this is the
 * same failure that once surfaced as "stream ended without a manifest", and the
 * reason the retired middleware was wrapped the same way. Trace capture is a
 * debugging aid; it must never be able to break a live generation.
 *
 * Observer hooks (`on*`) are swallowed. `executeTool` is a WRAPPER, not an
 * observer — swallowing it would drop the tool's result, so it falls through to
 * the unwrapped `execute()` exactly once, mirroring the middleware's
 * `catch { return o.doGenerate(); }`.
 */
export function nonFatalTelemetry(integration: Telemetry): Telemetry {
  const guarded: Telemetry = {};
  for (const [key, value] of Object.entries(integration)) {
    if (typeof value !== "function") continue;
    if (key === "executeTool") continue; // handled below — it returns a value
    const hook = value as (event: unknown) => unknown;
    Object.assign(guarded, {
      [key]: async (event: unknown) => {
        try {
          await hook.call(integration, event);
        } catch {
          // Capture is best-effort: the turn continues untraced.
        }
      },
    });
  }

  const { executeTool } = integration;
  if (executeTool) {
    // Generic in T: the wrapper returns the TOOL's result, so erasing T here
    // would erase every tool's result type at the call site.
    guarded.executeTool = async <T>(options: {
      callId: string;
      toolCallId: string;
      execute: () => PromiseLike<T>;
    }): Promise<T> => {
      try {
        // `.call` preserves `this` for integrations that need it but erases the
        // generic; the declared signature returns PromiseLike<T> for this same
        // T, so the cast restores what the call site already knows.
        return (await executeTool.call(integration, options)) as T;
      } catch {
        return options.execute();
      }
    };
  }
  return guarded;
}

/**
 * The capture integration to register at the composition root, or undefined
 * when capture is off. Registration is global and once-per-process, which is
 * why it belongs to the root and not to a turn.
 *
 * `DevToolsTelemetry()` THROWS under `NODE_ENV=production` by design, so the
 * `config.devtools` gate is load-bearing rather than merely tidy.
 */
export function captureTelemetry(): Telemetry | undefined {
  if (!config.devtools) return undefined;
  return nonFatalTelemetry(DevToolsTelemetry());
}

/**
 * A conversation id rendered for the viewer's run list. Ids are namespaced
 * `org_<org>--proj_<project>--<useCase>--<uuid>`; the list truncates from the
 * right, so the uuid — the only segment that differs between two conversations
 * of the same project — is dropped rather than allowed to push the project name
 * out of view.
 *
 * Defensive by construction: an id that does not match the namespaced shape is
 * returned verbatim. Evals, the playground and lazily-created conversations all
 * use plain ids, and a LABEL must never be able to fail a turn — which is also
 * why this does not reuse `snapshot-path.ts`'s parser, whose job is to reject.
 */
export function threadLabel(conversationId: string): string {
  const segments = conversationId.split("--");
  if (segments.length !== 4) return conversationId;
  const [orgSeg, projSeg, useCase] = segments as [string, string, string, string];
  if (!orgSeg.startsWith("org_") || !projSeg.startsWith("proj_")) return conversationId;
  return `${orgSeg.slice("org_".length)}/${projSeg.slice("proj_".length)}/${useCase}`;
}

/**
 * This turn's telemetry options — the `functionId` every step of the turn is
 * stamped with. Undefined when capture is off, so the call is byte-identical to
 * one made before any of this existed.
 */
export function turnTelemetry(conversationId: string): TelemetryOptions | undefined {
  if (!config.devtools) return undefined;
  return { functionId: threadLabel(conversationId) };
}

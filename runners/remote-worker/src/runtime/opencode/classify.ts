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

// OpenCode bus events → what the run loop is told about each (`MessageClass`,
// runtime/port.ts).
//
// The mirror of `runtime/claude/classify.ts`, and it tells the loop the same
// things about the same situations:
//
//   session.status {retry}          → retry          (the watchdog names the stall)
//   permission.asked                → stall_signal   permission_denied (the runtime rejects it)
//   session.compacted               → stall_signal   compaction
//   message.part.delta              → model_wait     (a per-token frame: never logged)
//   session.status {busy}           → model_wait     (a session waiting on its model)
//   aep.tick                        → tool_progress  (the adapter's clock; see messages.ts)
//   the ROOT session going idle     → turn_end
//   a CHILD session created / idle  → task_bookkeeping started / ended — the ids
//                                     `RunStream.stopTask` takes are session ids
//   aep.skills                      → init           (the preload check)
//   server / plugin / watcher chatter → noise        (see NOISE_EVENTS)
//   everything else                 → activity
//
// Per session, because it has to know which session is the root: the first
// `session.created` with no parent. The translator keys on the same fact the
// same way, so the two cannot disagree about whose idle ends a turn.

import type { ApiRetryInfo, MessageClass, MessageClassifier } from "../port.js";
import { num, obj, str } from "../fields.js";
import { NOISE_EVENTS, readEvent, SKILLS_DECLARED, TOOL_TICK } from "./messages.js";

export interface OpencodeClassifierOptions {
  /** The clock a retry's `next` timestamp is measured against; tests pin it. */
  now?: () => number;
}

/**
 * The retry's error as a CLOSED word, never the provider's text.
 *
 * OpenCode's retry status carries a free-text `message` (whatever the provider
 * said), and the loop prints `error` into a feed the console forwards to a
 * browser — the same reason Claude Code's classifier only ever passes the SDK's
 * enum through. So the text is read for a class and dropped.
 */
function retryClass(message: string): string {
  if (/overload/i.test(message)) return "overloaded";
  if (/rate.?limit|too many requests|\b429\b/i.test(message)) return "rate_limit";
  if (/timeout|timed out|ETIMEDOUT|ECONNRESET|ECONNREFUSED|socket/i.test(message)) return "connection";
  if (/\b5\d\d\b|server error|internal/i.test(message)) return "server_error";
  return "unknown";
}

/** An HTTP status the message names outright, or null — never guessed. */
function retryStatus(message: string): number | null {
  const m = /\b([45]\d\d)\b/.exec(message);
  return m ? Number(m[1]) : null;
}

function readRetry(status: Record<string, unknown>, now: number): ApiRetryInfo {
  const message = str(status.message);
  const next = num(status.next);
  return {
    attempt: num(status.attempt),
    // OpenCode does not state a ceiling — see ApiRetryInfo.maxRetries.
    maxRetries: null,
    retryDelayMs: next > 0 ? Math.max(0, next - now) : 0,
    errorStatus: retryStatus(message),
    error: retryClass(message),
  };
}

/** The class of one `permission.asked`: always a denial, since the runtime rejects every ask. */
function permissionSignal(props: Record<string, unknown>): MessageClass {
  const permission = str(props.permission) || "unknown";
  const patterns = Array.isArray(props.patterns) ? (props.patterns as unknown[]).filter((p) => typeof p === "string") : [];
  const on = patterns.length > 0 ? ` on ${patterns.slice(0, 3).join(", ")}` : "";
  return {
    kind: "stall_signal",
    signal: {
      level: "warn",
      code: "permission_denied",
      // The rule set is explicit allow/deny, so an ask means a rule the platform
      // did not write; it is rejected rather than left to hang the run.
      detail: `[permission] ${permission}${on} asked for approval and was rejected — the run has no one to ask`.slice(
        0,
        200,
      ),
    },
  };
}

/** The classifier for ONE session. */
export function createOpencodeClassifier(opts?: OpencodeClassifierOptions): MessageClassifier {
  const now = opts?.now ?? Date.now;
  let rootId = "";
  const children = new Set<string>();

  return (message) => {
    const ev = readEvent(message);
    if (!ev) return { kind: "activity" };
    const p = ev.properties;

    switch (ev.type) {
      case SKILLS_DECLARED: {
        const skills = (message as { skills?: unknown }).skills;
        return { kind: "init", resolvedSkills: Array.isArray(skills) ? skills.filter((s) => typeof s === "string") : [] };
      }
      case TOOL_TICK:
        return { kind: "tool_progress" };
      case "message.part.delta":
        return { kind: "model_wait", streaming: true };
      case "permission.asked":
        return permissionSignal(p);
      case "session.compacted":
        return { kind: "stall_signal", signal: { level: "info", code: "compaction", detail: "[compact] auto compaction" } };
      case "session.status": {
        const status = obj(p.status);
        const type = str(status.type);
        if (type === "retry") return { kind: "retry", info: readRetry(status, now()) };
        // Busy is the model being asked, not work done: as activity it would
        // reset the idle clock whenever the limiter dropped its heartbeat.
        if (type === "busy") return { kind: "model_wait", streaming: false };
        // The idle that ENDS something is `session.idle`, below, so the pair
        // never counts twice.
        return { kind: "activity" };
      }
      case "session.created": {
        const info = obj(p.info);
        const id = str(info.id);
        if (!id) return { kind: "activity" };
        if (!str(info.parentID)) {
          if (!rootId) rootId = id;
          return { kind: "activity" };
        }
        children.add(id);
        return { kind: "task_bookkeeping", started: id };
      }
      case "session.idle": {
        const id = str(p.sessionID);
        if (id && id === rootId) return { kind: "turn_end" };
        if (children.has(id)) return { kind: "task_bookkeeping", ended: id };
        return { kind: "activity" };
      }
      default:
        return NOISE_EVENTS.has(ev.type) ? { kind: "noise" } : { kind: "activity" };
    }
  };
}

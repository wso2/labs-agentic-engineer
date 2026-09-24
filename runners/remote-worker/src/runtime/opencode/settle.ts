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

// When an OpenCode session's stream ENDS — which is when the run settles
// (run_loop.ts: "the run settles when the stream closes").
//
// Claude Code's stream ends on its own: the CLI exits once its input is
// released. OpenCode's does not — `GET /event` is the whole server's bus and it
// stays open for as long as the server does — so the adapter has to decide, and
// the rule is:
//
//   the ROOT session is idle, NO other session is busy, and NO permission is
//   waiting for an answer.
//
// With foreground fan-out (the only shape this platform runs on OpenCode,
// ADR-0015) the root cannot go idle before its children: every `task` call
// blocks it until the child returns, measured on S2b (children idle at
// 7.1-11.5s, the root at 13.7s). So the rule is a GUARD there rather than the
// settle itself, and it stays because a rule that only holds by accident of
// timing is not a rule — a child still busy when the root idles means something
// went wrong, and closing then would kill it mid-flight.
//
// It is NOT sufficient for OpenCode's experimental background mode, and the
// replay of S2c proves it: there the root idles at once, and when the last
// child idles the rule holds for an instant BEFORE the root is woken with the
// children's results — so the stream would close with the lead's wrap-up turn
// unread. That mode would need "every background child reported back" as a
// fourth condition. It is not adopted, and `startup.ts` refuses a server that
// offers it, which is why this rule can stay three conditions long.
//
// Pure: it reads events and answers one question, so the replay tests can hold
// it against every recording.

import { obj, str } from "../fields.js";
import { readEvent, SKILLS_DECLARED } from "./messages.js";

export interface StreamCloser {
  /** Feed one message; true when the stream should close AFTER yielding it. */
  observe(message: unknown): boolean;
  /** Sessions currently busy — what `close()` aborts. */
  busySessions(): string[];
}

export function createStreamCloser(): StreamCloser {
  let rootId = "";
  let rootIdle = false;
  const busy = new Set<string>();
  const pendingPermissions = new Set<string>();

  function settled(): boolean {
    if (!rootId || !rootIdle) return false;
    for (const id of busy) if (id !== rootId) return false;
    return pendingPermissions.size === 0;
  }

  return {
    observe(message) {
      const ev = readEvent(message);
      if (!ev) return false;
      const p = ev.properties;
      switch (ev.type) {
        case "session.created": {
          const info = obj(p.info);
          if (!rootId && !str(info.parentID)) rootId = str(info.id);
          return false;
        }
        case "session.status": {
          const id = str(p.sessionID);
          const status = str(obj(p.status).type);
          if (!id) return false;
          // `retry` is a session still working: it will send again.
          if (status === "busy" || status === "retry") {
            busy.add(id);
            if (id === rootId) rootIdle = false;
            return false;
          }
          // The IDLE that counts is `session.idle`, which follows this one: it is
          // what the classifier ends a turn on, so closing here would end the
          // stream one message before the root's `turn_ended`.
          if (status === "idle") busy.delete(id);
          return false;
        }
        case "session.idle": {
          const id = str(p.sessionID);
          busy.delete(id);
          if (id && id === rootId) rootIdle = true;
          return settled();
        }
        case "permission.asked":
          if (str(p.id)) pendingPermissions.add(str(p.id));
          return false;
        case "permission.replied":
          pendingPermissions.delete(str(p.requestID) || str(p.permissionID));
          return settled();
        default:
          return false;
      }
    },
    busySessions: () => [...busy],
  };
}

export interface SessionStreamOptions {
  /** The skills the server discovered — the stream's first message (messages.ts). */
  skills: readonly string[];
  /** A permission ask arrived; the runtime rejects it (runtime.ts). */
  onPermissionAsked?: (properties: Record<string, unknown>) => void;
}

/**
 * The session's `RunStream.messages`: the skills declaration, then the bus
 * until the close rule holds. The message that satisfies the rule is yielded
 * BEFORE the stream ends, so the loop reads the root's last idle (its
 * `turn_ended`) and then sees the stream close (its `run_settled`).
 *
 * The runtime and the replay tests both read a session through this, so a
 * recording settles exactly where a live run would.
 */
export async function* sessionStream(
  source: AsyncIterable<unknown>,
  closer: StreamCloser,
  opts: SessionStreamOptions,
): AsyncGenerator<unknown, void, undefined> {
  yield { type: SKILLS_DECLARED, skills: [...opts.skills] };
  for await (const message of source) {
    const ev = readEvent(message);
    if (ev?.type === "permission.asked") opts.onPermissionAsked?.(ev.properties);
    yield message;
    if (closer.observe(message)) return;
  }
}

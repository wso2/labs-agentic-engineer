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

import type { TurnOutcome } from "../../agent-chat/chatStore";
import type { AppPrototype } from "./prototypes";
import { NEW_SESSION, follow, turnEnded, type ReviewSession } from "./revision";

// Each project's prototype reviews (model/revision.ts), kept outside React so
// they outlive the review overlay and the Prototype tab itself: a batch out
// with the agent is still there when the reviewer comes back, the turn's end
// is recorded even with no review open, and a failed batch is given back the
// next time the prototype is followed. Kept for the page's life, like the
// chat store; nothing here is persisted.

/** Each component's review session, by component. */
export type ReviewSessions = Readonly<Record<string, ReviewSession>>;

/** How a turn ended, from the chat (`chatStore.onTurnEnd`). */
export interface TurnEnds {
  onTurnEnd(fn: (projectName: string, outcome: TurnOutcome) => void): () => void;
}

const NONE: ReviewSessions = Object.freeze({});

/** The sessions following the prototypes as they are now (the same object when nothing changed). */
export function followAll(sessions: ReviewSessions, prototypes: readonly AppPrototype[]): ReviewSessions {
  let followed = sessions;
  for (const p of prototypes) {
    const before = followed[p.component] ?? NEW_SESSION;
    const after = follow(before, p);
    if (after !== before) followed = { ...followed, [p.component]: after };
  }
  return followed;
}

export function createReviewStore(chat: TurnEnds) {
  const projects = new Map<string, { sessions: ReviewSessions; listeners: Set<() => void> }>();

  const entry = (projectName: string) => {
    let e = projects.get(projectName);
    if (!e) {
      e = { sessions: NONE, listeners: new Set() };
      projects.set(projectName, e);
    }
    return e;
  };
  const set = (projectName: string, sessions: ReviewSessions) => {
    const e = entry(projectName);
    if (sessions === e.sessions) return;
    e.sessions = sessions;
    for (const fn of e.listeners) fn();
  };

  // Every project's turns, for the page's life: an ending is recorded whether or not its review is open.
  chat.onTurnEnd((projectName, outcome) => {
    const { sessions } = entry(projectName);
    const ended = Object.entries(sessions).map(([c, s]) => [c, turnEnded(s, outcome)] as const);
    if (ended.some(([c, s]) => s !== sessions[c])) set(projectName, Object.fromEntries(ended));
  });

  return {
    /** The project's sessions; the same object until one changes. */
    get(projectName: string): ReviewSessions {
      return projects.get(projectName)?.sessions ?? NONE;
    },
    /** Be told when the project's sessions change; returns the unsubscribe. */
    subscribe(projectName: string, fn: () => void): () => void {
      const e = entry(projectName);
      e.listeners.add(fn);
      return () => e.listeners.delete(fn);
    },
    /** Follow the project's prototypes as the room and the chat now have them. */
    follow(projectName: string, prototypes: readonly AppPrototype[]): void {
      set(projectName, followAll(entry(projectName).sessions, prototypes));
    },
    /** Change one component's session. */
    change(projectName: string, component: string, fn: (s: ReviewSession) => ReviewSession): void {
      const { sessions } = entry(projectName);
      const before = sessions[component] ?? NEW_SESSION;
      const after = fn(before);
      if (after !== before) set(projectName, { ...sessions, [component]: after });
    },
  };
}

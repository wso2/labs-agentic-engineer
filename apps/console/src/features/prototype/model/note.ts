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

import { parsePrototypeCommand } from "@aep/contracts/commands";
import type { ChatItem, NoteAction } from "../../agent-chat/chatLog";

// The chat's line after a `/prototype` turn: where to look at the result. It
// is worked out from the conversation (the turn's own writes, which the
// history carries) and the room (whether the prototype is valid now), never
// posted, so a reload and a teammate see the same lines.

export interface PrototypeNote {
  /** The item the note follows: the exchange's last. */
  afterId: string;
  text: string;
  actions: NoteAction[];
}

const PROTOTYPE_FILE = /^specs\/design\/components\/([^/]+)\/prototype\.(?:json|tsx)$/;

function noteFor(afterId: string, components: readonly string[]): PrototypeNote {
  const [only] = components;
  return components.length === 1 && only
    ? {
        afterId,
        text: `The ${only} prototype is ready to review.`,
        actions: [{ kind: "open-prototype", label: "Open prototype", component: only }],
      }
    : {
        afterId,
        text: "The prototypes are ready to review.",
        actions: [{ kind: "open-prototype", label: "Open prototypes", component: null }],
      };
}

/**
 * An Open prototype note after each `/prototype` exchange that produced a
 * prototype: it wrote a web application's prototype files, and that
 * prototype is valid now (`ready`, both files there and the manifest sound).
 * An exchange that wrote nothing (up to date, refused) or left no valid
 * prototype earns none; the one still running earns its note when it ends.
 */
export function prototypeNotes(items: readonly ChatItem[], ready: ReadonlySet<string>, running: boolean): PrototypeNote[] {
  const notes: PrototypeNote[] = [];
  let exchange: { lastId: string; written: Set<string> } | null = null;
  const close = () => {
    const components = exchange ? [...exchange.written].filter((c) => ready.has(c)) : [];
    if (exchange && components.length > 0) notes.push(noteFor(exchange.lastId, components));
    exchange = null;
  };
  for (const item of items) {
    if (item.kind === "user") {
      // A message that was not sent reached no agent: it neither ends an exchange nor is part of one.
      if (item.state === "failed") continue;
      close();
      if (parsePrototypeCommand(item.text)) exchange = { lastId: item.id, written: new Set() };
      continue;
    }
    if (!exchange) continue;
    exchange.lastId = item.id;
    const component = item.kind === "activity" && item.state === "done" ? PROTOTYPE_FILE.exec(item.path)?.[1] : undefined;
    if (component) exchange.written.add(component);
  }
  if (!running) close();
  return notes;
}

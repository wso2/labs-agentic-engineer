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

import { useMemo } from "react";
import type { ChatItem } from "../agent-chat/chatLog";
import { prototypeNotes, type PrototypeNote } from "./model/note";
import { usePrototypes } from "./usePrototypes";

/**
 * The chat's Open prototype notes, by the item each follows: one after every
 * `/prototype` exchange that produced a prototype valid now (model/note.ts).
 */
export function usePrototypeNotes(projectName: string, items: readonly ChatItem[], running: boolean): ReadonlyMap<string, PrototypeNote> {
  const prototypes = usePrototypes(projectName);
  const ready = useMemo(() => new Set((prototypes ?? []).flatMap((p) => (p.status === "ready" ? [p.component] : []))), [prototypes]);
  return useMemo(() => new Map(prototypeNotes(items, ready, running).map((n) => [n.afterId, n])), [items, ready, running]);
}

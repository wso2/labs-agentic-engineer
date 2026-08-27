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

// Cross-subtree handoff for register-form drafts (#05 Task 2). runTurn folds
// a `draftExternalResource` tool-call into this Map; RegisterFormPage
// subscribes. Lives next to chatStore so runTurn does not import marketplace.

import type { RegisterDraft } from "../marketplace/lib/registerDraft.js";

export {
  parseRegisterDraft,
  DRAFT_EXTERNAL_RESOURCE_TOOL,
  type RegisterDraft,
} from "../marketplace/lib/registerDraft.js";

const drafts = new Map<string, RegisterDraft>();
const listeners = new Map<string, Set<() => void>>();

function notify(chatKey: string): void {
  for (const fn of listeners.get(chatKey) ?? []) fn();
}

export function publishRegisterDraft(chatKey: string, draft: RegisterDraft): void {
  drafts.set(chatKey, draft);
  notify(chatKey);
}

export function peekRegisterDraft(chatKey: string): RegisterDraft | null {
  return drafts.get(chatKey) ?? null;
}

export function subscribeRegisterDraft(chatKey: string, fn: () => void): () => void {
  const set = listeners.get(chatKey) ?? new Set();
  set.add(fn);
  listeners.set(chatKey, set);
  return () => set.delete(fn);
}

/** Drop any draft for this key — tests drain the module-scoped map. */
export function clearRegisterDraft(chatKey: string): void {
  if (!drafts.has(chatKey)) return;
  drafts.delete(chatKey);
  notify(chatKey);
}

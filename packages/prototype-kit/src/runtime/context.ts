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
 * What every kit component and hook reads: the reviewer's view (mode, role,
 * display state, screen), the manifest, the params the last navigation
 * carried, the mock data store, and the two ways out — navigate and toggle a
 * selection.
 */

import { createContext, useContext } from "react";
import type { PrototypeManifest } from "../manifest/types.js";
import type { DataStore } from "./store.js";

export type KitMode = "preview" | "annotate";

/** The part of the review the reviewer controls, as the host sets it. */
export interface KitView {
  mode: KitMode;
  roleId: string;
  stateId: string;
  screenId: string;
  /** Selected element ids, in selection order (Annotate). */
  selectedKeys: readonly string[];
  /** Queued requests' numbers per element id on this screen (Annotate). */
  pins: Readonly<Record<string, readonly number[]>>;
}

export interface KitContextValue {
  manifest: PrototypeManifest;
  view: KitView;
  /** The params the last navigation to the current screen carried. */
  params: Readonly<Record<string, string>>;
  /** Navigate in Preview; a no-op while annotating. */
  go: (screenId: string, params?: Record<string, string>) => void;
  /** Toggle an element's selection (Annotate). */
  toggle: (elementKey: string) => void;
  store: DataStore;
}

export const KitContext = createContext<KitContextValue | null>(null);

export function useKit(): KitContextValue {
  const ctx = useContext(KitContext);
  if (!ctx) throw new Error("a kit component or hook renders only inside a prototype screen");
  return ctx;
}

/** Whether `screenId` is a manifest screen. */
export function isScreen(ctx: KitContextValue, screenId: string): boolean {
  return ctx.manifest.screens.some((s) => s.id === screenId);
}

/** Whether the viewing role reaches `screenId`. */
export function reaches(ctx: KitContextValue, screenId: string): boolean {
  return ctx.manifest.screens.some((s) => s.id === screenId && s.roleIds.includes(ctx.view.roleId));
}

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
 * What pressing a kit control does: its `onPress`, then its `to`, in Preview
 * only. While annotating, the selection layer takes the click first; the
 * guard covers Enter on a focused control.
 */

import { useKit, type KitContextValue } from "./context.js";

export interface Pressable {
  /** Navigate to this screen when pressed. Prefer it to `onPress` for plain navigation: the checks verify it. */
  to?: string | undefined;
  /** The params `to` carries, read on the target with `useParams`. */
  params?: Record<string, string> | undefined;
  /** Run when pressed: change mock data, open a dialog, navigate conditionally. */
  onPress?: (() => void) | undefined;
}

/** The press handler for a control, as a plain function so a component may call it per item. */
export function pressHandler(ctx: KitContextValue, { to, params, onPress }: Pressable): () => void {
  return () => {
    if (ctx.view.mode === "annotate") return;
    onPress?.();
    if (to !== undefined) ctx.go(to, params);
  };
}

export function usePress(pressable: Pressable): () => void {
  return pressHandler(useKit(), pressable);
}

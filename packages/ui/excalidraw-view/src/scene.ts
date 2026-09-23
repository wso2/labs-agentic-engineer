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

/* eslint-disable @typescript-eslint/no-explicit-any */
// Type-only imports: erased at compile time, so the lazy runtime load of
// @excalidraw/excalidraw (lazyExcalidraw.ts) is unaffected.
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import type { ExcalidrawElement } from "@aep/excalidraw-dsl";

export type Scene = { elements?: any; appState?: any; files?: any };

// appState.collaborators is a Map at runtime; a JSON round-trip turns it into a
// plain object and crashes Excalidraw's iteration. Drop it — the lib rebuilds it.
export function sanitizeAppState(appState: any): any {
  if (!appState || typeof appState !== "object") return appState;
  const { collaborators: _drop, ...rest } = appState;
  return rest;
}

export function parseScene(value: string): Scene | null {
  if (!value) return null;
  try {
    const parsed = JSON.parse(value);
    if (!parsed || typeof parsed !== "object") return null;
    return {
      elements: parsed.elements,
      appState: sanitizeAppState(parsed.appState),
      files: parsed.files,
    };
  } catch {
    return null;
  }
}

export function fitContentToViewport(api: any, elements: any) {
  requestAnimationFrame(() => {
    try {
      api.scrollToContent(elements, { fitToContent: true, viewportZoomFactor: 0.9, animate: false });
    } catch {
      /* api torn down */
    }
  });
}

/**
 * Bring a subset of the scene into view — one screen, or the screens an edit
 * touched. A slightly tighter zoom factor than the whole-board fit, so a single
 * screen fills the viewport with a small margin rather than floating in space.
 *
 * Typed against Excalidraw's own imperative API (type-only import — erased at
 * compile time, so the runtime lazy-load is unaffected). The elements are our
 * compiler's — runtime-compatible with what `scrollToContent` reads (it only
 * takes bounds), but declared independently, so the library call needs an
 * explicit unknown-bridge. The cast localises that single seam instead of
 * untyping the whole signature.
 */
export function focusElements(
  api: ExcalidrawImperativeAPI,
  elements: readonly ExcalidrawElement[],
  animate: boolean,
): void {
  if (!elements.length) return;
  requestAnimationFrame(() => {
    try {
      api.scrollToContent(elements as unknown as Parameters<ExcalidrawImperativeAPI["scrollToContent"]>[0], {
        fitToContent: true,
        viewportZoomFactor: 0.85,
        animate,
      });
    } catch {
      /* api torn down */
    }
  });
}

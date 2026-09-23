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

import { Suspense, useEffect, useMemo, useRef } from "react";
import { Box, CircularProgress } from "@wso2/oxygen-ui";
// Type-only: erased at compile time, so the lazy runtime import in
// lazyExcalidraw.ts is unaffected and the bundle still splits.
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import type { ExcalidrawElement } from "@aep/excalidraw-dsl";
import { ExcalidrawComponent } from "./lazyExcalidraw.js";
import { parseScene, fitContentToViewport, focusElements } from "./scene.js";
import {
  elementsOfScreens,
  openingFocusElements,
  screenAtViewportCenter,
  screensToFollow,
} from "./screenFocus.js";

export interface ExcalidrawViewProps {
  /** Serialised Excalidraw scene JSON. */
  scene: string;
  /** Fill the parent's height (else fixed 600px). */
  fillHeight?: boolean;
  /**
   * Screens the viewport should move to once this `scene` is applied — the
   * compiler's own report of what the last edit touched (`compileWireframes`
   * → `changedScreens`). Empty or absent means "leave the viewport where the
   * reader put it": the view never guesses what changed.
   */
  focusScreens?: readonly string[];
}

// On open, land on the FIRST screen at a readable size, with the top of the
// second peeking below as the cue that there is more — the peek is part of
// the fitted box, not left to the panel's aspect ratio. A scene with no
// screen tags (older compiles, non-wireframe scenes) falls back to fitting
// everything.
function focusInitial(api: ExcalidrawImperativeAPI, elements: ExcalidrawElement[] | undefined) {
  if (!elements?.length) return;
  const target = openingFocusElements(elements);
  if (target.length) focusElements(api, target, false);
  else fitContentToViewport(api, elements);
}

function ExcalidrawViewImpl({ scene, fillHeight, focusScreens }: ExcalidrawViewProps) {
  // Committed scenes remount via `key` upstream (uncontrolled + simple). A
  // STREAMED scene instead keeps one mounted canvas and pushes each new
  // compile through `updateScene` — remounting this (lazy, canvas-heavy)
  // component per line-flush would flicker and drop the viewport. The DSL
  // compiler emits stable element ids/seeds, so successive scenes diff
  // cleanly: existing elements keep their identity, new ones appear.
  const initialData = useMemo(() => parseScene(scene), [scene]);
  const apiRef = useRef<ExcalidrawImperativeAPI | null>(null);
  const mountedScene = useRef(scene);
  // Read inside the scene effect without being a dependency of it: a focus
  // rides the scene it was reported for, and a later prop change alone must
  // not re-pan a canvas the reader has since moved.
  const focusRef = useRef(focusScreens);
  focusRef.current = focusScreens;
  // What the previous scene changed — one flush of memory, enough to tell a
  // sweep (a different single screen each flush) from an edit being written.
  const previouslyChanged = useRef<readonly string[] | null>(null);
  const hostRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (scene === mountedScene.current) return; // initial mount already has it
    mountedScene.current = scene;
    const api = apiRef.current;
    const next = parseScene(scene);
    if (!api || !next?.elements) return; // unparseable → keep the last frame
    try {
      api.updateScene({ elements: next.elements });
      // Follow the agent's work: the compiler says which screens this scene
      // changed. Nothing reported → the viewport stays exactly where the
      // reader put it — never a refit of the whole board. And even a reported
      // change does not always move the camera: not if the reader is already
      // on that screen, and not once the flushes reveal a sweep across screens.
      const changed = focusRef.current ?? [];
      const host = hostRef.current;
      const looking = host
        ? screenAtViewportCenter(next.elements, api.getAppState(), host.clientWidth, host.clientHeight)
        : null;
      const target = screensToFollow(changed, looking, previouslyChanged.current);
      if (target.length > 0) focusElements(api, elementsOfScreens(next.elements, target), true);
      // A frame that changed nothing is a natural boundary: clearing the
      // memory there means a sweep's aftermath cannot make the NEXT unrelated
      // single-screen edit look like a continuation of it.
      previouslyChanged.current = changed.length > 0 ? changed : null;
    } catch {
      /* api torn down */
    }
  }, [scene]);

  return (
    <Box
      sx={{
        flex: fillHeight ? 1 : undefined,
        height: fillHeight ? undefined : "600px",
        minHeight: 0,
        minWidth: 0,
        position: "relative",
        width: "100%",
        overflow: "hidden",
        border: "1px solid",
        borderColor: "divider",
        borderRadius: 1,
        "& .help-icon": { display: "none !important" },
        "& .dropdown-menu-button": { display: "none !important" },
        "& .App-menu_top__left": { display: "none !important" },
      }}
    >
      <Box ref={hostRef} sx={{ position: "absolute", inset: 0, width: "100%", height: "100%" }}>
        <ExcalidrawComponent
          // parseScene returns a loose shape (scene.ts); aligning it with
          // Excalidraw's ExcalidrawInitialDataState is its own change.
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          initialData={initialData as any}
          viewModeEnabled
          excalidrawAPI={(api: ExcalidrawImperativeAPI) => {
            apiRef.current = api;
            focusInitial(api, initialData?.elements);
          }}
        />
      </Box>
    </Box>
  );
}

export function ExcalidrawView(props: ExcalidrawViewProps) {
  const fallback = (
    <Box
      sx={{
        flex: props.fillHeight ? 1 : undefined,
        height: props.fillHeight ? "100%" : "600px",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <CircularProgress size={28} />
    </Box>
  );
  return (
    <Suspense fallback={fallback}>
      <ExcalidrawViewImpl {...props} />
    </Suspense>
  );
}

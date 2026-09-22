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
 * The prototype review page's body (review shell variant D, "Inspector"): a
 * full-viewport takeover with the review bar on top and the application in
 * a browser window below. Preview shows nothing else. Annotate slides the
 * feedback inspector in from the right and the window shrinks to make room;
 * leaving Annotate removes it (#817).
 *
 * The queued requests are the page's (`feedback`), not view state: they
 * outlive the selection, the screen and a refreshed model. What the shell adds
 * is where a request points — the current screen, flow, display state and
 * selection — and the numbered pins on queued components.
 *
 * It owns the view state and reports the part a URL carries through
 * `onRequestChange` whenever that part changes, so the route can keep screen,
 * flow, state and mode in the address bar. It starts from `request` and does
 * not follow later changes to it — the URL follows the page, not the other
 * way round.
 */

import { useEffect, useMemo, useReducer, useRef, type ReactElement, type ReactNode } from "react";
import { Box, Slide, Stack } from "@wso2/oxygen-ui";
import type { PrototypeModelV1 } from "@aep/prototype-model";
import { annotationFor, pinsOnScreen, type PrototypeAnnotation } from "../model/annotations";
import {
  initialPrototypeView,
  reducePrototypeView,
  viewRequestOf,
  type PrototypeViewEvent,
  type PrototypeViewRequest,
  type PrototypeViewState,
} from "../model/viewState";
import { BrowserFrame } from "./BrowserFrame";
import { FeedbackInspector } from "./FeedbackInspector";
import { PrototypeRenderer } from "./PrototypeRenderer";
import { ReviewBar } from "./ReviewBar";

export interface PrototypeShellProps {
  model: PrototypeModelV1;
  request: PrototypeViewRequest;
  onRequestChange: (next: PrototypeViewRequest) => void;
  backLink: ReactElement<{ children?: ReactNode }>;
  /** A page-level notice under the review bar — the Outdated banner (#818). */
  notice?: ReactNode;
  feedback: PrototypeShellFeedback;
}

/** The Annotate batch as the shell sees it (#817) — see usePrototypeFeedback. */
export interface PrototypeShellFeedback {
  annotations: readonly PrototypeAnnotation[];
  onAdd: (annotation: Omit<PrototypeAnnotation, "id">) => void;
  onRemove: (id: string) => void;
  onSendAll: () => void;
  /** This batch is on its way. */
  sending: boolean;
  /** An agent turn is running on the project: the mode holds and nothing is queued or sent. */
  busy: boolean;
  error: string | null;
}

function sameRequest(a: PrototypeViewRequest, b: PrototypeViewRequest): boolean {
  return a.screen === b.screen && a.flow === b.flow && a.state === b.state && (a.mode ?? "preview") === (b.mode ?? "preview");
}

/** The address the frame shows: the component's host and the screen's ID as a path. */
function addressOf(model: PrototypeModelV1, screenId: string): string {
  const path = screenId.replace(/^screen\./, "").replace(/\./g, "/");
  return `${model.component}.example.com/${path}`;
}

export function PrototypeShell({ model, request, onRequestChange, backLink, notice, feedback }: PrototypeShellProps) {
  const [view, dispatch] = useReducer(
    (s: PrototypeViewState, e: PrototypeViewEvent) => reducePrototypeView(model, s, e),
    request,
    (r) => initialPrototypeView(model, r),
  );

  // A new model (a refreshed file) keeps the reviewer where they were, as far
  // as the new model allows, and clears the selection.
  const shownModel = useRef(model);
  useEffect(() => {
    if (shownModel.current === model) return;
    shownModel.current = model;
    dispatch({ type: "MODEL_REPLACED", model });
  }, [model]);

  const current = viewRequestOf(view);
  const reported = useRef(request);
  useEffect(() => {
    if (sameRequest(reported.current, current)) return;
    reported.current = current;
    onRequestChange(current);
  });

  const annotating = view.mode === "annotate";
  // Escape clears the selection — anywhere on the page, while annotating.
  useEffect(() => {
    if (!annotating) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) dispatch({ type: "CLEAR_SELECTION" });
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [annotating]);

  const pins = useMemo(
    () => pinsOnScreen(feedback.annotations, view.screenId),
    [feedback.annotations, view.screenId],
  );
  const locked = feedback.busy || feedback.sending;
  const add = (text: string) => {
    feedback.onAdd(annotationFor(view, text));
    // The request is queued with what it was about; the next starts fresh.
    dispatch({ type: "CLEAR_SELECTION" });
  };

  return (
    <Box sx={{ height: "100vh", display: "flex", flexDirection: "column", bgcolor: "background.default" }}>
      <ReviewBar model={model} view={view} dispatch={dispatch} backLink={backLink} modeLocked={locked} />
      {notice}
      <Stack direction="row" sx={{ flex: 1, minHeight: 0, overflow: "hidden" }}>
        <Box sx={{ flex: 1, minWidth: 0, display: "flex", p: 2 }}>
          <BrowserFrame title={model.name} address={addressOf(model, view.screenId)}>
            <PrototypeRenderer model={model} view={view} dispatch={dispatch} pins={pins} />
          </BrowserFrame>
        </Box>
        {annotating && (
          <Slide direction="left" in appear>
            <Box sx={{ display: "flex", minHeight: 0 }}>
              <FeedbackInspector
                model={model}
                view={view}
                dispatch={dispatch}
                annotations={feedback.annotations}
                onAdd={add}
                onRemove={feedback.onRemove}
                onSendAll={feedback.onSendAll}
                sending={feedback.sending}
                locked={locked}
                error={feedback.error}
              />
            </Box>
          </Slide>
        )}
      </Stack>
    </Box>
  );
}

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
 * a browser window below. Preview shows nothing else.
 *
 * It owns the view state and reports the part a URL carries through
 * `onRequestChange` whenever that part changes, so the route can keep screen,
 * flow, state and mode in the address bar. It starts from `request` and does
 * not follow later changes to it — the URL follows the page, not the other
 * way round.
 */

import { useEffect, useReducer, useRef, type ReactElement, type ReactNode } from "react";
import { Box } from "@wso2/oxygen-ui";
import type { PrototypeModelV1 } from "@aep/prototype-model";
import {
  initialPrototypeView,
  reducePrototypeView,
  viewRequestOf,
  type PrototypeViewEvent,
  type PrototypeViewRequest,
  type PrototypeViewState,
} from "../model/viewState";
import { BrowserFrame } from "./BrowserFrame";
import { PrototypeRenderer } from "./PrototypeRenderer";
import { ReviewBar } from "./ReviewBar";

export interface PrototypeShellProps {
  model: PrototypeModelV1;
  request: PrototypeViewRequest;
  onRequestChange: (next: PrototypeViewRequest) => void;
  backLink: ReactElement<{ children?: ReactNode }>;
  /** A page-level notice under the review bar — the Outdated banner (#818). */
  notice?: ReactNode;
}

function sameRequest(a: PrototypeViewRequest, b: PrototypeViewRequest): boolean {
  return a.screen === b.screen && a.flow === b.flow && a.state === b.state && (a.mode ?? "preview") === (b.mode ?? "preview");
}

/** The address the frame shows: the component's host and the screen's ID as a path. */
function addressOf(model: PrototypeModelV1, screenId: string): string {
  const path = screenId.replace(/^screen\./, "").replace(/\./g, "/");
  return `${model.component}.example.com/${path}`;
}

export function PrototypeShell({ model, request, onRequestChange, backLink, notice }: PrototypeShellProps) {
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

  return (
    <Box sx={{ height: "100vh", display: "flex", flexDirection: "column", bgcolor: "background.default" }}>
      <ReviewBar model={model} view={view} dispatch={dispatch} backLink={backLink} />
      {notice}
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", p: 2 }}>
        <BrowserFrame title={model.name} address={addressOf(model, view.screenId)}>
          <PrototypeRenderer model={model} view={view} dispatch={dispatch} />
        </BrowserFrame>
      </Box>
    </Box>
  );
}

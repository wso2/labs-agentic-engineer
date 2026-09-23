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
 * The Annotate batch (#817): the requests a reviewer queues before one Send
 * all. The shape is the contract's `PrototypeAnnotationInput` — IDs, never
 * labels, and the reviewer's words exactly as typed.
 */

import { PROTOTYPE_SCHEMA_VERSION } from "@aep/prototype-model";
import type { components } from "../../../generated/aep-api";
import type { PrototypeViewState } from "./viewState";

export type PrototypeAnnotation = components["schemas"]["PrototypeAnnotationInput"];
export type PrototypeFeedback = components["schemas"]["PrototypeFeedbackInput"];

/** The batch ceiling the contract enforces; Add request stops there. */
export const MAX_QUEUED_REQUESTS = 50;

/**
 * A request on what the reviewer is looking at now: the current screen, flow
 * and display state, and the selection in the order it was made — none means
 * the whole screen.
 */
export function annotationFor(view: PrototypeViewState, request: string): Omit<PrototypeAnnotation, "id"> {
  return {
    prototypeSchemaVersion: PROTOTYPE_SCHEMA_VERSION,
    screenId: view.screenId,
    flowId: view.flowId,
    stateId: view.stateId,
    componentIds: [...view.selectedComponentIds],
    request,
  };
}

/**
 * The numbered pins a screen shows: for each component a queued request on
 * this screen names, the request's position in the queue (1-based, queue
 * order) — the same number its card carries.
 */
export function pinsOnScreen(annotations: readonly PrototypeAnnotation[], screenId: string): ReadonlyMap<string, number[]> {
  const pins = new Map<string, number[]>();
  annotations.forEach((a, i) => {
    if (a.screenId !== screenId) return;
    for (const id of a.componentIds) pins.set(id, [...(pins.get(id) ?? []), i + 1]);
  });
  return pins;
}

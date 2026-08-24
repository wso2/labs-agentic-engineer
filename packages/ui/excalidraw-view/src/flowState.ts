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

import type { PrototypeModel } from "@aep/excalidraw-dsl";

/**
 * Which flow the viewer should show. A model with no declared flows resolves
 * to `null`, which is what hides the flow picker and returns the viewer to its
 * pre-flows behaviour: one flat list of every screen.
 */
export function resolveFlow(model: PrototypeModel, requested?: string): string | null {
  if (model.flows.length === 0) return null;
  if (requested && model.flows.some((f) => f.name === requested)) return requested;
  return model.flows[0]!.name;
}

/** The screen a flow starts on: its first listed screen. */
export function flowEntryScreen(model: PrototypeModel, flow: string | null): string | null {
  const found = flow === null ? undefined : model.flows.find((f) => f.name === flow);
  return found?.screens[0] ?? model.screens[0]?.name ?? null;
}

/**
 * Options for the screen picker: the selected flow's screens in walkthrough
 * order, so the control reads as the flow's steps.
 *
 * `current` is appended when it sits outside the flow. That happens on a
 * cross-flow click — chrome and shared screens (Login, a sign-out landing) are
 * reachable from more than one persona — and a Select must never hold a value
 * its options do not contain.
 */
export function pickerScreens(
  model: PrototypeModel,
  flow: string | null,
  current: string,
): string[] {
  const found = flow === null ? undefined : model.flows.find((f) => f.name === flow);
  if (!found) return model.screens.map((s) => s.name);
  return found.screens.includes(current) ? [...found.screens] : [...found.screens, current];
}

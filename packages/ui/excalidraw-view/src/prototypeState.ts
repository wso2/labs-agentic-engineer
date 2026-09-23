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

export interface PrototypeNavState { current: string; stack: string[] }
export type PrototypeNavEvent =
  | { type: 'navigate'; to: string }
  | { type: 'back' }
  /** Switching flows: land on the new flow's entry screen with no history —
   *  a persona switch starts a fresh walkthrough, it does not continue the
   *  previous persona's. */
  | { type: 'reset'; to: string };

/** Figma-style prototype navigation: every navigate (click OR picker jump)
 *  pushes history; back pops it. Same-screen navigates and empty-stack backs
 *  return the SAME state object so React skips the re-render. */
export function prototypeNavReducer(
  state: PrototypeNavState,
  event: PrototypeNavEvent,
): PrototypeNavState {
  if (event.type === 'navigate') {
    if (event.to === state.current) return state;
    return { current: event.to, stack: [...state.stack, state.current] };
  }
  if (event.type === 'reset') return { current: event.to, stack: [] };
  if (state.stack.length === 0) return state;
  return { current: state.stack.at(-1)!, stack: state.stack.slice(0, -1) };
}

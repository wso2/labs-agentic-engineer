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
 * The turn's web-search and MCP-catalog gates (port of aep-api's
 * `designOrCollabTurn` / `catalogTurn`, `A/spec/turn_runner.go:134-162`).
 * Both key on the flow token (`turnSpecFor`) and on whether the turn writes
 * through the Room.
 */

/** What the gates read about a turn. */
export interface TurnGateInputs {
  /** The `/<command>` token, `""` for chat. */
  flow: string;
  /** The turn joins the project's Room (a file-writing project turn). */
  roomScoped: boolean;
}

/**
 * The web-search gate, and the base of the MCP gate: design-flow turns and
 * Room turns. The Spec view authors design.json interactively through the
 * Room, so gating on the design flow alone would starve the architect of
 * `list_org_endpoints` and it would invent cross-project service names that
 * fail exact-name resolution at build.
 */
export function designOrRoomTurn(turn: TurnGateInputs): boolean {
  return turn.flow === "design" || turn.roomScoped;
}

/**
 * The MCP discovery gate: every `designOrRoomTurn`, plus the requirements
 * flows wherever they run. A requirements interview records a registered
 * external resource as a given instead of asking which service to use, so it
 * needs `list_external_resources` even with no Room. Web search stays a
 * design-turn affair.
 */
export function catalogTurn(turn: TurnGateInputs): boolean {
  if (designOrRoomTurn(turn)) return true;
  return turn.flow === "start" || turn.flow === "amend" || turn.flow === "settle";
}

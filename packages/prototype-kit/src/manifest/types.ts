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
 * A prototype's manifest, `prototype.json` (schema version 3): what the
 * preview's pickers offer and what every reader knows without running the
 * screens. Every id is an identity — stable across revisions, what feedback
 * points at; names are display only. It carries nothing host-specific, so a
 * prototype made anywhere opens anywhere.
 */

export interface PrototypeRole {
  id: string;
  name: string;
}

export interface PrototypeDisplayState {
  id: string;
  name: string;
}

export interface PrototypeScreen {
  id: string;
  name: string;
  /** The roles that can reach the screen. */
  roleIds: string[];
}

export interface PrototypeFlow {
  id: string;
  name: string;
  /** The role that walks the flow; every step is a screen that role reaches. */
  roleId: string;
  screenIds: string[];
}

export interface PrototypeManifest {
  schemaVersion: 3;
  name: string;
  /** The screen the prototype opens on. */
  entryScreen: string;
  roles: PrototypeRole[];
  states: PrototypeDisplayState[];
  screens: PrototypeScreen[];
  flows: PrototypeFlow[];
}

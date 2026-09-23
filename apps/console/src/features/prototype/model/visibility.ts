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
 * What a reviewer can see, given the role, flow and display state they chose.
 * Pure reads of the model: the reducer uses them to keep the view reachable,
 * the review bar to fill its selectors, and the registry to hide what a
 * display state does not show.
 */

import type {
  PrototypeField,
  PrototypeFlow,
  PrototypeModelV1,
  PrototypeNavigationItem,
  PrototypeScreen,
} from "@aep/prototype-model";

/** The screens a role can reach, in model order. */
export function screensForRole(model: PrototypeModelV1, roleId: string): PrototypeScreen[] {
  return model.screens.filter((s) => s.roleIds.includes(roleId));
}

/** The named flows a role walks. */
export function flowsForRole(model: PrototypeModelV1, roleId: string): PrototypeFlow[] {
  return model.flows.filter((f) => f.roleId === roleId);
}

/** A navigation's items the role sees (an item without `roleIds` is everyone's). */
export function navigationItemsForRole(
  items: PrototypeNavigationItem[],
  roleId: string,
): PrototypeNavigationItem[] {
  return items.filter((i) => !i.roleIds || i.roleIds.includes(roleId));
}

/** Whether a node shows in a display state (every state when `showIn` is absent). */
export function isShownIn(node: { showIn?: string[] | undefined }, stateId: string): boolean {
  return !node.showIn || node.showIn.includes(stateId);
}

/** The validation message a field shows in a display state, if any. */
export function fieldErrorIn(field: PrototypeField, stateId: string): string | undefined {
  if (!field.error) return undefined;
  return !field.errorIn || field.errorIn.includes(stateId) ? field.error : undefined;
}

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
 * Keyboard focus around a host's bubble (drawn by the host, over the frame).
 * A click away closes the bubble; when that click put focus somewhere (a
 * control), it stays there, and when it put it nowhere, the host puts it
 * back where the bubble pointed, so a keyboard user carries on from there.
 */

/**
 * Whether a click away from `bubble` left keyboard focus nowhere: still in
 * the bubble (which is going away), on the page itself, or on a container
 * around the bubble (a dialog takes focus on a click on its blank parts).
 * Asked once the click's focus change happened (on `click`, not before).
 */
export function focusLeftBehind(bubble: Element): boolean {
  const active = bubble.ownerDocument.activeElement;
  return active === null || active === bubble.ownerDocument.body || active.contains(bubble) || bubble.contains(active);
}

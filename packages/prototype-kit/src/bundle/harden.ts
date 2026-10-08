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
 * Freezes the shared prototypes before the render check runs a prototype
 * module, so a prototype-pollution attempt fails loudly (a TypeError in the
 * module's strict code) instead of quietly changing what the kit sees.
 */
export function hardenIntrinsics(): void {
  for (const intrinsic of [Object.prototype, Array.prototype, Function.prototype]) Object.freeze(intrinsic);
}

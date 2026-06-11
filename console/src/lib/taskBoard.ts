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

/** Returns a readable text color for a GitHub label hex (without #).
 *  Bright labels (high luminance) get darkened so text stays legible on the tinted bg. */
export function labelTextColor(hex: string): string {
  const r = parseInt(hex.slice(0, 2), 16) / 255;
  const g = parseInt(hex.slice(2, 4), 16) / 255;
  const b = parseInt(hex.slice(4, 6), 16) / 255;
  const luminance = 0.299 * r + 0.587 * g + 0.114 * b;
  if (luminance > 0.6) {
    const d = (v: number) => Math.round(parseInt(hex.slice(v, v + 2), 16) * 0.45).toString(16).padStart(2, '0');
    return `#${d(0)}${d(2)}${d(4)}`;
  }
  return `#${hex}`;
}

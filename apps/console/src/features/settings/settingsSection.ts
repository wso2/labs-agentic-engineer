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

// The Settings card's sections, in the order its menu lists them. The open
// one is in the address (`/settings?section=ai`).

export type SettingsSection = "github" | "ai" | "usage";

export const SETTINGS_SECTIONS: readonly { key: SettingsSection; label: string }[] = [
  { key: "github", label: "GitHub" },
  { key: "ai", label: "AI agents" },
  { key: "usage", label: "Usage" },
];

/** The section a `?section=` value names; anything else (none, a stale link) opens the first. */
export function sectionFromSearch(value: unknown): SettingsSection {
  return SETTINGS_SECTIONS.find((s) => s.key === value)?.key ?? "github";
}

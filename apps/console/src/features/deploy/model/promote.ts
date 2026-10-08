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

import { missingValues, nameList } from "./dependencies";
import type { EnvironmentColumn } from "./pipeline";

// Promote, as each environment's column offers it: what it would move, to
// where, and what the next environment still lacks. The platform has no
// promote operation yet, so the button is never enabled; it says what it
// will do and why it cannot yet, rather than pretend.

export interface PromoteView {
  /** "Promote v2 to Staging". */
  label: string;
  /** Why it is disabled. */
  reason: string;
  /** What the next environment lacks: a warning, never a block. Null when nothing is missing or not known yet. */
  warning: string | null;
}

/** The column's promote button; null on the last environment, which promotes nowhere. */
export function promoteView(from: EnvironmentColumn, to: EnvironmentColumn | undefined): PromoteView | null {
  if (!from.next) return null;
  const target = to?.label ?? from.next.label;
  const label = from.version ? `Promote ${from.version} to ${target}` : `Promote to ${target}`;
  if (!from.running) {
    return { label, reason: `Nothing runs in ${from.label} to promote.`, warning: null };
  }
  const missing = to?.dependencies ? missingValues(to.dependencies) : [];
  return {
    label,
    reason: "Promoting isn't available yet.",
    warning: missing.length > 0 ? `${target} has no values yet for ${nameList(missing)}.` : null,
  };
}

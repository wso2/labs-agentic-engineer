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

import type { PageHeaderStatus } from "../../../components/PageHeader";
import type { StatusTone } from "../../../components/StatusChip";
import { validationView, type StageTone } from "../../projects/lib/pipeline";

// StageTone → StatusTone. The two unions differ only in `ghost`, which the
// shared validation mapper never returns; it is mapped for exhaustiveness only.
const TONE_TO_STATUS: Record<StageTone, StatusTone> = {
  ghost: "neutral",
  neutral: "neutral",
  info: "info",
  warning: "warning",
  success: "success",
  error: "error",
};

/**
 * The version's validation state as a chip — DERIVED from the shared mapper
 * rather than restating its cases, so no surface can drift from the
 * deployments board's reading of the same enum (ADR-0016).
 *
 * That drift is the reason this is one function: `partial`, `inconclusive` and
 * `unreported` were once chipless on the validation page while the board named
 * them correctly, and later the page read "Validation failed" for a run the
 * board correctly read as "awaiting fix".
 *
 * `null` for the states with nothing to say — the caller renders those in
 * words, because "not validated yet" is a sentence rather than a badge.
 */
export function validationChip(state: string): PageHeaderStatus | null {
  const view = validationView(state);
  if (!view) return null;
  // The shared labels are lowercase for mid-sentence use; a chip leads.
  const lead = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
  return {
    label: lead(view.label),
    // A chip that stands alone — nothing beside it spells out what "Validated*"
    // hedges — needs the spoken form. Run through the same capitalization
    // rather than pre-cased at the mapper, so one casing rule covers both names.
    ...(view.spoken ? { spokenLabel: lead(view.spoken) } : {}),
    tone: TONE_TO_STATUS[view.tone],
  };
}

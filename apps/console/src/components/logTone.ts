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

import type { LineTone } from "@aep/progress-view";

/**
 * The ONE place a log line's semantic weight becomes an Oxygen palette token.
 *
 * `@aep/progress-view` decides what a line SAYS and how much it weighs, and
 * deliberately stops there: a TUI imports the same module, and leaking
 * `grey.400` into it would make this console's design system everyone's problem.
 * This is the console's half of that split.
 *
 * It lives beside LogSection rather than inside a feature because both feeds now
 * need it — the task log's v1 lines and the run feed's v2 events — and the run
 * feed reaching into the tasks feature for its colours was how one surface came
 * to own another's palette.
 *
 * Palette entries, never hex: the theme owns the actual colours.
 */
const TONE_COLORS: Record<LineTone, string> = {
  default: "grey.300",
  muted: "grey.400",
  info: "info.light",
  success: "success.light",
  warn: "warning.light",
  error: "error.light",
};

/** A formatted line's tone, as a colour the log surface can paint with. */
export function toneColor(tone: LineTone): string {
  return TONE_COLORS[tone];
}

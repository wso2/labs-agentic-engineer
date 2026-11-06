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

import { closingTag, tagWithSpace, type LineBlock, type Span } from "../../spec/model/ids";

// The technical decisions (specs/design/decisions.md) as the design skill
// writes them: one list entry per decision, "[data-model] Amounts are stored
// in cents. *assumed* (F1, F2)": the artifact it shapes, the decision, the
// tag while the user has not confirmed it, then the features it serves. A
// confirmed decision is the same line without the tag (ADR-0006). Unlike a
// requirement's, the tag is followed by the features, so the spec's
// parseLine does not find it.

/** The file the technical decisions are written in. */
export const DECISIONS_PATH = "specs/design/decisions.md";

/** After the tag: punctuation, and the features the decision serves, "(F1, F2)" or "(all)". */
const AFTER_TAG = /^[\s.,;:]*(\([^()]*\))?[\s.,;:]*$/;

/** The `*assumed*` tag closing a decision, as a span of its text; null when the decision is confirmed. */
export function decisionTag(line: LineBlock): Span | null {
  return line.kind === "listItem" ? closingTag(line.text, line.emphasis, "assumed", AFTER_TAG) : null;
}

/** How many decisions are still assumed. */
export function assumedCount(lines: readonly LineBlock[]): number {
  return lines.filter((l) => decisionTag(l) !== null).length;
}

/** A decision's text once kept: without its tag. */
export function keptText(line: LineBlock): string {
  const tag = decisionTag(line);
  if (!tag) return line.text;
  const span = tagWithSpace(line.text, tag);
  return line.text.slice(0, span.start) + line.text.slice(span.end);
}

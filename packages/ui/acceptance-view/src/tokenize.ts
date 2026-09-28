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
 * Emphasis for the parts of a step that carry its data.
 *
 * A Gherkin step is a sentence with literals embedded in it, and the literals
 * are what make it falsifiable — `"Bridge Cafe"`, `"12:30"`, `3`. The
 * authoring skill insists on them for exactly that reason ("a vague step is one
 * an agent will satisfy any way it likes"), so a reader scanning a spec is
 * looking for them.
 *
 * DELIBERATELY A REGEX, not a grammar. prismjs ships a complete Gherkin
 * language and is already in the tree — but only transitively, through an
 * Oxygen component nothing here uses, and its grammar is anchored on
 * `^Feature:` / `^Scenario:` / `^@tag`. It has no notion of a bare step line,
 * which is all this is ever handed: `parseFeature` has already taken the
 * keyword off. Promoting a transitive dependency to a direct one to get less
 * than ten lines of regex would be the wrong trade.
 */

export interface StepToken {
  readonly text: string;
  /** A quoted string, an `<outline parameter>`, or a bare number. */
  readonly emphasis: boolean;
}

/** Keeps the delimiters, because a capturing group in `split` returns them. */
const PARTS = /("[^"]*"|<[^>]*>|\b\d+\b)/g;

const EMPHASISED = /^(?:"|<|\d+$)/;

export function tokenizeStep(text: string): readonly StepToken[] {
  return text
    .split(PARTS)
    .filter((part) => part !== "")
    .map((part) => ({ text: part, emphasis: EMPHASISED.test(part) }));
}

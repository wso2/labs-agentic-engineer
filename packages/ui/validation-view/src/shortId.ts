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
 * Short display forms for the two id levels.
 *
 * The authoring convention (skills/validation-criteria/SKILL.md) fixes both
 * shapes: a requirement is `REQ-NNN`, and a criterion is
 * `AC-<req-number>-<letter>`. Inside a requirement's own card the
 * `AC-<req-number>-` half is already on the page, so a row that reprints it
 * spends its most-scanned column on repetition.
 *
 * Kept as pure functions rather than done inline in the view, because the
 * fallback below is the whole substance of them and it deserves its own tests.
 */

/**
 * The requirement's number, normalised past leading zeros — the shared half of
 * both ids, so both functions agree on what `REQ-001` and `AC-1-a` have in common.
 */
function requirementNumber(id: string): string | undefined {
  return /^REQ-0*(\d+)$/.exec(id)?.[1];
}

/**
 * `REQ-001` → `1`; anything else verbatim.
 *
 * The fallback is not defensive padding: `parse.ts` takes ids as arbitrary
 * strings, oracles can be hand-edited, and older ones predate the convention. An
 * id that does not match is shown in full rather than cut into a fragment that no
 * longer identifies anything.
 */
export function shortRequirementId(id: string): string {
  return requirementNumber(id) ?? id;
}

/**
 * `AC-001-a` → `a`, but only inside the requirement whose number it names.
 *
 * The number match is the point. A criterion filed under the wrong requirement is
 * a real thing an edited oracle can contain, and the number is the only part of
 * the id that says so — dropping it would render `AC-001-a` as a tidy `a` under
 * REQ-002 and hide the mismatch. So a criterion that does not belong keeps its
 * full id, where the reader can see why.
 */
export function shortCriterionId(
  criterionId: string,
  requirementId: string,
): string {
  const number = requirementNumber(requirementId);
  if (number === undefined) return criterionId;
  const parts = /^AC-0*(\d+)-(.+)$/.exec(criterionId);
  if (!parts || parts[1] !== number) return criterionId;
  return parts[2] ?? criterionId;
}

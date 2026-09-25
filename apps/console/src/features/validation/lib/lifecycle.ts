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
 * Is this version's validation still moving?
 *
 * `running` is a judging in flight and `awaiting-fix` is the repair before the
 * next one — ADR-0016 §7 names the two together as the lifecycle, which is what
 * the cancel control follows, and what picks the ledger's poll cadence.
 *
 * `none` is NOT live. It is "nothing has judged this version", the resting
 * state of every version a run never reached — so counting it here held the
 * ledger of any project holding one at its 5s cadence for ever. Four call
 * sites spelled this predicate out and the fifth added `none`; one function is
 * what stops the two readings existing at once.
 *
 * Nothing here says a state is FINAL: the sweep judges a version the moment a
 * build deploys, and a landed repair puts a settled row back in flight. That
 * is why the ledger slows to an idle cadence rather than stopping (queries.ts).
 */
export function validationIsLive(state: string): boolean {
  return state === "running" || state === "awaiting-fix";
}

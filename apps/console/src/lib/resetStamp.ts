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
 * A model provider's reset time as a reader plans around it: the time alone
 * today, the date as well when the plan resets another day (a weekly limit).
 * Shared by every sentence that tells a reader when a provider limit lifts —
 * a blocked build and a failed chat turn say it the same way. An unparseable
 * value is shown as given rather than as "Invalid Date".
 */
export function resetStamp(iso: string, now: Date): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const time: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString(undefined, time);
  return d.toLocaleString(undefined, { month: "short", day: "numeric", ...time });
}

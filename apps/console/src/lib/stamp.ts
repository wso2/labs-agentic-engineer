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

// When something happened, in the reader's own time zone: the stamps the
// build and validation surfaces share, and the reset time a model provider's
// limit names (a failed chat turn and a blocked build say it the same way).
// An unparseable value reads as nothing, or as given, never "Invalid Date".

/** "4 Oct, 10:12": when a build, task or attempt started or ended; "" when unknown. */
export function stamp(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}

/** A provider's reset time: the time alone today, the date as well when it resets another day. */
export function resetStamp(iso: string, now: Date): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const time: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString(undefined, time);
  return d.toLocaleString(undefined, { month: "short", day: "numeric", ...time });
}

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

import { projectKeys } from "../../projects/api/keys";

export const validationKeys = {
  // A validation artifact's content, keyed by (path, version): the dev spec
  // version advances on each merged validation run, so it doubles as the
  // cache-buster that invalidates a stale criteria/report read.
  file: (name: string, path: string, version: string) =>
    [...projectKeys.detail(name), "validation", "file", path, version] as const,
  // The validation ledger — one row per version.
  list: (name: string) => [...projectKeys.detail(name), "validations"] as const,
  // One version's validation history.
  detail: (name: string, tag: string) =>
    [...projectKeys.detail(name), "validations", tag] as const,
  // One attempt's report and the criteria it was judged against. Keyed by the
  // CYCLE and by WHICH COMMIT it is read at: an attempt in flight is answered
  // from the branch tip, the same attempt once merged from its own commit. Two
  // contents, so two entries — sharing one, the tip's empty answer is what the
  // settled read caches forever.
  snapshot: (name: string, tag: string, cycleId: string, settled: boolean) =>
    [
      ...projectKeys.detail(name),
      "validations",
      tag,
      "cycles",
      cycleId,
      settled ? "merged" : "head",
    ] as const,
};

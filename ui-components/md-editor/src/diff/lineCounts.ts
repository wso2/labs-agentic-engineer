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

import { diffLines } from 'diff';

export interface LineDiffCounts {
  added: number;
  removed: number;
}

/**
 * Line-level diff summary suitable for compact UI badges (e.g. a sidebar
 * "+12 / -3" chip). Identical inputs return `{added: 0, removed: 0}`. Uses
 * the same `diff` package the diff viewer already depends on, so there's
 * no extra runtime cost for callers that ship the viewer.
 */
export function countLineChanges(oldText: string, newText: string): LineDiffCounts {
  if (oldText === newText) return { added: 0, removed: 0 };
  const changes = diffLines(oldText, newText, { newlineIsToken: false });
  let added = 0;
  let removed = 0;
  for (const change of changes) {
    const count = change.count ?? change.value.split('\n').filter(Boolean).length;
    if (change.added) added += count;
    else if (change.removed) removed += count;
  }
  return { added, removed };
}

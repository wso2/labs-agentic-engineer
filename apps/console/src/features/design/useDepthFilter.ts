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

import { useCallback, useState } from "react";
import type { DepthFilter } from "./model/artifacts";

// The artifact list's filter is the user's, not the project's: it is kept in
// this browser and comes back on every project's design card. Storage can be
// missing or refused (a private window); the filter then starts at All.

const KEY = "aep:design:filter";

function isFilter(value: unknown): value is DepthFilter {
  return value === "all" || value === "business" || value === "technical";
}

function readFilter(): DepthFilter {
  try {
    const stored = localStorage.getItem(KEY);
    return isFilter(stored) ? stored : "all";
  } catch {
    return "all";
  }
}

export function useDepthFilter(): [DepthFilter, (next: DepthFilter) => void] {
  const [filter, setFilter] = useState<DepthFilter>(readFilter);
  const choose = useCallback((next: DepthFilter) => {
    setFilter(next);
    try {
      localStorage.setItem(KEY, next);
    } catch {
      /* not kept: it still applies until the page goes */
    }
  }, []);
  return [filter, choose];
}

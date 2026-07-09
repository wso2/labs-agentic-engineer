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

// Extends the project cache tree so project-level invalidation reaches the
// spec reads too.
export const specKeys = {
  files: (projectName: string) =>
    [...projectKeys.detail(projectName), "spec", "files"] as const,
  // Content is immutable per (path, sha): a sha change from the list poll
  // makes a new key, so stale content is never shown and unchanged files
  // never refetch (#113 decision 4).
  file: (projectName: string, path: string, sha: string) =>
    [...projectKeys.detail(projectName), "spec", "file", path, sha] as const,
};

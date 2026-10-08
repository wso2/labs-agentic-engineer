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

// A project's repository as people read it: `owner/repo`, from the clone URL
// without its host and `.git`. The overview links it to the repository's page.

export interface RepoLabel {
  /** `https://github.com/acme/acme-expenses` */
  href: string;
  /** `acme/acme-expenses` */
  short: string;
}

/** Null when the project has no repository yet or the URL is not one. */
export function repoLabel(repoUrl: string | undefined): RepoLabel | null {
  if (!repoUrl) return null;
  let url: URL;
  try {
    url = new URL(repoUrl);
  } catch {
    return null;
  }
  const path = url.pathname.replace(/^\/+|\/+$/g, "").replace(/\.git$/, "");
  if (!path) return null;
  return { href: `${url.origin}/${path}`, short: path };
}

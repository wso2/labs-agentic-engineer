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

/** Platform-owned project that backs AE Studio; never shown in the console. */
const SYSTEM_PROJECT_NAME = "ae-system";

export function isSystemProject(name: string): boolean {
  return name === SYSTEM_PROJECT_NAME;
}

/** Drops system projects from a list page; the pagination cursor is kept. */
export function withoutSystemProjects<
  P extends { name: string },
  T extends { items?: P[] | null },
>(page: T): T {
  return {
    ...page,
    items: page.items?.filter((p) => !isSystemProject(p.name)) ?? page.items,
  };
}

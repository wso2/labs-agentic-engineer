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

// Client-side name suggestion (issue #71 decision: heuristic, no backend
// call). Filler words carry no meaning for a project name.
const FILLER = new Set([
  "a", "an", "the", "and", "or", "of", "for", "to", "with", "in", "on",
  "that", "which", "my", "our", "me", "i", "want", "need", "like", "build",
  "make", "create", "app", "application", "website", "web", "site", "tool",
  "system", "platform", "service", "software", "simple", "new", "online",
  "some", "it", "is", "should", "be", "can", "will", "where", "users",
  "user", "lets", "let",
]);

const NAME_MAX = 40;
const NAME_PATTERN = /^[a-z]([a-z0-9-]*[a-z0-9])?$/;

export function isValidProjectName(name: string): boolean {
  return name.length <= NAME_MAX && NAME_PATTERN.test(name);
}

export function suggestProjectName(prompt: string): string {
  const words = prompt
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, " ")
    .split(/[\s-]+/)
    .filter((w) => w && !FILLER.has(w));
  const slug = words
    .slice(0, 3)
    .join("-")
    .replace(/^[^a-z]+/, "")
    .replace(/-+/g, "-")
    .replace(/-$/, "")
    .slice(0, NAME_MAX)
    .replace(/-$/, "");
  return isValidProjectName(slug) ? slug : "my-project";
}

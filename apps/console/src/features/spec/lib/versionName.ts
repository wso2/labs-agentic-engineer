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
 * What a version may be called (ADR-0030). The name the user types IS the tag,
 * the milestone title and the `/builds/<name>` address, so the field refuses
 * what a tag cannot hold rather than repairing it silently — a name the
 * platform rewrites is not the user's name.
 *
 * The rule is a deliberate SUBSET of git's own `check-ref-format`: the
 * characters below are safe in a ref, safe in a URL path segment and safe to
 * read back. Git would accept more (a slash, say), and the server is still the
 * authority — this is the field's own answer, given while the user types.
 */

/** The rule, as the field says it under the box. */
export const VERSION_NAME_HINT =
  "Letters, digits, dot, dash and underscore.";

const ALLOWED = /^[A-Za-z0-9._-]+$/;

/**
 * Why this name cannot be used, or null when it can.
 *
 * `taken` is the project's existing version names (the tags read). The check is
 * case-sensitive, because git refs are: `V2` and `v2` are two different tags,
 * and calling them a collision would refuse a name that works.
 */
export function versionNameError(
  name: string,
  taken: readonly string[],
): string | null {
  const value = name.trim();
  if (value === "") return "Name the version.";
  if (!ALLOWED.test(value)) return VERSION_NAME_HINT;
  // A leading dash reads as a flag to every command that later takes this name
  // as an argument; a leading dot hides the ref. Both are git's rules too.
  if (value.startsWith("-") || value.startsWith("."))
    return "Start with a letter or a digit.";
  if (value.endsWith(".") || value.endsWith(".lock"))
    return "Do not end with a dot or with .lock.";
  if (value.includes("..")) return "Do not use two dots in a row.";
  if (taken.includes(value)) return `A version named ${value} already exists.`;
  return null;
}

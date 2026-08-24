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

// Cross-references between spec documents (#579 follow-up).
//
// A PRD names its feature docs — depth lives in
// `specs/requirements/features/<slug>.md` and the PRD body stays lean — so the
// document is full of pointers to files sitting one click away in the same
// view. Written as ordinary markdown links they render as EXTERNAL links,
// `target="_blank"` and all (the shared schema's Link mark stamps that on
// parse), so following one opens a new browser tab at a path the console does
// not serve.
//
// Resolving is deliberately closed over the files the project HAS: a link to a
// feature doc nobody has written yet stays plain text rather than becoming a
// control that selects nothing.

/** A link target that is not a document reference at all. */
const EXTERNAL = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i;

/**
 * Whether an href addresses somewhere other than this repository — a scheme, a
 * protocol-relative host, or an in-page anchor.
 *
 * The click handler needs the distinction that `resolveSpecHref` erases: it
 * answers null both for `https://wso2.com` and for a repo path nobody has
 * written yet, and those two deserve opposite treatment on a click.
 */
export function isExternalHref(href: string): boolean {
  const target = href.trim();
  return target === "" || EXTERNAL.test(target);
}

/** Normalise `a/./b`, `a/../b` and repeated slashes to a plain repo path. */
function normalize(path: string): string {
  const out: string[] = [];
  for (const segment of path.split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") out.pop();
    else out.push(segment);
  }
  return out.join("/");
}

/**
 * The spec file a link points at, or null when it points somewhere else.
 *
 * `from` is the document holding the link, so a relative href resolves against
 * its directory the way it would in the repository — which is what makes
 * `[Receipts](features/receipts.md)` work from the PRD and
 * `[The PRD](../prd.md)` work back from a feature doc.
 */
export function resolveSpecHref(
  href: string,
  from: string,
  knownPaths: readonly string[],
): string | null {
  const target = href.trim();
  if (target === "" || EXTERNAL.test(target)) return null;
  // Strip a query or fragment — neither means anything to a file selection.
  const bare = target.split(/[?#]/)[0] ?? "";
  if (bare === "") return null;
  if (bare.startsWith("/")) {
    const rooted = normalize(bare);
    return knownPaths.includes(rooted) ? rooted : null;
  }
  // Markdown says relative-to-the-document, and that is tried first. But the
  // agent is told to write every spec path in full (`SPEC_PATHS_RULE`), so a
  // link's href is just as likely to be repo-rooted already — and since a path
  // can only ever match one known file, trying both resolves the ambiguity
  // rather than creating it.
  const dir = from.split("/").slice(0, -1).join("/");
  for (const candidate of [normalize(`${dir}/${bare}`), normalize(bare)]) {
    if (knownPaths.includes(candidate)) return candidate;
  }
  return null;
}

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

import type { SourceDocument, SpecFeature } from "../api/specModel";

// The spec's files and the keys the URL names them by: `?file=F2` is the
// Approvals file, `?file=product-wide` the product-wide page, a source
// document its own ID, and no key the product page.

export const PRODUCT_KEY = "prd";
export const PRODUCT_WIDE_KEY = "product-wide";

/**
 * Room paths are the repo paths, verbatim (`specs/requirements/prd.md`): the
 * collab room keys every file by it, and so do the agent and a turn's `target`.
 */
export const PRD_PATH = "specs/requirements/prd.md";
export const PRODUCT_WIDE_PATH = "specs/requirements/product-wide.md";

/** A markdown file of the spec, by its URL key and its room path. */
export interface MarkdownFile {
  key: string;
  path: string;
}

/** A markdown file by its URL key and the name the rail gives it. */
export interface NamedFile {
  key: string;
  label: string;
}

/** The files at these room paths, named as the rail names them, in rail order. */
export function namedFiles(features: Pick<SpecFeature, "id" | "name" | "path">[], paths: string[]): NamedFile[] {
  const wanted = new Set(paths);
  const name = (key: string) =>
    key === PRODUCT_KEY ? "Product" : key === PRODUCT_WIDE_KEY ? "Product-wide" : (features.find((f) => f.id === key)?.name ?? key);
  return markdownFiles(features)
    .filter((f) => wanted.has(f.path))
    .map((f) => ({ key: f.key, label: name(f.key) }));
}

/** The spec's markdown files, in rail order: product, features, product-wide. */
export function markdownFiles(features: Pick<SpecFeature, "id" | "path">[]): MarkdownFile[] {
  return [
    { key: PRODUCT_KEY, path: PRD_PATH },
    ...features.map((f) => ({ key: f.id, path: f.path })),
    { key: PRODUCT_WIDE_KEY, path: PRODUCT_WIDE_PATH },
  ];
}

export type OpenFile =
  | { kind: "product"; key: string; path: string }
  | { kind: "feature"; key: string; path: string; feature: SpecFeature }
  | { kind: "product-wide"; key: string; path: string }
  | { kind: "document"; key: string; document: SourceDocument };

/** The file a URL key opens. An unknown or missing key opens the product page. */
export function openFile(
  model: { features: SpecFeature[]; documents: SourceDocument[] },
  key: string | undefined,
): OpenFile {
  const feature = model.features.find((f) => f.id === key);
  if (feature) return { kind: "feature", key: feature.id, path: feature.path, feature };
  if (key === PRODUCT_WIDE_KEY) return { kind: "product-wide", key, path: PRODUCT_WIDE_PATH };
  const document = model.documents.find((d) => d.id === key);
  if (document) return { kind: "document", key: document.id, document };
  return { kind: "product", key: PRODUCT_KEY, path: PRD_PATH };
}

/**
 * A link's target path, resolved against the file it is written in, as a
 * relative link in the repo would be: `features/F2-approvals.md` from
 * `specs/requirements/prd.md` is `specs/requirements/features/F2-approvals.md`.
 */
export function resolveHref(fromPath: string, href: string): string | null {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("/") || href.startsWith("#")) return null;
  const parts = fromPath.split("/").slice(0, -1);
  for (const segment of href.split("#")[0]!.split("/")) {
    if (segment === "..") {
      if (parts.length === 0) return null;
      parts.pop();
    } else if (segment !== "." && segment !== "") {
      parts.push(segment);
    }
  }
  return parts.join("/");
}

/** The URL key of the markdown file a link in `fromPath` points at, if it is one of the spec's. */
export function fileKeyForHref(files: MarkdownFile[], fromPath: string, href: string): string | null {
  const path = resolveHref(fromPath, href);
  return files.find((f) => f.path === path)?.key ?? null;
}

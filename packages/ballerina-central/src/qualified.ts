/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Package coordinates, branded so the parser is the only way to obtain one.
 *
 * The error this prevents is the reader's single most common caller mistake:
 * passing `org/name:version` where `org/name` belongs. Both are strings, so
 * plain types cannot tell them apart and the mistake surfaces as a confusing
 * "package not found" from Central. Branding moves it to the type checker — a
 * raw `string` cannot reach a request builder at all.
 */

import { err, ok, type Result } from "./result.js";

declare const brand: unique symbol;
type Brand<T, B> = T & { readonly [brand]: B };

export type Org = Brand<string, "Org">;
export type PkgName = Brand<string, "PkgName">;
export type Version = Brand<string, "Version">;

export interface QualifiedName {
  readonly org: Org;
  readonly name: PkgName;
}

/** `org/name`, the form Central's URLs and this CLI's argv both use. */
export function formatQualifiedName(qualified: QualifiedName): string {
  return `${qualified.org}/${qualified.name}`;
}

const QUALIFIED_NAME = /^([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+)$/;
// Deliberately permissive: Central publishes versions this reader only ever
// echoes back, so the check exists to reject an argument that is obviously a
// package name or a shell mishap, not to enforce semver.
const VERSION = /^[A-Za-z0-9_.+-]+$/;

/**
 * `.` and `..` pass every pattern above and are legal path traversal.
 *
 * Nothing derived a filesystem path from these values until the docs cache did,
 * and a cache keyed `<root>/v1/docs/<org>/<name>/<version>.json` turns a `..`
 * that reaches a segment into a write outside its own root. The cache checks its
 * segments again before joining them — this is the outer of two independent
 * guards, kept here so the branded type itself cannot hold one.
 */
function isTraversal(segment: string): boolean {
  return segment === "." || segment === "..";
}

export function parseQualifiedName(input: string): Result<QualifiedName> {
  const match = QUALIFIED_NAME.exec(input.trim());
  if (!match || match.slice(1).some(isTraversal)) {
    return err({
      kind: "validation",
      message: `Invalid package name '${input}'. Expected 'org/name' (no version suffix).`,
      suggestion: "Drop any ':version' suffix and pass strictly 'org/name', e.g. 'ballerinax/github'.",
    });
  }
  // Both groups are guaranteed by the pattern; `noUncheckedIndexedAccess` cannot
  // know that, and asserting here is cheaper than threading undefined onward.
  const [, org, name] = match as unknown as [string, string, string];
  return ok({ org: org as Org, name: name as PkgName });
}

export function parseVersion(input: string): Result<Version> {
  const trimmed = input.trim();
  if (!VERSION.test(trimmed) || isTraversal(trimmed)) {
    return err({
      kind: "validation",
      message: `Invalid version '${input}'.`,
      suggestion: "Pass a published version such as '6.0.0', or omit it to resolve the latest.",
    });
  }
  return ok(trimmed as Version);
}

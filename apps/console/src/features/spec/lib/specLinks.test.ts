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

import { describe, expect, it } from "vitest";
import { resolveSpecHref } from "./specLinks";

const PRD = "specs/requirements/prd.md";
const RECEIPTS = "specs/requirements/features/receipts.md";
const FILES = [PRD, RECEIPTS, "specs/design/domain-model.md"];

const from = (href: string, doc = PRD) => resolveSpecHref(href, doc, FILES);

describe("resolveSpecHref", () => {
  it("resolves a feature doc named relative to the PRD", () => {
    expect(from("features/receipts.md")).toBe(RECEIPTS);
    expect(from("./features/receipts.md")).toBe(RECEIPTS);
  });

  it("resolves the full repo path, with or without a leading slash", () => {
    expect(from(RECEIPTS)).toBe(RECEIPTS);
    expect(from(`/${RECEIPTS}`)).toBe(RECEIPTS);
  });

  it("walks back out of a feature doc to its PRD", () => {
    expect(from("../prd.md", RECEIPTS)).toBe(PRD);
    expect(from("receipts.md", RECEIPTS)).toBe(RECEIPTS);
  });

  it("leaves a link to a file the project does not have alone", () => {
    expect(from("features/not-written-yet.md")).toBeNull();
  });

  it("leaves anything that is not a document reference alone", () => {
    expect(from("https://example.com/features/receipts.md")).toBeNull();
    expect(from("mailto:someone@example.com")).toBeNull();
    expect(from("//example.com/x.md")).toBeNull();
    expect(from("#open-questions")).toBeNull();
    expect(from("")).toBeNull();
    expect(from("   ")).toBeNull();
  });

  it("ignores a query or fragment hung off a real path", () => {
    expect(from("features/receipts.md#limits")).toBe(RECEIPTS);
    expect(from("features/receipts.md?v=2")).toBe(RECEIPTS);
  });

  it("cannot be walked out of the repo into a path that merely looks known", () => {
    expect(from("../../../specs/requirements/features/receipts.md")).toBe(RECEIPTS);
    expect(from("../design/domain-model.md")).toBe("specs/design/domain-model.md");
  });
});

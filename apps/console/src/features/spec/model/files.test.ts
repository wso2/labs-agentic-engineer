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
import type { SpecFeature } from "../api/specModel";
import { fileKeyForHref, markdownFiles, openFile, resolveHref } from "./files";

const approvals: SpecFeature = {
  id: "F2",
  name: "Approvals",
  path: "specs/requirements/features/F2-approvals.md",
  purpose: "",
  stage: "Interviewed",
};
const policy = { id: "tne-policy", title: "Policy.pdf", pages: 1, rows: [] };
const model = { features: [approvals], documents: [policy] };

describe("openFile", () => {
  it("opens a feature, product-wide, or a document by its key", () => {
    expect(openFile(model, "F2")).toMatchObject({ kind: "feature", key: "F2", path: approvals.path });
    expect(openFile(model, "product-wide")).toMatchObject({ kind: "product-wide", path: "specs/requirements/product-wide.md" });
    expect(openFile(model, "tne-policy")).toMatchObject({ kind: "document", document: policy });
  });

  it("opens the product page for no key or one the spec does not have", () => {
    expect(openFile(model, undefined)).toMatchObject({ kind: "product", key: "prd", path: "specs/requirements/prd.md" });
    expect(openFile(model, "F9")).toMatchObject({ kind: "product" });
  });
});

describe("links between files", () => {
  const files = markdownFiles([approvals]);

  it("resolves a relative link against the file it is written in", () => {
    expect(resolveHref("specs/requirements/prd.md", "features/F2-approvals.md")).toBe("specs/requirements/features/F2-approvals.md");
    expect(resolveHref("specs/requirements/features/F2-approvals.md", "../product-wide.md#p4")).toBe(
      "specs/requirements/product-wide.md",
    );
  });

  it("leaves web links, absolute paths and escapes above the root alone", () => {
    expect(resolveHref("specs/requirements/prd.md", "https://xero.com")).toBeNull();
    expect(resolveHref("specs/requirements/prd.md", "/etc/prd.md")).toBeNull();
    expect(resolveHref("prd.md", "../x.md")).toBeNull();
  });

  it("names the spec file a link opens", () => {
    expect(fileKeyForHref(files, "specs/requirements/prd.md", "features/F2-approvals.md")).toBe("F2");
    expect(fileKeyForHref(files, "specs/requirements/prd.md", "product-wide.md")).toBe("product-wide");
    expect(fileKeyForHref(files, "specs/requirements/prd.md", "features/F9-nothing.md")).toBeNull();
  });
});

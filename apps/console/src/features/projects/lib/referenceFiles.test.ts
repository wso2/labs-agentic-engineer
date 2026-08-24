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
import {
  MAX_REFERENCE_FILES,
  MAX_REFERENCE_FILE_BYTES,
  referenceTypeLabel,
  screenReferenceFiles,
} from "./referenceFiles";

function file(name: string, content: string | Uint8Array<ArrayBuffer>, type = ""): File {
  return new File([content], name, { type });
}

describe("screenReferenceFiles", () => {
  // The two groups the models actually read: PDF plus the four native image
  // media types, and text in whatever shape a brief or an API spec arrives in.
  it("accepts every natively-read binary type", () => {
    const names = ["spec.pdf", "shot.png", "a.jpg", "b.jpeg", "anim.gif", "ui.webp"];
    const { accepted, rejected } = screenReferenceFiles(
      [],
      names.map((n) => file(n, "x")),
    );
    expect(accepted.map((f) => f.name)).toEqual(names);
    expect(rejected).toEqual([]);
  });

  it("accepts the text formats a brief or an API spec arrives in", () => {
    const names = [
      "prd.md", "notes.txt", "rows.csv", "rows.tsv", "schema.json",
      "openapi.yaml", "config.yml", "feed.xml", "page.html", "doc.rst",
    ];
    const { accepted, rejected } = screenReferenceFiles(
      [],
      names.map((n) => file(n, "x")),
    );
    expect(accepted.map((f) => f.name)).toEqual(names);
    expect(rejected).toEqual([]);
  });

  it("rejects other extensions with a reason naming the accepted set", () => {
    const { accepted, rejected } = screenReferenceFiles(
      [],
      [file("spec.docx", "x")],
    );
    expect(accepted).toEqual([]);
    expect(rejected).toHaveLength(1);
    expect(rejected[0]?.name).toBe("spec.docx");
    // Office formats are out on purpose: the models don't read them natively,
    // so accepting one would store bytes no turn can use.
    expect(rejected[0]?.reason).toMatch(/\.pdf/);
    expect(rejected[0]?.reason).toMatch(/\.webp/);
  });

  it("rejects a file over the size cap", () => {
    const big = new Uint8Array(MAX_REFERENCE_FILE_BYTES + 1);
    const { accepted, rejected } = screenReferenceFiles(
      [],
      [file("big.pdf", big)],
    );
    expect(accepted).toEqual([]);
    expect(rejected[0]?.reason).toMatch(/5 MB/);
  });

  it("rejects files past the count cap, counting already-attached ones", () => {
    const existing = Array.from({ length: MAX_REFERENCE_FILES - 1 }, (_, i) =>
      file(`doc-${i}.md`, "x"),
    );
    const { accepted, rejected } = screenReferenceFiles(existing, [
      file("fits.md", "x"),
      file("overflow.md", "x"),
    ]);
    expect(accepted.map((f) => f.name)).toEqual(["fits.md"]);
    expect(rejected[0]?.name).toBe("overflow.md");
    expect(rejected[0]?.reason).toMatch(/10/);
  });

  it("rejects a duplicate of an already-attached file name", () => {
    const { accepted, rejected } = screenReferenceFiles(
      [file("prd.md", "old")],
      [file("prd.md", "new")],
    );
    expect(accepted).toEqual([]);
    expect(rejected[0]?.reason).toMatch(/[Aa]lready attached/);
  });

  it("accepts images (.png, .jpg, .jpeg) — mockups and screenshots are references too", () => {
    const { accepted, rejected } = screenReferenceFiles(
      [],
      [file("mockup.png", "x"), file("photo.jpg", "x"), file("scan.JPEG", "x")],
    );
    expect(accepted.map((f) => f.name)).toEqual(["mockup.png", "photo.jpg", "scan.JPEG"]);
    expect(rejected).toEqual([]);
  });

  // Two names, one stored path: the server would write the path twice and the
  // second document would silently replace the first.
  it("rejects a name that sanitizes onto an attached document's path", () => {
    const { accepted, rejected } = screenReferenceFiles(
      [file("prd.md", "old")],
      [file("PRD.md", "new")],
    );
    expect(accepted).toEqual([]);
    expect(rejected[0]?.name).toBe("PRD.md");
    expect(rejected[0]?.reason).toMatch(/prd\.md/);
  });

  it("rejects the second of two incoming names that sanitize to one path", () => {
    const { accepted, rejected } = screenReferenceFiles(
      [],
      [file("my notes.md", "a"), file("my-notes.md", "b")],
    );
    expect(accepted.map((f) => f.name)).toEqual(["my notes.md"]);
    expect(rejected[0]?.name).toBe("my-notes.md");
  });
});

describe("referenceTypeLabel", () => {
  it("badges a card with the upper-cased extension", () => {
    expect(referenceTypeLabel("Anjana Income Expense All Years USD Tax.pdf")).toBe("PDF");
    expect(referenceTypeLabel("prd.md")).toBe("MD");
    expect(referenceTypeLabel("mockup.JPEG")).toBe("JPEG");
  });

  it("reads the LAST dot, so a dotted stem does not become the badge", () => {
    expect(referenceTypeLabel("01f197b7fc20198885e567acc2d1f4ac.v2.pdf")).toBe("PDF");
  });
});


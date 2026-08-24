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
  MAX_ATTACHMENT_FILES,
  MAX_ATTACHMENT_FILE_BYTES,
  MAX_CHAT_ATTACHMENT_TOTAL_BYTES,
  screenChatAttachments,
} from "./chatAttachments";

/** A File of a given size without allocating the bytes. */
function fileOf(name: string, size = 16): File {
  const file = new File(["x"], name);
  Object.defineProperty(file, "size", { value: size });
  return file;
}

describe("screenChatAttachments", () => {
  it("accepts every type the models can read, native and text", () => {
    const names = [
      "mock.pdf", "shot.png", "a.jpg", "b.jpeg", "c.gif", "d.webp",
      "brief.md", "notes.txt", "rows.csv", "rows.tsv", "cfg.json",
      "api.yaml", "api.yml", "feed.xml", "page.html", "readme.rst",
    ];
    // Ten at a time — the count cap is a separate rule and would otherwise
    // swallow the type check this case is about.
    for (const name of names) {
      const { accepted, rejected } = screenChatAttachments([], [fileOf(name)]);
      expect(rejected, name).toEqual([]);
      expect(accepted.map((f) => f.name)).toEqual([name]);
    }
  });

  it("rejects Office formats the models cannot read natively", () => {
    const { accepted, rejected } = screenChatAttachments([], [fileOf("spec.docx")]);
    expect(accepted).toEqual([]);
    expect(rejected).toHaveLength(1);
    expect(rejected[0]?.reason).toContain("files are accepted");
  });

  it("rejects a file over the 5 MB per-file cap", () => {
    const { accepted, rejected } = screenChatAttachments(
      [],
      [fileOf("huge.pdf", MAX_ATTACHMENT_FILE_BYTES + 1)],
    );
    expect(accepted).toEqual([]);
    expect(rejected[0]?.reason).toBe("Larger than 5 MB");
  });

  it("rejects past the per-message count cap", () => {
    const attached = Array.from({ length: MAX_ATTACHMENT_FILES }, (_, i) => fileOf(`f${i}.md`));
    const { accepted, rejected } = screenChatAttachments(attached, [fileOf("one-more.md")]);
    expect(accepted).toEqual([]);
    expect(rejected[0]?.reason).toContain(`At most ${MAX_ATTACHMENT_FILES} files per message`);
  });

  it("rejects a duplicate name, because the agent dedupes attachments by name", () => {
    const { accepted, rejected } = screenChatAttachments([fileOf("brief.md")], [fileOf("brief.md")]);
    expect(accepted).toEqual([]);
    expect(rejected[0]?.reason).toBe("Already attached");
  });

  it("does NOT reject names that merely differ in case", () => {
    // The create view rejects this pair because both land on ONE repo path.
    // Chat attachments land on no path at all, so the rule does not apply and
    // borrowing it would refuse a legitimate selection.
    const { accepted, rejected } = screenChatAttachments([fileOf("PRD.md")], [fileOf("prd.md")]);
    expect(rejected).toEqual([]);
    expect(accepted.map((f) => f.name)).toEqual(["prd.md"]);
  });

  it("holds the TOTAL-bytes line that a per-file cap cannot", () => {
    // The defect this closes: ten 5 MB files each pass the per-file cap and
    // together are 50 MB — 3x the model's 20 MB encoded budget — so the agent
    // would silently drop the tail. Three 5 MB files already exceed 15 MB.
    const five = MAX_ATTACHMENT_FILE_BYTES;
    const { accepted, rejected } = screenChatAttachments([], [
      fileOf("a.pdf", five),
      fileOf("b.pdf", five),
      fileOf("c.pdf", five),
      fileOf("d.pdf", five),
    ]);
    expect(accepted.map((f) => f.name)).toEqual(["a.pdf", "b.pdf", "c.pdf"]);
    expect(rejected).toHaveLength(1);
    expect(rejected[0]?.name).toBe("d.pdf");
    expect(rejected[0]?.reason).toContain("total for one message");
  });

  it("names the remaining room in a total-budget rejection", () => {
    const { rejected } = screenChatAttachments(
      [fileOf("big.pdf", MAX_CHAT_ATTACHMENT_TOTAL_BYTES - 1024 * 1024)],
      [fileOf("next.pdf", 2 * 1024 * 1024)],
    );
    // "too big" is unactionable when the file itself is under the per-file cap.
    expect(rejected[0]?.reason).toContain("1 MB of room left");
  });

  it("never overstates the remaining room", () => {
    // Rounding made the notice contradict itself: with 4.998 MB left, refusing a
    // 5 MB file read "5 MB of room left" — telling the user it should have fitted.
    const five = MAX_ATTACHMENT_FILE_BYTES;
    const { rejected } = screenChatAttachments(
      [fileOf("a.pdf", five), fileOf("b.pdf", five), fileOf("small.md", 2048)],
      [fileOf("c.pdf", five)],
    );
    expect(rejected).toHaveLength(1);
    expect(rejected[0]?.reason).toContain("4.9 MB of room left");
    expect(rejected[0]?.reason).not.toContain("5 MB of room left");
  });

  it("is exactly the model's encoded budget: 15 MiB raw base64s to 20 MiB", () => {
    // The derivation the cap exists to restate — 4 bytes out per 3 in.
    const encoded = Math.ceil(MAX_CHAT_ATTACHMENT_TOTAL_BYTES / 3) * 4;
    expect(encoded).toBe(20 * 1024 * 1024);
  });

  it("blames the file's own property before the set's, and reports each refusal once", () => {
    const { accepted, rejected } = screenChatAttachments([], [
      fileOf("ok.md", 32),
      fileOf("wrong.docx", 32),
      fileOf("big.pdf", MAX_ATTACHMENT_FILE_BYTES + 1),
    ]);
    expect(accepted.map((f) => f.name)).toEqual(["ok.md"]);
    expect(rejected.map((r) => r.name)).toEqual(["wrong.docx", "big.pdf"]);
    // A wrong-type file is never blamed on a budget it also happened to exceed.
    expect(rejected[0]?.reason).toContain("files are accepted");
    expect(rejected[1]?.reason).toBe("Larger than 5 MB");
  });
});

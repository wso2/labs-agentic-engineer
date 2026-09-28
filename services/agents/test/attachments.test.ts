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
 * Attachments follow the model (`conversation/attachments.ts`): a PDF stays a
 * native document only where the connection reads PDFs natively, else it
 * becomes its text under the same file name; a scan (no text) and an image the
 * model cannot read are refused naming the file when the user just attached
 * them, and left out with the reason when they are reference documents.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { FilePart } from "ai";
import { AttachmentRefusedError, fitAttachments, fitReferences } from "../src/conversation/attachments.js";
import { minimalPdf } from "./pdf-fixture.js";

const FIRST_PARTY = { nativePdf: true, imageInput: "yes" as const };
const OLLAMA_NO_VISION = { nativePdf: false, imageInput: "no" as const };
const OLLAMA_VISION = { nativePdf: false, imageInput: "yes" as const };
const UNKNOWN_HOST = { nativePdf: false, imageInput: "unknown" as const };

const pdfPart = (filename: string, bytes: Buffer): FilePart => ({
  type: "file",
  mediaType: "application/pdf",
  data: bytes.toString("base64"),
  filename,
});
const imagePart = (filename: string): FilePart => ({ type: "file", mediaType: "image/png", data: "iVBORw0KGgo=", filename });
const textPart = (filename: string): FilePart => ({
  type: "file",
  mediaType: "text/plain",
  data: Buffer.from("notes").toString("base64"),
  filename,
});

test("a PDF stays a native document part where the connection reads PDFs natively", async () => {
  const part = pdfPart("brief.pdf", minimalPdf("Checkout brief"));
  assert.deepEqual(await fitAttachments([part], FIRST_PARTY), [part]);
});

test("off Anthropic's own API a PDF becomes a text part under the same file name", async () => {
  const [part] = await fitAttachments([pdfPart("brief.pdf", minimalPdf("Checkout brief for shoppers"))], OLLAMA_NO_VISION);
  assert.equal(part!.type, "file");
  assert.equal(part!.mediaType, "text/plain");
  assert.equal(part!.filename, "brief.pdf", "the name is the dedupe key and the journal's chip");
  assert.match(Buffer.from(part!.data as string, "base64").toString("utf8"), /Checkout brief for shoppers/);
});

test("a PDF with no extractable text is refused, naming the file", async () => {
  await assert.rejects(fitAttachments([pdfPart("scan.pdf", minimalPdf())], OLLAMA_VISION), (err: unknown) => {
    assert.ok(err instanceof AttachmentRefusedError);
    assert.equal(err.filename, "scan.pdf");
    assert.match(err.message, /^scan\.pdf: the PDF has no extractable text/);
    return true;
  });
});

test("bytes that are not a PDF are refused, naming the file", async () => {
  await assert.rejects(fitAttachments([pdfPart("broken.pdf", Buffer.from("%PDF-1.4 truncated"))], UNKNOWN_HOST), {
    name: "AttachmentRefusedError",
    message: "broken.pdf: the file could not be read as a PDF",
  });
});

test("an image is refused when the model reads no images, and sent when it does or when nobody knows", async () => {
  await assert.rejects(fitAttachments([imagePart("mockup.png")], OLLAMA_NO_VISION), (err: unknown) => {
    assert.ok(err instanceof AttachmentRefusedError);
    assert.equal(err.message, "mockup.png: the model on this connection does not read images");
    return true;
  });
  const image = imagePart("mockup.png");
  assert.deepEqual(await fitAttachments([image], OLLAMA_VISION), [image]);
  assert.deepEqual(await fitAttachments([image], UNKNOWN_HOST), [image], "unknown sends; the provider's own error names a refusal");
});

test("text parts pass unchanged, and order is kept", async () => {
  const notes = textPart("notes.txt");
  const out = await fitAttachments([notes, pdfPart("brief.pdf", minimalPdf("Brief"))], OLLAMA_VISION);
  assert.equal(out[0], notes);
  assert.equal(out[1]!.filename, "brief.pdf");
});

test("references the model cannot read are left out with the reason; the rest are fitted in order", async () => {
  const notes = textPart("notes.txt");
  const { parts, unreadable } = await fitReferences(
    [imagePart("flow.png"), notes, pdfPart("scan.pdf", minimalPdf()), pdfPart("brief.pdf", minimalPdf("Brief"))],
    OLLAMA_NO_VISION,
  );
  assert.equal(parts.length, 2);
  assert.equal(parts[0], notes);
  assert.equal(parts[1]!.filename, "brief.pdf");
  assert.equal(parts[1]!.mediaType, "text/plain", "a readable PDF still becomes its text");
  assert.deepEqual(unreadable.map((r) => r.filename), ["flow.png", "scan.pdf"]);
  assert.equal(unreadable[0]!.reason, "the model on this connection does not read images");
  assert.match(unreadable[1]!.reason, /^the PDF has no extractable text/);
});

test("an image reference is sent as is where the model reads images", async () => {
  const image = imagePart("flow.png");
  assert.deepEqual(await fitReferences([image], OLLAMA_VISION), { parts: [image], unreadable: [] });
});

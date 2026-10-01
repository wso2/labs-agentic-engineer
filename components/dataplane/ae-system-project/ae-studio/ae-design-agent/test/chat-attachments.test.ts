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

import { test } from "node:test";
import assert from "node:assert/strict";
import { isTurnAttachment, isTurnAttachmentsOrAbsent } from "@aep/agent-stream";
import {
  MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES,
  toAttachmentParts,
} from "../src/conversation/load-workspace.js";
import { projectDisplayHistory } from "../src/conversation/display-history.js";
import { attachmentsNote } from "../src/prompts/turn.js";
import type { Conversation } from "../src/store/conversation-store.js";

/** An attachment whose base64 payload is `encodedBytes` long. */
function attachment(name: string, mediaType = "application/pdf", encodedBytes = 8) {
  return { name, mediaType, data: "A".repeat(encodedBytes) };
}

/** Silence the best-effort skip warnings for one call. */
function quietly<T>(fn: () => T): T {
  const original = console.warn;
  console.warn = () => {};
  try {
    return fn();
  } finally {
    console.warn = original;
  }
}

function conversationWith(turns: Conversation["turns"], messages: Conversation["messages"]): Conversation {
  return {
    id: "c1",
    messages,
    turns,
    status: "done",
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

// --- toAttachmentParts -------------------------------------------------------

test("toAttachmentParts builds a native file part per attachment", () => {
  assert.deepEqual(
    toAttachmentParts([attachment("mockup.pdf"), attachment("shot.png", "image/png")]),
    [
      { type: "file", data: "AAAAAAAA", mediaType: "application/pdf", filename: "mockup.pdf" },
      { type: "file", data: "AAAAAAAA", mediaType: "image/png", filename: "shot.png" },
    ],
  );
});

test("toAttachmentParts keeps the filename, which is the dedupe key against history", () => {
  // run-conversation-turn drops a part whose filename the conversation already
  // holds; a part without one would re-send a 5 MB PDF on every turn.
  const [part] = toAttachmentParts([attachment("brief.md", "text/plain")]);
  assert.equal(part?.filename, "brief.md");
});

test("toAttachmentParts touches no filesystem — the bytes arrive inline", () => {
  // The whole difference from readReferenceAttachments: a reference is read from
  // the snapshot by path; an attachment is never stored anywhere, so a path-like
  // name is just a name and no I/O happens.
  const parts = toAttachmentParts([attachment("/nonexistent/absolute/path.pdf")]);
  assert.equal(parts.length, 1);
  assert.equal(parts[0]?.data, "AAAAAAAA");
});

test("toAttachmentParts returns [] for absent or empty input", () => {
  assert.deepEqual(toAttachmentParts(undefined), []);
  assert.deepEqual(toAttachmentParts([]), []);
});

test("toAttachmentParts skips a blank name rather than emitting a nameless part", () => {
  assert.deepEqual(toAttachmentParts([attachment("   ")]), []);
});

test("toAttachmentParts skips an attachment that does not fit the turn's budget", () => {
  const parts = quietly(() =>
    toAttachmentParts([
      attachment("huge.pdf", "application/pdf", MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES + 1),
    ]),
  );
  assert.deepEqual(parts, []);
});

test("toAttachmentParts takes in order until the budget runs out, without starving the tail", () => {
  const big = Math.floor(MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES * 0.9);
  const parts = quietly(() =>
    toAttachmentParts([
      attachment("first.pdf", "application/pdf", big),
      attachment("wont-fit.pdf", "application/pdf", big),
      // A small one behind an oversized one must still get its chance.
      attachment("small.md", "text/plain", 16),
    ]),
  );
  assert.deepEqual(
    parts.map((p) => p.filename),
    ["first.pdf", "small.md"],
  );
});

test("toAttachmentParts SHARES the budget with reference parts rather than doubling it", () => {
  // The ceiling belongs to the model request, not to either channel, so an
  // already-spent amount carries in.
  const spent = MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES - 4;
  assert.deepEqual(quietly(() => toAttachmentParts([attachment("a.pdf", "application/pdf", 8)], spent)), []);
  assert.equal(toAttachmentParts([attachment("a.pdf", "application/pdf", 4)], spent).length, 1);
});

// --- the wire guard ----------------------------------------------------------

test("isTurnAttachment accepts a well-formed attachment, empty data included", () => {
  assert.equal(isTurnAttachment({ name: "a.pdf", mediaType: "application/pdf", data: "AA" }), true);
  // A zero-byte file is still a file.
  assert.equal(isTurnAttachment({ name: "a.md", mediaType: "text/plain", data: "" }), true);
});

test("isTurnAttachment rejects a blank name or a missing media type", () => {
  assert.equal(isTurnAttachment({ mediaType: "text/plain", data: "AA" }), false);
  assert.equal(isTurnAttachment({ name: "  ", mediaType: "text/plain", data: "AA" }), false);
  assert.equal(isTurnAttachment({ name: "a.pdf", data: "AA" }), false);
});

test("isTurnAttachment rejects non-objects", () => {
  assert.equal(isTurnAttachment(null), false);
  assert.equal(isTurnAttachment("a.pdf"), false);
});

test("isTurnAttachmentsOrAbsent treats absent as valid and a malformed list as invalid", () => {
  assert.equal(isTurnAttachmentsOrAbsent(undefined), true);
  assert.equal(isTurnAttachmentsOrAbsent([]), true);
  assert.equal(isTurnAttachmentsOrAbsent([{ name: "a.pdf", mediaType: "text/plain", data: "" }]), true);
  assert.equal(isTurnAttachmentsOrAbsent([{ name: "a.pdf" }]), false);
  assert.equal(isTurnAttachmentsOrAbsent("nope"), false);
});

// --- the display projection --------------------------------------------------

test("projectDisplayHistory serves the journal's attachment names on the user row", () => {
  // Without this a reload shows the agent discussing a document that appears
  // nowhere in the thread: the projection replaces the row's content with the
  // journal text, so the names have to come from the journal too.
  const conv = conversationWith(
    [
      {
        turnId: "t1",
        text: "what is wrong here?",
        messageIndex: 0,
        createdAt: new Date(0),
        attachments: ["error.png", "rows.csv"],
      },
    ],
    [{ role: "user", content: "a composed model prompt, not for display" }],
  );
  assert.deepEqual(projectDisplayHistory(conv), [
    { role: "user", content: "what is wrong here?", attachments: ["error.png", "rows.csv"] },
  ]);
});

test("projectDisplayHistory omits the field for a turn that carried none", () => {
  const conv = conversationWith(
    [{ turnId: "t1", text: "hello", messageIndex: 0, createdAt: new Date(0) }],
    [{ role: "user", content: "composed" }],
  );
  assert.deepEqual(projectDisplayHistory(conv), [{ role: "user", content: "hello" }]);
});

// --- the prompt line ---------------------------------------------------------

test("attachmentsNote names the files so a bare \"this\" has an antecedent", () => {
  // The failure it fixes: the model CAN read the attached document, but "add
  // this as a separate form" gives `this` no antecedent in the text, so asking
  // for clarification is the honest response to an ambiguous instruction.
  const note = attachmentsNote(["2025-Motor Claim Form.pdf"]);
  assert.match(note, /attached this file to this message/);
  assert.match(note, /- 2025-Motor Claim Form\.pdf/);
  assert.match(note, /read it before asking about anything it already answers/);
});

test("attachmentsNote agrees in number for several files", () => {
  const note = attachmentsNote(["a.pdf", "b.csv"]);
  assert.match(note, /attached these files to this message/);
  assert.match(note, /read them before asking about anything they already answer:/);
  assert.match(note, /- a\.pdf\n- b\.csv/);
});

test("attachmentsNote is empty for none, so a turn without attachments is unchanged", () => {
  // Byte-identical prompts matter here: the prefix is the prompt cache key.
  assert.equal(attachmentsNote(undefined), "");
  assert.equal(attachmentsNote([]), "");
  assert.equal(attachmentsNote(["  ", ""]), "");
});

test("attachmentsNote is driven by the surviving PARTS, so a skipped file is never named", () => {
  // Naming a file the budget dropped would tell the model to read a document it
  // cannot see. The names come from the parts that survived, not from the request
  // — which is why run-conversation-turn derives them rather than taking a
  // parallel names field.
  const parts = quietly(() =>
    toAttachmentParts([
      attachment("kept.pdf", "application/pdf", 16),
      attachment("dropped.pdf", "application/pdf", MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES + 1),
    ]),
  );
  const note = attachmentsNote(parts.flatMap((p) => (p.filename ? [p.filename] : [])));
  assert.match(note, /- kept\.pdf/);
  assert.doesNotMatch(note, /dropped\.pdf/);
});

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
 * The file-type vocabulary shared by the console's TWO attachment channels.
 * Shared because a user who attached a PDF on the create view and a PDF in the
 * chat attached "the same kind of thing" — a picker that accepted one and
 * refused the other would be indefensible, and two drifting copies of the list
 * is how that happens.
 *
 * What is NOT shared is everything downstream of the type check, because the
 * channels differ in kind, not degree (ADR-0017 / ADR-0019):
 *
 *  - **Reference documents** (`features/projects/lib/referenceFiles.ts`) are
 *    project-scoped and STORED. They land on a repo-shaped path inside each
 *    turn's snapshot, so that module adds name sanitization and a
 *    collides-on-one-path rule that only make sense for a path.
 *  - **Chat attachments** (`features/agent-chat/lib/chatAttachments.ts`) are
 *    conversation-scoped and UNSTORED. They never land on a path, so they need
 *    no sanitization — but they do need a per-message TOTAL-bytes cap, because
 *    the whole set has to fit one model request.
 */

/**
 * Read NATIVELY by the model as file parts: PDF as a document block, and the
 * four image media types the Messages API accepts. There is no fifth image
 * type — this list is the API's, not a preference.
 */
const NATIVE_ATTACHMENT_EXTENSIONS = [
  "pdf", "png", "jpg", "jpeg", "gif", "webp",
] as const;

/**
 * Read as TEXT. Open-ended by nature — these are the formats a requirements
 * brief, an API spec or a data sample actually arrives in.
 *
 * Office formats (`.docx`/`.xlsx`/`.pptx`) are deliberately absent from both
 * groups: the models do not read them natively, so accepting one would carry
 * bytes no turn can use.
 */
const TEXT_ATTACHMENT_EXTENSIONS = [
  "md", "txt", "csv", "tsv", "json", "yaml", "yml", "xml", "html", "rst",
] as const;

/**
 * The `accept` attribute for a file input, derived from the two groups so the
 * picker, the screening and the hint text can never disagree about what is
 * allowed.
 */
export const ATTACHMENT_ACCEPT = [
  ...NATIVE_ATTACHMENT_EXTENSIONS,
  ...TEXT_ATTACHMENT_EXTENSIONS,
]
  .map((e) => `.${e}`)
  .join(",");

const ACCEPTED_EXTENSIONS = new Set<string>([
  ...NATIVE_ATTACHMENT_EXTENSIONS,
  ...TEXT_ATTACHMENT_EXTENSIONS,
]);

/** The per-file ceiling, on raw bytes. Both channels, and the server, agree. */
export const MAX_ATTACHMENT_FILE_BYTES = 5 * 1024 * 1024;

/** At most this many files in one upload / one message. */
export const MAX_ATTACHMENT_FILES = 10;

/** One file that was refused, with the reason to show verbatim. */
export interface RejectedFile {
  name: string;
  reason: string;
}

/** Lower-cased extension without the dot; "" when the name has no dot. */
function extensionOf(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot === -1 ? "" : name.slice(dot + 1).toLowerCase();
}

export function isAcceptedAttachment(name: string): boolean {
  return ACCEPTED_EXTENSIONS.has(extensionOf(name));
}

/**
 * The extensions a browser can render as a picture, so an attachment can show
 * ITSELF instead of its name.
 *
 * Deliberately NOT the same set as `NATIVE_ATTACHMENT_EXTENSIONS`: `.pdf` is
 * read natively by the model but an `<img>` cannot display it, so a PDF falls
 * back to its name. "Native to the model" and "previewable in the composer" are
 * different questions and only coincide for the four image types.
 */
const PREVIEWABLE_EXTENSIONS = new Set<string>(["png", "jpg", "jpeg", "gif", "webp"]);

export function isImageAttachment(name: string): boolean {
  return PREVIEWABLE_EXTENSIONS.has(extensionOf(name));
}

/**
 * The badge on an attachment card — the extension, upper-cased (PDF, MD, PNG).
 * Not the file's size: an oversized file never becomes a card, it becomes a
 * rejection notice, so size has nothing left to tell the user here.
 */
export function attachmentTypeLabel(name: string): string {
  return extensionOf(name).toUpperCase();
}

/**
 * The accepted set as a human list, for a rejection notice. Built at the point
 * of FAILURE rather than shown up front: it is 16 entries, which turns a hint
 * into a wall of text nobody reads.
 */
export function acceptedTypesSentence(): string {
  return ATTACHMENT_ACCEPT.split(",").join(", ");
}

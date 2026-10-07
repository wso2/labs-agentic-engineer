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

// Reference documents attached on New project: 5 MB per file, at most 10
// files, and only types the models can actually read. Copied from the
// console's `lib/attachments.ts` (the type vocabulary) and
// `features/projects/lib/referenceFiles.ts` (the screening), folded into one
// module because this app has no chat attachments yet to share the vocabulary
// with. When the chat grows attachments (N6), the type vocabulary moves out
// to be shared, as it is in the old console.
//
// The bytes go up as multipart to POST /projects/{name}/references and are
// never committed (ADR-0017): the server stores them off-git and overlays them
// into each turn's snapshot at a repo-shaped path, which is why the screening
// below cares about the path a name lands on.

/**
 * Read natively by the model as file parts: PDF as a document block, and the
 * four image media types the Messages API accepts.
 */
const NATIVE_EXTENSIONS = ["pdf", "png", "jpg", "jpeg", "gif", "webp"] as const;

/**
 * Read as text: the formats a requirements brief, an API spec or a data sample
 * arrives in.
 */
const TEXT_EXTENSIONS = [
  "md", "txt", "csv", "tsv", "json", "yaml", "yml", "xml", "html", "rst",
] as const;

/**
 * Word, Excel and PowerPoint: the models do not read them, so the server
 * converts each to markdown on upload (S5) and the turns read its words.
 */
const OFFICE_EXTENSIONS = ["docx", "xlsx", "pptx"] as const;

/** The file input's `accept`, so the picker and the screening agree. */
export const REFERENCE_ACCEPT = [...NATIVE_EXTENSIONS, ...TEXT_EXTENSIONS, ...OFFICE_EXTENSIONS]
  .map((e) => `.${e}`)
  .join(",");

const ACCEPTED_EXTENSIONS = new Set<string>([...NATIVE_EXTENSIONS, ...TEXT_EXTENSIONS, ...OFFICE_EXTENSIONS]);

/** The per-file ceiling on raw bytes; the server enforces the same. */
export const MAX_REFERENCE_FILE_BYTES = 5 * 1024 * 1024;

/** At most this many documents per project. */
export const MAX_REFERENCE_FILES = 10;

// Where the server overlays the stored documents inside each turn's snapshot.
// The console never writes this path; it only screens for collisions on it.
const REFERENCES_DIR = "specs/requirements/references";

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

// Screens a selection against what is already attached: per-file type and
// size, the total count cap, duplicate names, and names that differ but land on
// one repo path. Rejections carry the reason verbatim for the UI: one notice
// per file, never a silent drop.
export function screenReferenceFiles(
  attached: File[],
  incoming: File[],
): { accepted: File[]; rejected: RejectedFile[] } {
  const accepted: File[] = [];
  const rejected: RejectedFile[] = [];
  const names = new Set(attached.map((f) => f.name));
  const paths = new Set(attached.map((f) => referencePathOf(f.name)));
  let count = attached.length;
  for (const file of incoming) {
    const path = referencePathOf(file.name);
    if (!ACCEPTED_EXTENSIONS.has(extensionOf(file.name))) {
      rejected.push({
        name: file.name,
        // The accepted set is named here, at the point of failure, rather
        // than up front, where 16 entries turn a hint into a wall of text.
        reason: `Only ${REFERENCE_ACCEPT.split(",").join(", ")} files are accepted`,
      });
    } else if (file.size > MAX_REFERENCE_FILE_BYTES) {
      rejected.push({ name: file.name, reason: "Larger than 5 MB" });
    } else if (count >= MAX_REFERENCE_FILES) {
      rejected.push({
        name: file.name,
        reason: `At most ${MAX_REFERENCE_FILES} documents per project`,
      });
    } else if (names.has(file.name)) {
      rejected.push({ name: file.name, reason: "Already attached" });
    } else if (paths.has(path)) {
      // `PRD.md` and `prd.md` are two selections but one path: accepting both
      // would have the later one silently replace the earlier document.
      rejected.push({
        name: file.name,
        reason: `Conflicts with another document's name (${sanitizeName(file.name)})`,
      });
    } else {
      accepted.push(file);
      names.add(file.name);
      paths.add(path);
      count++;
    }
  }
  return { accepted, rejected };
}

// Repo-safe file name: the stem loses anything outside [a-z0-9._-]; the
// accepted extension survives as-is. Mirrors the server's canonical-path
// validation, which would 400 on spaces or traversal.
function sanitizeName(name: string): string {
  const dot = name.lastIndexOf(".");
  const stem = name
    .slice(0, dot)
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${stem || "document"}${name.slice(dot).toLowerCase()}`;
}

// The repo path a selection lands on.
function referencePathOf(name: string): string {
  return `${REFERENCES_DIR}/${sanitizeName(name)}`;
}

// The badge on an attachment: the extension, upper-cased (PDF, MD, PNG). Not
// the file's size: an oversized file never becomes an attachment, it becomes a
// rejection notice, so size has nothing left to tell the user.
export function referenceTypeLabel(name: string): string {
  return extensionOf(name).toUpperCase();
}

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

// Reference documents attached on the create view (#383). Grilling decisions:
// 5 MB per file, at most 10 files, and only types the models can actually read.
//
// The accepted set, the per-file cap, the count cap and the type badge now live
// in `src/lib/attachments.ts`, shared with the chat composer's attachments
// (#428) — a picker that took a PDF on one surface and refused it on the other
// would be indefensible. What stays HERE is what is specific to a reference:
// the repo-shaped path it lands on inside each turn's snapshot, and therefore
// name sanitization and the collides-on-one-path rule. Chat attachments never
// land on a path and need neither (ADR-0019).
//
// The bytes go up as multipart to POST /projects/{name}/references and are
// never committed (ADR-0017) — the server stores them off-git and overlays them
// into each turn's snapshot AT this path, which is why the screening below
// still cares about the repo-path a name lands on.

import {
  ATTACHMENT_ACCEPT,
  MAX_ATTACHMENT_FILES,
  MAX_ATTACHMENT_FILE_BYTES,
  acceptedTypesSentence,
  attachmentTypeLabel,
  isAcceptedAttachment,
  type RejectedFile,
} from "../../../lib/attachments";

// Where the server overlays the stored documents inside each turn's snapshot.
// The console never writes this path — it only screens for collisions on it.
const REFERENCES_DIR = "specs/requirements/references";

export const MAX_REFERENCE_FILE_BYTES = MAX_ATTACHMENT_FILE_BYTES;
export const MAX_REFERENCE_FILES = MAX_ATTACHMENT_FILES;
export const REFERENCE_ACCEPT = ATTACHMENT_ACCEPT;

export type { RejectedFile };

// Screens a selection against what is already attached: per-file type and
// size, the total count cap, duplicate names, and names that differ but land on
// one repo path. Rejections carry the reason verbatim for the UI — one notice
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
    if (!isAcceptedAttachment(file.name)) {
      rejected.push({
        name: file.name,
        reason: `Only ${acceptedTypesSentence()} files are accepted`,
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
      // would put two writes on it in the apply batch, and the later one would
      // silently replace the earlier document.
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

// The repo path a selection lands on. Screening and the apply batch below both
// go through here so they can never disagree about what a name becomes.
function referencePathOf(name: string): string {
  return `${REFERENCES_DIR}/${sanitizeName(name)}`;
}

// The badge shown on an attachment card — the extension, upper-cased (PDF, MD,
// PNG). Not the file's size: an oversized file never becomes a card, it becomes
// a rejection notice, so size has nothing left to tell the user here.
export function referenceTypeLabel(name: string): string {
  return attachmentTypeLabel(name);
}

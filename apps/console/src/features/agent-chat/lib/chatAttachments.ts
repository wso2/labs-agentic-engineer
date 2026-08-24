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

// Chat attachments (#428): files the user attaches to ONE message in the agent
// chat. They are conversation-scoped model content — the platform never stores
// them and never commits them (ADR-0019).
//
// The accepted types, the per-file cap and the count cap are shared with the
// create view's reference documents (`src/lib/attachments.ts`). Two things are
// different here, and both follow from "unstored, one message":
//
//  1. No name sanitization and no path-collision rule. A reference lands on a
//     repo-shaped path inside a snapshot; a chat attachment lands on no path at
//     all, so there is nothing to sanitize FOR.
//  2. A TOTAL-bytes cap, which references have no need of. See below.

import {
  ATTACHMENT_ACCEPT,
  MAX_ATTACHMENT_FILES,
  MAX_ATTACHMENT_FILE_BYTES,
  acceptedTypesSentence,
  isAcceptedAttachment,
  type RejectedFile,
} from "../../../lib/attachments";

export { ATTACHMENT_ACCEPT, MAX_ATTACHMENT_FILES, MAX_ATTACHMENT_FILE_BYTES };
export type { RejectedFile };

/**
 * The whole message's raw-byte ceiling, and the reason every other number here
 * exists.
 *
 * The agents service enforces a **20 MiB base64-ENCODED** attachment budget per
 * turn (`MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES`, #384), past which it warns and
 * SKIPS the rest. base64 is 4 bytes out per 3 in, so 20 MiB encoded is exactly
 * 15 MiB raw:
 *
 *     ceil(15 MiB / 3) * 4  ===  20 MiB     (15728640 / 3 * 4 === 20971520)
 *
 * This is the load-bearing cap, not the per-file one. A per-file limit alone
 * cannot hold the line: ten 5 MiB files each pass it and together are 50 MiB —
 * **3x** the budget — so the agent would silently drop the last seven. Issue
 * #428 originally specified exactly that. Screening the TOTAL here is what turns
 * a silent truncation into a visible rejection the user can act on.
 */
export const MAX_CHAT_ATTACHMENT_TOTAL_BYTES = 15 * 1024 * 1024;

/** Bytes already committed by `attached`. */
function totalBytes(files: File[]): number {
  return files.reduce((sum, f) => sum + f.size, 0);
}

/**
 * Screens a selection against what is already attached to this message: per-file
 * type and size, the count cap, duplicate names, and the message's total-bytes
 * budget. One notice per refused file, reason verbatim — never a silent drop.
 *
 * Order matters. Type and per-file size come first because they are properties
 * of the file alone and the user can act on them directly ("this one is too
 * big"). The set-scoped rules come after, so a wrong-type file is never blamed
 * on the budget it also happened to exceed.
 */
export function screenChatAttachments(
  attached: File[],
  incoming: File[],
): { accepted: File[]; rejected: RejectedFile[] } {
  const accepted: File[] = [];
  const rejected: RejectedFile[] = [];
  const names = new Set(attached.map((f) => f.name));
  let count = attached.length;
  let bytes = totalBytes(attached);

  for (const file of incoming) {
    if (!isAcceptedAttachment(file.name)) {
      rejected.push({
        name: file.name,
        reason: `Only ${acceptedTypesSentence()} files are accepted`,
      });
    } else if (file.size > MAX_ATTACHMENT_FILE_BYTES) {
      rejected.push({ name: file.name, reason: "Larger than 5 MB" });
    } else if (count >= MAX_ATTACHMENT_FILES) {
      rejected.push({
        name: file.name,
        reason: `At most ${MAX_ATTACHMENT_FILES} files per message`,
      });
    } else if (names.has(file.name)) {
      // Not cosmetic: the agents service dedupes attachments BY FILE NAME
      // against the conversation's history, so two different files sharing one
      // name in a single message would collapse to whichever arrived first.
      rejected.push({ name: file.name, reason: "Already attached" });
    } else if (bytes + file.size > MAX_CHAT_ATTACHMENT_TOTAL_BYTES) {
      // Worded for the SET, because that is what the user has to change — and
      // it names the remaining room, since "too big" is unactionable when the
      // file itself is under the per-file limit.
      rejected.push({
        name: file.name,
        reason:
          `Over the 15 MB total for one message — ` +
          `${formatMb(MAX_CHAT_ATTACHMENT_TOTAL_BYTES - bytes)} of room left`,
      });
    } else {
      accepted.push(file);
      names.add(file.name);
      count++;
      bytes += file.size;
    }
  }
  return { accepted, rejected };
}

/**
 * Megabytes to one decimal, trimmed ("2.0 MB" → "2 MB").
 *
 * FLOORED, not rounded. Rounding let the notice overstate the room and
 * contradict itself: with 4.998 MB left, refusing a 5 MB file read "Over the
 * 15 MB total — 5 MB of room left", which tells the user their file should have
 * fitted. Flooring reports 4.9 MB, so the number is always something a file of
 * that size would actually fit into.
 */
function formatMb(bytes: number): string {
  const mb = Math.max(0, bytes) / (1024 * 1024);
  const floored = Math.floor(mb * 10) / 10;
  return `${String(floored).replace(/\.0$/, "")} MB`;
}

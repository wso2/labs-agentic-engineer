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
 * DesignReview — the Design step's review state at `specs/design/review.json`:
 * per generated artifact, the user's comments on it and whether they ticked
 * it Checked. One file in the project's spec repo, no platform tables: the
 * console and the design agent both read and write it as an ordinary spec file
 * (the console through the room, the agent with its file tools), and git keeps
 * its history.
 *
 * An ARTIFACT is a file the Design step generates, keyed by its repo-relative
 * path under `specs/design/` or `specs/validation/acceptance/` (e.g.
 * `specs/design/domain-model.md`). A web-application's prototype is one
 * artifact made of two files; it is keyed by its manifest,
 * `specs/design/components/<component>/prototype.json`.
 *
 * CHECKED is a content hash, not a flag: `checkedHash` is the hash the
 * artifact had when the user ticked it, and the tick shows only while the
 * artifact still hashes to it — a changed artifact unticks itself. The hash is
 * the lowercase hex SHA-256 of the file's text, UTF-8 encoded, exactly as
 * stored (no normalisation), except the technical decisions
 * (`specs/design/decisions.md`): theirs is taken of the file as it reads
 * once every decision is kept, each decision's closing `*assumed*` tag and
 * the space before it removed, so confirming a decision leaves the file
 * checked (ADR-0052, amended). The prototype's is its revision hash, the kit's
 * `prototypeHash(manifest, source)` (`@wso2/prototype-kit/feedback`): the
 * SHA-256 of `prototype.json`'s text, a NUL, then `prototype.tsx`'s — the same
 * hash its full-screen review names a feedback batch by.
 *
 * A COMMENT moves `draft` → `sent` → `addressed`, and the user may reopen an
 * addressed one (`reopened`). Drafts and reopened comments wait in the file;
 * the agent acts on a comment only once it is sent (Address comments sends
 * every waiting one in one turn), and marks it `addressed` with a one-line
 * `reply`. The artifact's revision number itself is not stored here: it comes
 * from the design runs that changed the artifact (the turn-usage ledger);
 * `revision` on an addressed comment records the one that addressed it.
 *
 * A QUESTION is how the agent asks about an ambiguous comment instead of
 * guessing: it writes `question` on the comment and leaves it `sent`, and the
 * comment waits on the user (the Design card's "Answer 1 question"). The
 * user's `answer` puts it back in the next batch as `reopened`, like a
 * reopened reply; Address comments sends it, and the agent acts on it with
 * the answer, or asks again by writing a new `question` and removing the
 * `answer`. Question and answer stay on an addressed comment as its history.
 *
 * The Zod validator (`designReviewSchema` in `../design-review-schema.ts`) is
 * drift-guarded against these types.
 */

/** Where a comment is in the review loop. */
export type ReviewCommentStatus = "draft" | "sent" | "addressed" | "reopened";

/** Who wrote a comment: the platform user's id and display name. */
export interface ReviewAuthor {
  id: string;
  name: string;
}

/**
 * Where on a prototype a comment was made, from its full-screen review: the
 * kit's feedback request (`FeedbackRequest` in `@wso2/prototype-kit/feedback`)
 * without its text. `elementIds` is empty for the whole screen.
 */
export interface ReviewAnchor {
  screenId: string;
  flowId?: string;
  roleId: string;
  stateId: string;
  elementIds: string[];
}

/** One comment on an artifact. */
export interface ReviewComment {
  /** Unique within the file. */
  id: string;
  author: ReviewAuthor;
  text: string;
  /** ISO 8601. */
  createdAt: string;
  status: ReviewCommentStatus;
  /** ISO 8601: when Address comments sent it. */
  sentAt?: string;
  /** The agent's one-line answer; an addressed comment carries one. */
  reply?: string;
  /** The artifact revision that addressed it. */
  revision?: number;
  /** ISO 8601: when the agent addressed it. */
  addressedAt?: string;
  /** Prototype comments only: the screen and elements it is about. */
  anchor?: ReviewAnchor;
  /** The agent's question about an ambiguous comment, which it left `sent`; never on a draft. */
  question?: string;
  /** The user's answer to `question`; it reopens the comment for the next batch. */
  answer?: string;
}

/** One artifact's review state. */
export interface ReviewedArtifact {
  /** Repo-relative path, unique within the file. */
  path: string;
  /** The artifact's content hash when the user ticked it Checked; absent when unticked. */
  checkedHash?: string;
  comments?: ReviewComment[];
}

/**
 * The whole file. `artifacts` is a list keyed by `path` rather than a map, so
 * the published JSON Schema can describe each entry to the Go save gate (its
 * interpreter does not validate a map's values).
 */
export interface DesignReview {
  version: 1;
  artifacts: ReviewedArtifact[];
}

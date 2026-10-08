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

import type { CommentAnchor, DesignComment } from "../api/designModel";

// Design feedback is pinned comments. A comment is open until the agent
// addresses it (Address comments works through every open one in one turn);
// the agent's reply says what it changed, and the user then resolves it, or
// replies because it is not right yet, which opens it again. Unresolved
// comments never block a build.
//
// These are the rules the server keeps. MSW applies them (mocks/handlers),
// and the card offers only the moves they allow.

/** A new comment on an artifact element; its pin number follows the last one made. */
export function pinComment(
  comments: DesignComment[],
  input: { id: string; artifactId: string; anchor: CommentAnchor; text: string },
): DesignComment[] {
  const n = comments.reduce((max, c) => Math.max(max, c.n), 0) + 1;
  return [
    ...comments,
    { ...input, text: input.text.trim(), n, earlier: [], status: "open", reply: null, specLine: null },
  ];
}

/** The agent addressed an open comment: its reply, and the spec line it changed when the comment was a requirement. */
export function addressComment(comment: DesignComment, reply: string, specLine: string | null): DesignComment {
  if (comment.status !== "open") return comment;
  return { ...comment, status: "addressed", reply, specLine: specLine ?? comment.specLine };
}

export function canResolve(comment: Pick<DesignComment, "status">): boolean {
  return comment.status === "addressed";
}

export function canReply(comment: Pick<DesignComment, "status">): boolean {
  return comment.status === "addressed";
}

/** The user is done with an addressed comment. Null when it cannot be resolved now. */
export function resolveComment(comments: DesignComment[], id: string): DesignComment[] | null {
  const comment = comments.find((c) => c.id === id);
  if (!comment || !canResolve(comment)) return null;
  return comments.map((c) => (c.id === id ? { ...c, status: "resolved" as const } : c));
}

/**
 * The user replies to an addressed comment: it is not right yet. The reply
 * becomes what the comment asks, the earlier words are kept, and it is open
 * again for the next Address comments. Null when it cannot take a reply now.
 */
export function replyToComment(comments: DesignComment[], id: string, text: string): DesignComment[] | null {
  const comment = comments.find((c) => c.id === id);
  if (!comment || !canReply(comment) || !text.trim()) return null;
  return comments.map((c) =>
    c.id === id
      ? { ...c, text: text.trim(), earlier: [...c.earlier, c.text], status: "open" as const, reply: null }
      : c,
  );
}

export function openComments(comments: DesignComment[]): DesignComment[] {
  return comments.filter((c) => c.status === "open");
}

/** Comments on an artifact the user still has to deal with: open or addressed. */
export function unresolvedOn(comments: DesignComment[], artifactId: string): DesignComment[] {
  return comments.filter((c) => c.artifactId === artifactId && c.status !== "resolved");
}

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
 * The history filter: which stored parts of a conversation the current
 * connection can replay. The store keeps every turn's `ModelMessage[]`
 * verbatim, and some parts only make sense to the provider that produced them:
 * a provider-executed tool call (Anthropic's `web_search`) has no result
 * message another host would accept, and reasoning is signed or shaped for
 * the host that wrote it. Each turn's journal entry names the connection that
 * wrote it (`TurnJournalEntry.connection`), so the filter touches only the
 * turns another connection wrote and leaves a single-connection conversation
 * byte-identical, which keeps its prompt cache.
 *
 * One part depends on the model rather than on who wrote it: an image. A model
 * without vision refuses the whole request over one stored image (measured on
 * Ollama: `400 this model does not support image input`), so when the current
 * model reads no images every stored image, from any turn, is replaced by a
 * short text naming it.
 */

import type { ModelMessage } from "ai";
import { imageLeftOutOfHistory } from "../prompts/turn.js";
import type { ModelCapabilities } from "../shared/model.js";
import type { TurnJournalEntry } from "../store/conversation-store.js";

/** The connection a history is replayed to. */
export interface ReplayTarget {
  /** Its `connectionFingerprint`. */
  fingerprint: string;
  /** Whether its model reads images. Only `no` changes the history. */
  imageInput: ModelCapabilities["imageInput"];
}

/**
 * The fingerprint a turn counts as when no journal entry states one: journaled
 * before fingerprints existed, or sent with no journal at all. Every turn from
 * before connections ran on Anthropic's API; a journal-less turn after them
 * that ran elsewhere is at worst filtered on its own connection, which drops
 * only its reasoning. A fixed fact about stored data, so it does not follow
 * the service's current default connection.
 */
const UNSTAMPED_TURN_CONNECTION = "anthropic@api.anthropic.com";

/**
 * The messages to send `current`.
 *
 * When every turn's fingerprint equals `current.fingerprint`, and the model
 * reads images or none are stored, this returns `messages` ITSELF, so the
 * prompt is byte-identical. Otherwise it returns a filtered COPY: in the turns
 * another connection wrote — and only those — reasoning parts,
 * provider-executed tool calls and their results are dropped, and a message
 * left with no content goes with them; text, client tool calls and their
 * results stay. On a model that reads no images, every image part becomes a
 * text part naming the file. Deterministic, so after a switch the cleaned
 * prefix is the same on every turn and caches again from the second.
 *
 * A turn is the run of messages from one user message to the next: a turn
 * appends exactly one user message, first. The caller must not treat the
 * returned array as the transcript — it may be a copy.
 */
export function historyFor(
  messages: ModelMessage[],
  journal: readonly Pick<TurnJournalEntry, "messageIndex" | "connection">[],
  current: ReplayTarget,
): ModelMessage[] {
  const stamped = new Map<number, string>();
  for (const entry of journal) {
    if (entry.connection !== undefined) stamped.set(entry.messageIndex, entry.connection);
  }
  let turnConnection = UNSTAMPED_TURN_CONNECTION;
  const foreign = messages.map((m, index) => {
    if (m.role === "user") turnConnection = stamped.get(index) ?? UNSTAMPED_TURN_CONNECTION;
    return turnConnection !== current.fingerprint;
  });
  const withoutImages = current.imageInput === "no" && messages.some(hasImage);
  if (!foreign.includes(true) && !withoutImages) return messages;
  return messages.flatMap((m, index) => {
    const cleaned = foreign[index] ? replayableOn(m) : m;
    if (!cleaned) return [];
    return [withoutImages ? imagesAsText(cleaned) : cleaned];
  });
}

/** Whether `part` is an image: an image part, or a file part with an image media type. */
function isImagePart(part: { type: string; mediaType?: string }): boolean {
  return part.type === "image" || (part.type === "file" && (part.mediaType?.startsWith("image/") ?? false));
}

/**
 * Whether `m` carries an image a model could be sent. Only user messages carry
 * one: attachments ride the user message, and a tool's output is JSON here.
 */
function hasImage(m: ModelMessage): boolean {
  return m.role === "user" && typeof m.content !== "string" && m.content.some(isImagePart);
}

/** `m` with each image part replaced by a text part naming it; `m` itself when it has none. */
function imagesAsText(m: ModelMessage): ModelMessage {
  if (m.role !== "user" || !hasImage(m) || typeof m.content === "string") return m;
  return {
    ...m,
    content: m.content.map((part) =>
      isImagePart(part)
        ? { type: "text", text: imageLeftOutOfHistory(part.type === "file" ? part.filename : undefined) }
        : part,
    ),
  };
}

/**
 * `m` without the parts only its own connection can replay; undefined when
 * nothing is left. Only assistant messages carry such parts: a tool result in
 * an assistant message is always a provider-executed one (a client tool's
 * result is a `tool` message), and a `custom` part is provider-specific by
 * definition.
 */
function replayableOn(m: ModelMessage): ModelMessage | undefined {
  if (m.role !== "assistant" || typeof m.content === "string") return m;
  const content = m.content.filter((part) => {
    switch (part.type) {
      case "reasoning":
      case "reasoning-file":
      case "custom":
      case "tool-result":
        return false;
      case "tool-call":
        return part.providerExecuted !== true;
      default:
        return true;
    }
  });
  if (content.length === m.content.length) return m;
  return content.length > 0 ? { ...m, content } : undefined;
}

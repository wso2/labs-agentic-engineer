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
 * `historyFor` leaves a conversation written by the current connection exactly
 * as stored — the same array, so the prompt stays byte-identical (the cached
 * prefix holds). Turns another connection wrote lose only what that connection
 * alone can replay: reasoning, provider-executed tool calls and their results.
 * On a model that reads no images, every stored image becomes a text naming it.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { ModelMessage } from "ai";
import { historyFor, type ReplayTarget } from "../src/conversation/history-for.js";
import { imageLeftOutOfHistory } from "../src/prompts/turn.js";

const ANTHROPIC = "anthropic@api.anthropic.com";
const OLLAMA = "openai-compatible@ollama.com";

/** Replaying to `fingerprint`, on a model that reads images unless stated. */
const on = (fingerprint: string, imageInput: ReplayTarget["imageInput"] = "yes"): ReplayTarget => ({ fingerprint, imageInput });

const MESSAGES: ModelMessage[] = [
  { role: "user", content: "first" },
  { role: "assistant", content: [{ type: "text", text: "one" }] },
  { role: "user", content: "second" },
  { role: "assistant", content: [{ type: "text", text: "two" }] },
];

/** A turn on Anthropic's API as the store keeps it: signed reasoning, a server-run web_search, a client addFile. */
const ANTHROPIC_TURN: ModelMessage[] = [
  { role: "user", content: "write the PRD" },
  {
    role: "assistant",
    content: [
      { type: "reasoning", text: "check Stripe first", providerOptions: { anthropic: { signature: "c2ln" } } },
      { type: "tool-call", toolCallId: "srvtoolu_1", toolName: "web_search", input: { query: "Stripe" }, providerExecuted: true },
      {
        type: "tool-result",
        toolCallId: "srvtoolu_1",
        toolName: "web_search",
        output: { type: "json", value: [{ url: "https://docs.stripe.com", title: "Stripe", encryptedContent: "ZW5j", type: "web_search_result" }] },
      },
      { type: "text", text: "Stripe exists. Writing the PRD." },
      { type: "tool-call", toolCallId: "toolu_1", toolName: "addFile", input: { path: "specs/requirements/prd.md", content: "# PRD" } },
    ],
  },
  {
    role: "tool",
    content: [{ type: "tool-result", toolCallId: "toolu_1", toolName: "addFile", output: { type: "json", value: { ok: true } } }],
  },
  {
    role: "assistant",
    content: [
      { type: "reasoning", text: "done", providerOptions: { anthropic: { signature: "c2ln" } } },
      { type: "tool-call", toolCallId: "srvtoolu_2", toolName: "web_search", input: { query: "again" }, providerExecuted: true },
      { type: "tool-result", toolCallId: "srvtoolu_2", toolName: "web_search", output: { type: "json", value: [] } },
    ],
  },
];

test("every fingerprint matching returns the stored array itself", () => {
  const journal = [
    { messageIndex: 0, connection: "anthropic@ollama.com" },
    { messageIndex: 2, connection: "anthropic@ollama.com" },
  ];
  assert.equal(historyFor(MESSAGES, journal, on("anthropic@ollama.com")), MESSAGES);
});

test("a turn with no fingerprint counts as anthropic@api.anthropic.com", () => {
  const snapshot = structuredClone(MESSAGES);
  const journal = [{ messageIndex: 0 }, { messageIndex: 2, connection: ANTHROPIC }];
  const out = historyFor(MESSAGES, journal, on(ANTHROPIC));
  assert.equal(out, MESSAGES);
  assert.deepEqual(out, snapshot, "nothing was rewritten");
});

test("a conversation with no journal is replayed as stored on Anthropic's API", () => {
  assert.equal(historyFor(MESSAGES, [], on(ANTHROPIC)), MESSAGES);
});

test("an Anthropic turn replayed to another connection drops reasoning and server tool calls, keeps text and client tools", () => {
  const snapshot = structuredClone(ANTHROPIC_TURN);
  const out = historyFor(ANTHROPIC_TURN, [{ messageIndex: 0, connection: ANTHROPIC }], on(OLLAMA));
  assert.notEqual(out, ANTHROPIC_TURN, "a filtered history is a copy");
  assert.deepEqual(ANTHROPIC_TURN, snapshot, "the stored transcript is never rewritten");
  assert.deepEqual(out, [
    { role: "user", content: "write the PRD" },
    {
      role: "assistant",
      content: [
        { type: "text", text: "Stripe exists. Writing the PRD." },
        { type: "tool-call", toolCallId: "toolu_1", toolName: "addFile", input: { path: "specs/requirements/prd.md", content: "# PRD" } },
      ],
    },
    ANTHROPIC_TURN[2],
    // The last assistant message held nothing replayable, so it is gone.
  ]);
});

test("only the turns another connection wrote are filtered; the current connection's turns stay as stored", () => {
  const ollamaTurn: ModelMessage[] = [
    { role: "user", content: "and now?" },
    { role: "assistant", content: [{ type: "reasoning", text: "ollama's own reasoning" }, { type: "text", text: "ok" }] },
  ];
  const messages = [...ANTHROPIC_TURN, ...ollamaTurn];
  const journal = [
    { messageIndex: 0, connection: ANTHROPIC },
    { messageIndex: ANTHROPIC_TURN.length, connection: OLLAMA },
  ];
  const out = historyFor(messages, journal, on(OLLAMA));
  assert.equal(out[out.length - 1], ollamaTurn[1], "the current connection's message is the stored object, reasoning and all");
  assert.equal(out.length, 3 + ollamaTurn.length);
});

test("a journal-less turn in the middle counts as Anthropic's, not as the turn before it", () => {
  const messages: ModelMessage[] = [
    { role: "user", content: "on ollama" },
    { role: "assistant", content: [{ type: "reasoning", text: "r1" }, { type: "text", text: "a" }] },
    { role: "user", content: "no journal" },
    { role: "assistant", content: [{ type: "reasoning", text: "r2" }, { type: "text", text: "b" }] },
  ];
  const out = historyFor(messages, [{ messageIndex: 0, connection: OLLAMA }], on(OLLAMA));
  assert.equal(out[1], messages[1], "the journaled Ollama turn is untouched");
  assert.deepEqual(out[3], { role: "assistant", content: [{ type: "text", text: "b" }] });
});

test("the filter is deterministic: the same stored history cleans to the same bytes on every turn", () => {
  const journal = [{ messageIndex: 0, connection: ANTHROPIC }];
  assert.equal(JSON.stringify(historyFor(ANTHROPIC_TURN, journal, on(OLLAMA))), JSON.stringify(historyFor(ANTHROPIC_TURN, journal, on(OLLAMA))));
});

/** A turn whose user message carried a reference image and a chat screenshot, on Anthropic's API. */
const IMAGE_TURN: ModelMessage[] = [
  {
    role: "user",
    content: [
      { type: "text", text: "build this" },
      { type: "file", mediaType: "image/png", data: "iVBORw0KGgo=", filename: "specs/requirements/references/flow.png" },
      { type: "image", image: "iVBORw0KGgo=", mediaType: "image/png" },
      { type: "file", mediaType: "text/plain", data: "bm90ZXM=", filename: "notes.txt" },
    ],
  },
  { role: "assistant", content: [{ type: "text", text: "on it" }] },
];

test("on a model that reads no images, each stored image becomes a text naming it, from any turn", () => {
  const snapshot = structuredClone(IMAGE_TURN);
  // The same connection wrote the turn: only the images change.
  const out = historyFor(IMAGE_TURN, [{ messageIndex: 0, connection: OLLAMA }], on(OLLAMA, "no"));
  assert.deepEqual(IMAGE_TURN, snapshot, "the stored transcript is never rewritten");
  assert.match(imageLeftOutOfHistory("flow.png"), /flow\.png/, "the stand-in names the file");
  assert.deepEqual(out, [
    {
      role: "user",
      content: [
        { type: "text", text: "build this" },
        { type: "text", text: imageLeftOutOfHistory("specs/requirements/references/flow.png") },
        { type: "text", text: imageLeftOutOfHistory(undefined) },
        IMAGE_TURN[0]!.content[3],
      ],
    },
    IMAGE_TURN[1],
  ]);
});

test("images stay when the model reads them or nobody knows, so the stored array is returned itself", () => {
  const journal = [{ messageIndex: 0, connection: OLLAMA }];
  assert.equal(historyFor(IMAGE_TURN, journal, on(OLLAMA, "yes")), IMAGE_TURN);
  assert.equal(historyFor(IMAGE_TURN, journal, on(OLLAMA, "unknown")), IMAGE_TURN);
  assert.equal(historyFor(MESSAGES, [], on(ANTHROPIC, "no")), MESSAGES, "no image stored, nothing to replace");
});

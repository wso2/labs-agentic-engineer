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
 * `projectDisplayHistory` (#463): the get-conversation read serves the turn
 * journal's raw client-sent text for user rows — never the composed model
 * prompt — paired by each entry's stated messageIndex, so a journal-less turn
 * anywhere in the history never shifts another turn's pairing.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { projectDisplayHistory } from "../src/conversation/display-history.js";
import type { Conversation, TurnJournalEntry } from "../src/store/conversation-store.js";

const PROMPT_1 = "The following skill guidance is ALREADY LOADED…\n\n## Skill: start\n\nExisting files:…\n\nInstruction: /start";
const PROMPT_2 = "Existing files:…\n\nInstruction: Answers: …";
const AUTHOR = { id: "sarah@example.com", displayName: "Sarah Perera" };

function entry(messageIndex: number, text: string, author?: TurnJournalEntry["author"]): TurnJournalEntry {
  return {
    turnId: `t${messageIndex}`,
    text,
    ...(author ? { author } : {}),
    messageIndex,
    createdAt: new Date("2026-01-01T00:00:00Z"),
  };
}

function conv(messages: Conversation["messages"], turns: TurnJournalEntry[]): Conversation {
  return {
    id: "c1",
    messages,
    turns,
    status: "done",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
  };
}

test("user rows carry the journal text + author object; assistant rows pass through", () => {
  const out = projectDisplayHistory(
    conv(
      [
        { role: "user", content: PROMPT_1 },
        { role: "assistant", content: "made the PRD" },
      ],
      [entry(0, "/start", AUTHOR)],
    ),
  );
  assert.deepEqual(out[0], { role: "user", content: "/start", author: AUTHOR });
  assert.deepEqual(out[1], { role: "assistant", content: "made the PRD" });
});

test("pre-journal user rows fall back to their raw content", () => {
  const out = projectDisplayHistory(
    conv(
      [
        { role: "user", content: PROMPT_1 }, // legacy turn: no entry
        { role: "assistant", content: "a" },
        { role: "user", content: PROMPT_2 }, // journaled turn at index 2
        { role: "assistant", content: "b" },
      ],
      [entry(2, "Answers: lunch at noon")],
    ),
  );
  assert.equal((out[0] as { content: string }).content, PROMPT_1, "legacy turn stays raw");
  assert.deepEqual(out[2], { role: "user", content: "Answers: lunch at noon" });
});

// A journal-less turn AFTER journaled ones (older caller mid-rolling-deploy)
// must not shift earlier turns' pairing — the review's interleaving scenario.
test("a journal-less turn between journaled turns shifts nothing", () => {
  const out = projectDisplayHistory(
    conv(
      [
        { role: "user", content: PROMPT_1 }, // journaled at index 0
        { role: "assistant", content: "a" },
        { role: "user", content: PROMPT_2 }, // journal-less (old replica)
        { role: "assistant", content: "b" },
        { role: "user", content: "raw 3" }, // journaled at index 4
      ],
      [entry(0, "/start", AUTHOR), entry(4, "add a returns policy")],
    ),
  );
  assert.deepEqual(out[0], { role: "user", content: "/start", author: AUTHOR });
  assert.equal((out[2] as { content: string }).content, PROMPT_2, "un-journaled turn stays raw, unshifted");
  assert.deepEqual(out[4], { role: "user", content: "add a returns policy" });
});

test("a journal-less conversation projects byte-identically", () => {
  const messages: Conversation["messages"] = [
    { role: "user", content: PROMPT_1 },
    { role: "assistant", content: "a" },
  ];
  assert.deepEqual(projectDisplayHistory(conv(messages, [])), messages);
});

test("author is omitted, not empty, when the journal has none", () => {
  const out = projectDisplayHistory(conv([{ role: "user", content: PROMPT_1 }], [entry(0, "hi")]));
  assert.deepEqual(out[0], { role: "user", content: "hi" });
  assert.ok(!("author" in (out[0] as object)));
});

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

import { describe, expect, it } from "vitest";
import { mapConversationMessage, startTurnBody } from "./turns";

describe("startTurnBody", () => {
  it("includes collab: true for a spec-room turn", () => {
    expect(startTurnBody("hello", true)).toEqual({ instruction: "hello", collab: true });
  });

  it("omits collab for Marketplace register chat", () => {
    expect(startTurnBody("/register-external-resource github", false)).toEqual({
      instruction: "/register-external-resource github",
    });
  });
});

describe("mapConversationMessage", () => {
  it("keeps an author present on the payload", () => {
    expect(
      mapConversationMessage({
        role: "user",
        content: "hi",
        author: { id: "u-sarah", displayName: "Sarah Perera" },
      }),
    ).toEqual({
      role: "user",
      content: "hi",
      author: { id: "u-sarah", displayName: "Sarah Perera" },
    });
  });

  it("omits author when the payload has none", () => {
    expect(mapConversationMessage({ role: "assistant", content: "hi" })).toEqual({
      role: "assistant",
      content: "hi",
    });
  });

  it("falls back to a `user` field with a `name` property", () => {
    expect(
      mapConversationMessage({ role: "user", content: "hi", user: { id: "u-1", name: "Ann" } }),
    ).toEqual({ role: "user", content: "hi", author: { id: "u-1", displayName: "Ann" } });
  });

  it("drops a malformed author instead of throwing", () => {
    expect(
      mapConversationMessage({ role: "user", content: "hi", author: { id: 42 } }),
    ).toEqual({ role: "user", content: "hi" });
  });

  it("carries attachment names off the journal (#428)", () => {
    expect(
      mapConversationMessage({
        role: "user",
        content: "what is wrong here?",
        attachments: ["error.png", "rows.csv"],
      }),
    ).toEqual({
      role: "user",
      content: "what is wrong here?",
      attachments: ["error.png", "rows.csv"],
    });
  });

  it("omits attachments entirely when the payload has none", () => {
    // A message without attachments must keep the exact row shape it had
    // before the feature existed — not gain an empty array.
    expect(mapConversationMessage({ role: "user", content: "hi" })).toEqual({
      role: "user",
      content: "hi",
    });
  });

  it("drops malformed attachment entries rather than rendering blank chips", () => {
    // Untyped extension field in the contract, so this is untrusted input.
    expect(
      mapConversationMessage({
        role: "user",
        content: "hi",
        attachments: ["ok.pdf", 42, "", "   ", null],
      }),
    ).toEqual({ role: "user", content: "hi", attachments: ["ok.pdf"] });
    expect(
      mapConversationMessage({ role: "user", content: "hi", attachments: "nope" }),
    ).toEqual({ role: "user", content: "hi" });
  });

  it("returns null for a non-object entry", () => {
    expect(mapConversationMessage("nope")).toBeNull();
    expect(mapConversationMessage(null)).toBeNull();
  });

  it("returns null when role is missing or not a string", () => {
    expect(mapConversationMessage({ content: "hi" })).toBeNull();
  });
});

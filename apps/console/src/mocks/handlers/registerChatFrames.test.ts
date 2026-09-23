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

import { REGISTER_EXTERNAL_RESOURCE_COMMAND } from "@aep/contracts/commands";
import { describe, expect, it, vi } from "vitest";
import { MARKETPLACE_CHAT_PROJECT } from "../../features/marketplace/constants";

// Same literals as @aep/agent-stream. Vitest cannot load that barrel here
// (dist pulls @aep/excalidraw-dsl, which has no dist in this worktree).
const ANSWER_PREFIX = 'Answer to "';
vi.mock("@aep/agent-stream", () => ({
  ANSWER_PREFIX: 'Answer to "',
  ANSWERS_PREFIX: "Answers:",
}));

import { registerChatFrames } from "./registerChatFrames";

function framesJson(frames: unknown[] | null): string {
  return JSON.stringify(frames);
}

describe("registerChatFrames", () => {
  it("asks a question and omits draftExternalResource for an unsure register instruction", () => {
    const frames = registerChatFrames(`${REGISTER_EXTERNAL_RESOURCE_COMMAND} an API`);
    const json = framesJson(frames);
    expect(json).toMatch(/ask_questions?/);
    expect(json).not.toContain("draftExternalResource");
    expect(json).not.toContain("envValues");
  });

  it("emits a Stripe-like draft without envValues for a sure register instruction", () => {
    const frames = registerChatFrames(
      `${REGISTER_EXTERNAL_RESOURCE_COMMAND} Register Stripe`,
    );
    const json = framesJson(frames);
    expect(json).toContain("draftExternalResource");
    expect(json).not.toContain("envValues");
  });

  it("emits the sure draft on a marketplace answer turn", () => {
    const frames = registerChatFrames(
      `${ANSWER_PREFIX}What should this resource be called?": stripe`,
      MARKETPLACE_CHAT_PROJECT,
    );
    const json = framesJson(frames);
    expect(json).toContain("draftExternalResource");
    expect(json).not.toContain("envValues");
  });

  it("does not emit a register draft on a real-project answer turn", () => {
    const frames = registerChatFrames(
      `${ANSWER_PREFIX}Who is the primary user of this app?": Individual consumers`,
      "todo-app",
    );
    expect(frames).toBeNull();
  });
});

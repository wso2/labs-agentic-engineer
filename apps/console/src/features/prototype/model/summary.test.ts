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
import { parseManifestJson } from "@wso2/prototype-kit/manifest";
import { SAMPLE_MANIFEST } from "../../../mocks/fixtures/prototype";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";
import { feedbackSummary } from "./summary";

const parsed = parseManifestJson(SAMPLE_MANIFEST);
const manifest = parsed.ok ? parsed.manifest : null;

const feedback = (...requests: PrototypeFeedback["requests"]): PrototypeFeedback => ({
  prototypeHash: "a".repeat(64),
  component: "expense-web",
  requests,
});
const onPending = { screenId: "screen.pending", roleId: "manager", stateId: "state.default" };

describe("feedbackSummary", () => {
  it("names the screen, role, state, elements and text of each request, numbered", () => {
    const fb = feedback(
      { ...onPending, elementIds: ["btn.reject", "btn.approve"], text: "Put Approve on the right" },
      { ...onPending, stateId: "state.empty", elementIds: [], text: "Say who to ask" },
    );
    expect(feedbackSummary(fb, manifest).split("\n")).toEqual([
      "Feedback on the Acme Expenses prototype (2 comments)",
      "1. Pending approvals (Manager, Default) — btn.reject, btn.approve: Put Approve on the right",
      "2. Pending approvals (Manager, Nothing to show) — whole screen: Say who to ask",
    ]);
  });

  it("falls back to ids for names the manifest lacks", () => {
    const fb = feedback({ ...onPending, elementIds: ["btn.reject"], text: "Red" });
    expect(feedbackSummary(fb, null)).toBe(
      "Feedback on the expense-web prototype (1 comment)\n1. screen.pending (manager, state.default) — btn.reject: Red",
    );
  });

  it("clips a long request, so the chat row stays a summary", () => {
    const line = feedbackSummary(feedback({ ...onPending, elementIds: [], text: "x".repeat(900) }), manifest).split("\n")[1]!;
    expect(line.endsWith("…")).toBe(true);
    expect(line.length).toBeLessThan(400);
  });
});

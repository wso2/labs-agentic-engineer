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

// @vitest-environment jsdom

// The spec stage's actions. The stage is derived from GIT, which cannot see an
// interview: no requirements file exists until the agent writes one, so the old
// card invited "Generate spec" for the entire duration of the interview it had
// already started — and a second click injected a `/start` that the start skill
// read as the user's skip valve.
//
// Two signals now cover that blindness, and they know different things. The
// local chat log (`engaged`) only exists once the panel has mounted; the
// server's `spec.agent` (#562) is what a user who never opened the chat sees —
// which, since the platform fires the kickoff itself, is the ordinary case.

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { components } from "../../../generated/aep-api";
import {
  addMessage,
  chatKeyFor,
  consumePendingSeed,
  peekPendingSeed,
  replaceMessages,
} from "../../agent-chat/chatStore";
import { OverviewPipeline } from "./OverviewPipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ user: { email: "me@x.com", name: "Me" }, orgHandle: ORG }),
}));

const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mockNavigate,
}));

const QUESTIONS: AskQuestionInput[] = [
  { question: "Who signs in?", options: [{ label: "Anyone" }] },
];

function status(spec: Partial<ProjectStatus["spec"]>): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: { exists: false, version: "", dirty: false, design: false, agent: "", ...spec },
    build: { version: "", status: "idle" },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
    },
  };
}

function renderPipeline(spec: Partial<ProjectStatus["spec"]> = {}) {
  return render(<OverviewPipeline projectName={PROJECT} status={status(spec)} />);
}

describe("OverviewPipeline — the spec card", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    consumePendingSeed(KEY);
  });

  // The caption used to be three captions, walked in order during a single
  // kickoff with no input from the user: Generate spec → Open spec → Continue
  // spec. Each came from a different signal that moved on its own. A control
  // that renames itself while you read it cannot be learned, and the
  // destination never actually varied.
  it.each([
    ["nothing started", {}, false],
    ["kickoff running", { agent: "working" }, false],
    ["question waiting", {}, true],
    ["kickoff failed", { agent: "failed" }, false],
    ["published spec", { exists: true, version: "v2" }, false],
  ])("says Open spec and nothing else — %s", (_name, spec, withQuestion) => {
    if (withQuestion) {
      addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    }
    renderPipeline(spec);

    expect(screen.getByRole("button", { name: "Open spec" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Continue spec/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Try again/ })).not.toBeInTheDocument();
  });

  it("navigates to the spec and never sends", () => {
    renderPipeline();

    fireEvent.click(screen.getByRole("button", { name: "Open spec" }));
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
    });
    // Starting moved to the spec view's empty state — this card is a
    // destination in every state now, never a send.
    expect(peekPendingSeed(KEY)).toBeNull();
  });

  // The line is the part that moves, and it always says something. It used to
  // blank itself the moment the agent asked a question, because a turn that
  // ends ON one has written no PRD and reads server-side as "never ran".
  it.each([
    ["nothing started", {}, false, "Nothing written yet."],
    ["kickoff running", { agent: "working" }, false, "The agent is writing your requirements."],
    ["question waiting", {}, true, "The agent has questions for you."],
    [
      "kickoff failed",
      { agent: "failed" },
      false,
      "The agent couldn't start — open the spec to try again.",
    ],
  ])("never blanks the line — %s", (_name, spec, withQuestion, line) => {
    if (withQuestion) {
      addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    }
    renderPipeline(spec);

    expect(screen.getByText(line as string)).toBeInTheDocument();
  });

  // The version is a separate fact from what is happening right now.
  it("keeps the version while an amendment interview is open", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline({ exists: true, version: "v2" });

    expect(screen.getByText("v2")).toBeInTheDocument();
    expect(screen.getByText("The agent has questions for you.")).toBeInTheDocument();
  });
});

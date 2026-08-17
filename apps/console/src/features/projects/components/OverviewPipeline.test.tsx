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

// The spec stage speaks for the agent through a new project's first run (#485),
// and its CTA is pure navigation. Git cannot see an interview — no requirements
// file exists until the agent writes one — so the card reads the backend's
// kickoff report joined with the live chat log, and it starts nothing: the
// console composing a second `/start` is what produced "an agent turn is
// already running for this project" for real users.

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { components } from "../../../generated/aep-api";
import { addMessage, chatKeyFor, replaceMessages } from "../../agent-chat/chatStore";
import { OverviewPipeline } from "./OverviewPipeline";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type SpecStage = ProjectStatus["spec"];

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

const mockRetry = vi.fn();
vi.mock("../api/queries", () => ({
  useRetrySpecKickoff: () => ({
    mutate: mockRetry,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

const QUESTIONS: AskQuestionInput[] = [
  { question: "Who signs in?", options: [{ label: "Anyone" }] },
  { question: "How do they pay?", options: [{ label: "Card" }] },
];

const NO_KICKOFF: SpecStage["kickoff"] = { status: "none", reason: "" };

function status(spec: Partial<SpecStage>): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: {
      exists: false,
      version: "",
      dirty: false,
      design: false,
      kickoff: NO_KICKOFF,
      ...spec,
    },
    build: { version: "", status: "idle" },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
    },
  };
}

function renderPipeline(spec: Partial<SpecStage> = {}) {
  return render(<OverviewPipeline projectName={PROJECT} status={status(spec)} />);
}

describe("OverviewPipeline — the spec stage's action", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
  });

  // The single most expensive lesson of the previous build: the console must
  // never compose `/start`. The CTA goes to the spec view and does nothing else.
  it("navigates, and only navigates — no generate signal exists any more", () => {
    renderPipeline();

    fireEvent.click(screen.getByRole("button", { name: /Generate spec/ }));
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
    });
  });

  it("says the agent is starting the interview while the kickoff is claimed", () => {
    renderPipeline({ kickoff: { status: "pending", reason: "" } });

    expect(screen.getByText("Agent is starting the interview")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
  });

  it("says the agent is looking at your idea once its turn is running", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
    renderPipeline({ kickoff: { status: "started", reason: "" } });

    expect(screen.getByText("Agent is looking at your idea")).toBeInTheDocument();
  });

  it("counts the questions the agent is waiting on", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline({ kickoff: { status: "started", reason: "" } });

    expect(
      screen.getByText("Agent has 2 questions about your idea"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Continue spec/ }));
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
    });
  });

  it("uses the singular for one question", () => {
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc1",
      questions: [QUESTIONS[0]!],
    });
    renderPipeline({ kickoff: { status: "started", reason: "" } });

    expect(screen.getByText("Agent has 1 question about your idea")).toBeInTheDocument();
  });

  // A failed kickoff has to SAY so and offer the way out. A permanent
  // "starting…" with no error and no recovery is the state this replaces.
  it("names a failed kickoff and offers Retry, which calls the backend", () => {
    renderPipeline({
      kickoff: { status: "failed", reason: "The agents service was unreachable." },
    });

    expect(screen.getByText("The spec interview could not be started.")).toBeInTheDocument();
    expect(screen.getByText("The agents service was unreachable.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Continue spec/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Retry/ }));
    expect(mockRetry).toHaveBeenCalledTimes(1);
    // Retry asks the BACKEND to start the interview; it never navigates and it
    // never sends chat text.
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  // An amendment interview runs against a spec that already exists and asks
  // questions the same way, but the stage card that replaces the CTA offered no
  // sign one was open.
  it("brings the action back for an amendment on an existing spec", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline({ exists: true, version: "v2" });

    expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
    // The version and its status stay on screen, so continuing doesn't read as
    // starting over — and the card says no less than the one it replaced.
    expect(screen.getByText("v2")).toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });

  // Once a document exists it is the answer; the kickoff has nothing left to
  // say, and the backend stops reporting on it for the same reason.
  it("ignores a stale kickoff once the spec exists", () => {
    renderPipeline({
      exists: true,
      version: "v2",
      kickoff: { status: "failed", reason: "stale" },
    });

    expect(screen.queryByText("stale")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Retry/ })).not.toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });

  it("leaves the plain stage card alone when nothing is in flight", () => {
    renderPipeline({ exists: true, version: "v2" });

    expect(screen.queryByRole("button", { name: /Continue spec/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });
});

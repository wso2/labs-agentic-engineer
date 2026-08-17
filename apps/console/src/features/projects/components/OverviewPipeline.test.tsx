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

// The spec stage's action. The stage is derived from GIT, which cannot see an
// interview: no requirements file exists until the agent writes one, so the old
// card invited "Generate spec" for the entire duration of the interview the
// BACKEND had already started — and clicking it injected a second `/start`,
// which the one-active-turn guard rejected. The card no longer injects
// anything; these tests hold that line.

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { components } from "../../../generated/aep-api";
import { addMessage, chatKeyFor, replaceMessages } from "../../agent-chat/chatStore";
import { OverviewPipeline, OverviewPipelineSkeleton } from "./OverviewPipeline";

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

// The server-sourced interview state (#485) — mocked so these tests drive it
// directly; the hook's own polling/fallback logic is covered in
// useSpecInterview.test.tsx.
const mockInterview =
  vi.fn<
    (...args: unknown[]) => {
      running: boolean;
      questionsWaiting: number;
      drafting: boolean;
      started: boolean;
    }
  >();
vi.mock("../../agent-chat/useSpecInterview", () => ({
  useSpecInterview: (...args: unknown[]) => mockInterview(...args),
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
    spec: {
      exists: false,
      version: "",
      dirty: false,
      design: false,
      kickoff: { status: "none" as const, reason: "" },
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

function renderPipeline(spec: Partial<ProjectStatus["spec"]> = {}) {
  return render(<OverviewPipeline projectName={PROJECT} status={status(spec)} />);
}

describe("OverviewPipeline — the spec stage's action", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockInterview.mockReturnValue({
      running: false,
      questionsWaiting: 0,
      drafting: false,
      started: false,
    });
  });

  // The reported bug, at its source: the backend fires every new project's
  // `/start` at create, so this card has nothing to inject — and what it used
  // to inject raced that turn into a 409. NO navigation from this card carries
  // a `search` any more, in any state.
  it("navigates with no generate signal on an untouched project", () => {
    renderPipeline();

    fireEvent.click(screen.getByRole("button", { name: /Open spec/ }));
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
    });
  });

  it("never offers to generate the spec", () => {
    renderPipeline();

    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
  });

  it("offers Continue spec, with NO generate signal, while a question waits", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline();

    fireEvent.click(screen.getByRole("button", { name: /Continue spec/ }));
    // (SpecView itself opens the chat panel beside the doc while the turn is
    // active — the form still owns the spec body.)
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
    });
  });

  it("offers Continue spec while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "in_flight" });
    renderPipeline();

    expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
  });

  // An amendment interview runs against a spec that already exists, asks
  // questions the same way, and is skipped by a stray `/start` the same way —
  // but the stage card that replaces the CTA offered no sign one was open.
  it("brings the action back for an amendment on an existing spec", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline({ exists: true, version: "v2" });

    expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
    // The version and its status stay on screen, so continuing doesn't read as
    // starting over — and the card says no less than the one it replaced.
    expect(screen.getByText("v2")).toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });

  it("leaves the plain stage card alone when nothing is in flight", () => {
    renderPipeline({ exists: true, version: "v2" });

    expect(screen.queryByRole("button", { name: /Continue spec/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Open spec/ })).not.toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });

  // #485: the BE starts /start at create, so on a fresh landing the card must
  // show the LIVE state — no chat log exists in this browser yet, which is
  // exactly why the state is server-sourced (useSpecInterview), not log-derived.
  describe("the BE-started interview (#485)", () => {
    it("shows the preparing stage while the turn streams", () => {
      mockInterview.mockReturnValue({
        running: true,
        questionsWaiting: 0,
        drafting: false,
        started: true,
      });
      renderPipeline();

      expect(screen.getByText("Agent is processing the idea")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /Continue spec/ }));
      // No `search`, here as everywhere: the injected /start is gone.
      expect(mockNavigate).toHaveBeenCalledWith({
        to: "/projects/$projectName/spec",
        params: { projectName: PROJECT },
      });
    });

    it("shows the waiting-question count once the turn parks on questions", () => {
      mockInterview.mockReturnValue({
        running: false,
        questionsWaiting: 4,
        drafting: false,
        started: true,
      });
      renderPipeline();

      expect(screen.getByText("Agent has 4 questions")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
    });

    // The live-testing round: the card's stage text must cover the DRAFTING
    // phase too — after the answers land, "questions waiting" is over but the
    // agent is still working, and the card has to say on what.
    it("shows the drafting stage once the answered interview turn writes the PRD", () => {
      mockInterview.mockReturnValue({
        running: true,
        questionsWaiting: 0,
        drafting: true,
        started: true,
      });
      renderPipeline();

      expect(screen.getByText("Agent is drafting the PRD…")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Continue spec/ })).toBeInTheDocument();
    });

    it("keeps the singular for one waiting question", () => {
      mockInterview.mockReturnValue({
        running: false,
        questionsWaiting: 1,
        drafting: false,
        started: true,
      });
      renderPipeline();

      expect(screen.getByText("Agent has 1 question")).toBeInTheDocument();
    });

    // Round 3: `running` dips false between the 12 s poll's intervals and
    // between one first-run turn ending and the next attaching. The label must
    // not flip back there — the exchange is still open, and the button would
    // stop naming where it leads.
    it("keeps Continue spec through a lull in the started interview", () => {
      mockInterview.mockReturnValue({
        running: false,
        questionsWaiting: 0,
        drafting: false,
        started: true,
      });
      renderPipeline();

      expect(screen.queryByRole("button", { name: /Open spec/ })).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /Continue spec/ }));
      expect(mockNavigate).toHaveBeenCalledWith({
        to: "/projects/$projectName/spec",
        params: { projectName: PROJECT },
      });
    });

    it("polls only pre-spec: a settled project disables the hook", () => {
      renderPipeline({ exists: true, version: "v2" });

      expect(mockInterview).toHaveBeenCalledWith(ORG, PROJECT, false);
    });

    it("arms the hook on a fresh project", () => {
      renderPipeline();

      expect(mockInterview).toHaveBeenCalledWith(ORG, PROJECT, true);
    });
  });
});

// Live-testing round 2: for the first seconds of a fresh project the status
// query is still in flight, and the pipeline area was one blank grey slab —
// no stages, no chip, nothing to read. The loading state now shows the
// journey's real shape and skeletons for everything the status decides.
describe("OverviewPipelineSkeleton — the status-loading state", () => {
  it("names the three stages while claiming nothing about them", () => {
    render(<OverviewPipelineSkeleton />);

    expect(screen.getByTestId("overview-pipeline-skeleton")).toBeInTheDocument();
    expect(screen.getByText("Spec")).toBeInTheDocument();
    expect(screen.getByText("Build")).toBeInTheDocument();
    expect(screen.getByText("Deploy")).toBeInTheDocument();
    // No invented status: no version chip, no state line, no CTA.
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByText(/^Agent /)).not.toBeInTheDocument();
  });

  it("marks itself busy for assistive tech", () => {
    render(<OverviewPipelineSkeleton />);

    expect(screen.getByTestId("overview-pipeline-skeleton")).toHaveAttribute(
      "aria-busy",
      "true",
    );
  });
});

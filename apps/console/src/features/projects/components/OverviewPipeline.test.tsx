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

// The spec stage's two actions. The stage is derived from GIT, which cannot see
// an interview: no requirements file exists until the agent writes one, so the
// old card invited "Generate spec" for the entire duration of the interview it
// had already started — and a second click injected a `/start` that the start
// skill read as the user's skip valve.

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AskQuestionInput } from "@aep/agent-stream";
import type { components } from "../../../generated/aep-api";
import { addMessage, chatKeyFor, replaceMessages } from "../../agent-chat/chatStore";
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
    spec: { exists: false, version: "", dirty: false, design: false, ...spec },
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
  });

  it("offers Generate spec, carrying the generate signal, on an untouched project", () => {
    renderPipeline();

    fireEvent.click(screen.getByRole("button", { name: /Generate spec/ }));
    expect(mockNavigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/spec",
      params: { projectName: PROJECT },
      search: { generate: "requirements" },
    });
  });

  it("offers Continue spec, with NO generate signal, while a question waits", () => {
    addMessage(KEY, { role: "question", turnId: "t1", toolCallId: "tc1", questions: QUESTIONS });
    renderPipeline();

    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Continue spec/ }));
    // No `search`: the param is what injects the second `/start`, and dropping
    // it also leaves the chat panel closed so the question form owns the body.
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
    expect(screen.queryByRole("button", { name: /Generate spec/ })).not.toBeInTheDocument();
    expect(screen.getByText("published")).toBeInTheDocument();
  });

  it("retry still works: a failed attempt leaves Generate spec armed", () => {
    addMessage(KEY, { role: "user", content: "/start", turnId: "t1", status: "failed" });
    renderPipeline();

    expect(screen.getByRole("button", { name: /Generate spec/ })).toBeInTheDocument();
  });
});

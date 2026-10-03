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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { SAMPLE_COMPONENT, SAMPLE_MANIFEST, SAMPLE_SOURCE } from "../../../mocks/fixtures/prototype";
import { appPrototypes, manifestPath, sourcePath, type AppPrototype } from "../../prototype/model/prototypes";
import type { ProjectChat } from "../chatStore";

// The conversation's rows that the prototype adds to: a review's requests in
// place of the wire text, and the Open prototype note after a prototype turn,
// both worked out from the log (which the history carries) and the room.

let prototypes: AppPrototype[] | undefined;
vi.mock("../../prototype/usePrototypes", () => ({ usePrototypes: () => prototypes }));
const written = (...components: string[]) =>
  appPrototypes(
    components,
    Object.fromEntries(components.flatMap((c) => [[manifestPath(c), SAMPLE_MANIFEST], [sourcePath(c), SAMPLE_SOURCE]])),
    null,
  );

let chat: ProjectChat;
vi.mock("../useProjectChat", () => ({
  useProjectChat: () => chat,
  chatStore: {},
  canSend: () => true,
}));
vi.mock("../../spec/useSpecWorkspace", () => ({ useSpecModel: () => ({ data: { features: [] } }) }));
vi.mock("../useStartInterview", () => ({ useStartInterview: () => ({ start: vi.fn(), ready: true, waiting: false }) }));
const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

// jsdom has no ResizeObserver, which the thread uses to follow its growth.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

const { Thread } = await import("./Thread");

const ready = (items: ProjectChat["items"]): ProjectChat => ({ status: "ready", error: null, items, turn: { phase: "idle" } });

function renderThread() {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <Thread projectName="acme" scope={{ kind: "design" }} />
    </OxygenUIThemeProvider>,
  );
}

afterEach(() => {
  cleanup();
  navigate.mockReset();
});

const wrote = (id: string, component: string, state: "done" | "failed" = "done"): ProjectChat["items"][number] => ({
  kind: "activity",
  id,
  turnId: "history",
  toolCallId: id,
  op: "add",
  path: `specs/design/components/${component}/prototype.tsx`,
  state,
});

describe("a prototype review in the conversation", () => {
  it("reads as its requests, named from the manifest, not as the /prototype line it went over the wire as", () => {
    prototypes = written(SAMPLE_COMPONENT);
    const prototypeFeedback = {
      prototypeHash: "a".repeat(64),
      component: SAMPLE_COMPONENT,
      requests: [{ screenId: "screen.pending", roleId: "manager", stateId: "state.default", elementIds: ["btn.reject"], text: "Wider" }],
    };
    chat = ready([{ kind: "user", id: "h0", text: "/prototype expense-web", prototypeFeedback, state: "sent" }]);
    renderThread();
    expect(screen.getByText(/Feedback on the Acme Expenses prototype \(1 request\)/)).toHaveTextContent(
      /1\. Pending approvals \(Manager, Default\) — btn\.reject: Wider/,
    );
    expect(screen.queryByText("/prototype expense-web")).toBeNull();
  });

  it("reads a plain /prototype line (Make prototype) as asking for it", () => {
    prototypes = [];
    chat = ready([{ kind: "user", id: "h0", text: "/prototype expense-web", state: "sent" }]);
    renderThread();
    expect(screen.getByText("Prototype expense-web.")).toBeInTheDocument();
  });

  it("offers Open prototype after a /prototype turn that wrote a valid prototype, and opens its review", () => {
    prototypes = written(SAMPLE_COMPONENT);
    chat = ready([
      { kind: "user", id: "h0", text: "/prototype expense-web", state: "sent" },
      wrote("h1", SAMPLE_COMPONENT),
      { kind: "agent", id: "h2", turnId: "history", text: "Done." },
    ]);
    renderThread();
    expect(screen.getByText("The expense-web prototype is ready to review.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open prototype" }));
    expect(navigate).toHaveBeenCalledWith({
      to: "/projects/$projectName/prototype",
      params: { projectName: "acme" },
      search: { review: "expense-web" },
    });
  });

  it("offers nothing when the turn's writes were refused or the prototype is not valid now", () => {
    prototypes = written(SAMPLE_COMPONENT);
    chat = ready([{ kind: "user", id: "u1", text: "/prototype expense-web", state: "sent" }, wrote("w1", SAMPLE_COMPONENT, "failed")]);
    renderThread();
    expect(screen.queryByRole("button", { name: "Open prototype" })).toBeNull();
    cleanup();
    prototypes = [];
    chat = ready([{ kind: "user", id: "u1", text: "/prototype expense-web", state: "sent" }, wrote("w1", SAMPLE_COMPONENT)]);
    renderThread();
    expect(screen.queryByRole("button", { name: "Open prototype" })).toBeNull();
  });

  it("opens the Prototype tab after a turn that made several", () => {
    prototypes = written(SAMPLE_COMPONENT, "admin-web");
    chat = ready([{ kind: "user", id: "u1", text: "/prototype", state: "sent" }, wrote("w1", SAMPLE_COMPONENT), wrote("w2", "admin-web")]);
    renderThread();
    fireEvent.click(screen.getByRole("button", { name: "Open prototypes" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/prototype", params: { projectName: "acme" }, search: {} });
  });
});

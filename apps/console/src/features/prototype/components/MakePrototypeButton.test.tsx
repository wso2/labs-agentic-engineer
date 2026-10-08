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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { designKey } from "../../design/api/designModel";
import type { ProjectChat } from "../../agent-chat/chatStore";
import { SAMPLE_MANIFEST, SAMPLE_SOURCE } from "../../../mocks/fixtures/prototype";
import { appPrototypes, manifestPath, revisingIn, sourcePath, type AppPrototype } from "../model/prototypes";

// Make prototype, in the design card's actions: shown once the design has a
// web application, a `/prototype` turn in the chat, waiting while one runs.

let prototypes: AppPrototype[] | undefined;
vi.mock("../usePrototypes", () => ({ usePrototypes: () => prototypes }));

let chat: ProjectChat;
const send = vi.fn<(...args: unknown[]) => Promise<boolean>>(async () => true);
vi.mock("../../agent-chat/useProjectChat", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../agent-chat/useProjectChat")>()),
  useProjectChat: () => chat,
  chatStore: { send: (...args: unknown[]) => send(...args) },
}));

const openChat = vi.fn();
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open: openChat }) }));

const { MakePrototypeButton } = await import("./MakePrototypeButton");

const idle: ProjectChat = { status: "ready", error: null, items: [], turn: { phase: "idle" } };
const made = { [manifestPath("expense-web")]: SAMPLE_MANIFEST, [sourcePath("expense-web")]: SAMPLE_SOURCE };

let queryClient = new QueryClient();

function renderButton() {
  return render(
    <QueryClientProvider client={queryClient}>
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <MakePrototypeButton projectName="acme-expenses" />
      </OxygenUIThemeProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  queryClient = new QueryClient();
  chat = idle;
  send.mockClear();
  openChat.mockClear();
});

afterEach(cleanup);

describe("Make prototype", () => {
  it("is not offered until the design has a web application", () => {
    prototypes = [];
    renderButton();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("opens the chat, sends /prototype for the web application and reads the design again once the turn is sent", async () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    prototypes = appPrototypes(["expense-web"], {}, null);
    renderButton();
    fireEvent.click(screen.getByRole("button", { name: "Make prototype" }));
    expect(openChat).toHaveBeenCalled();
    expect(send).toHaveBeenCalledWith("acme-expenses", "/prototype expense-web", { kind: "prototype" });
    await vi.waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") }));
  });

  it("sends a bare /prototype when the design has several web applications", () => {
    prototypes = appPrototypes(["admin-web", "expense-web"], {}, null);
    renderButton();
    fireEvent.click(screen.getByRole("button", { name: "Make prototypes" }));
    expect(send).toHaveBeenCalledWith("acme-expenses", "/prototype", { kind: "prototype" });
  });

  it("says Update prototype once one exists", () => {
    prototypes = appPrototypes(["expense-web"], made, null);
    renderButton();
    expect(screen.getByRole("button", { name: "Update prototype" })).toBeEnabled();
  });

  it.each<[string, ProjectChat]>([
    ["the chat is loading", { ...idle, status: "loading" }],
    ["another turn is running", { ...idle, turn: { phase: "running", turnId: "t1", instruction: "/design F1" } }],
  ])("is unavailable while %s", (_, state) => {
    chat = state;
    prototypes = appPrototypes(["expense-web"], {}, null);
    renderButton();
    expect(screen.getByRole("button", { name: "Make prototype" })).toBeDisabled();
  });

  it("says why it waits while another turn runs", () => {
    chat = { ...idle, turn: { phase: "running", turnId: "t1", instruction: "/design F1" } };
    prototypes = appPrototypes(["expense-web"], {}, null);
    renderButton();
    expect(screen.getByLabelText(/The agent is busy with a turn/)).toContainElement(screen.getByRole("button", { name: "Make prototype" }));
  });

  it("says what runs while its own turn runs", () => {
    chat = { ...idle, turn: { phase: "running", turnId: "t1", instruction: "/prototype expense-web" } };
    prototypes = appPrototypes(["expense-web"], made, revisingIn("/prototype expense-web"));
    renderButton();
    expect(screen.getByRole("button", { name: "Updating prototype…" })).toBeDisabled();
  });
});

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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({ to, children }: { to: string; children?: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

const mockRotate = vi.fn<(projectName: string) => Promise<string>>(
  async () => "fresh-conversation-id",
);
vi.mock("../../agent-chat/api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../agent-chat/api/conversations")>();
  return {
    ...real,
    rotateConversation: (projectName: string) => mockRotate(projectName),
    rotateCurrentConversation: async (_qc: unknown, projectName: string) =>
      mockRotate(projectName),
  };
});

type EnvironmentDTO = components["schemas"]["EnvironmentDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type RegisterExternalResourceRequest =
  components["schemas"]["RegisterExternalResourceRequest"];

let environmentsState: {
  data?: EnvironmentDTO[];
  isLoading: boolean;
  isError: boolean;
  error?: Error | null;
  refetch: ReturnType<typeof vi.fn>;
};
let registerState: {
  mutate: ReturnType<typeof vi.fn>;
  isPending: boolean;
  error: Error | null;
};
let updateState: {
  mutate: ReturnType<typeof vi.fn>;
  isPending: boolean;
  error: Error | null;
};
let resourcesState: {
  data?: ExternalResourceDTO[];
  isLoading: boolean;
  isError: boolean;
};

vi.mock("../api/queries", () => ({
  useOrgEnvironments: () => environmentsState,
  useRegisterExternalResource: () => registerState,
  useUpdateExternalResource: () => updateState,
  useExternalResources: () => resourcesState,
}));

vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({
    user: { name: "Test", email: "t@example.com" },
    orgHandle: "acme",
    signOut: vi.fn(),
  }),
}));

vi.mock("../../agent-chat/components/AgentChatPanel", () => ({
  AgentChatPanel: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="agent-chat-panel">
      <button type="button" onClick={onClose}>
        Close agent chat
      </button>
    </div>
  ),
}));

import { REGISTER_EXTERNAL_RESOURCE_COMMAND } from "@aep/contracts/commands";
import {
  addMessage,
  chatKeyFor,
  consumePendingSeed,
  getMessages,
  peekPendingSeed,
  replaceMessages,
} from "../../agent-chat/chatStore";
import {
  clearRegisterDraft,
  publishRegisterDraft,
} from "../../agent-chat/registerDraftStore";
import { MARKETPLACE_CHAT_PROJECT } from "../constants";
import { RegisterFormPage } from "./RegisterFormPage";

function resetState() {
  environmentsState = {
    data: [{ name: "development" }, { name: "staging-local" }],
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  };
  registerState = {
    mutate: vi.fn(
      (
        _body: unknown,
        opts?: { onSuccess?: () => void },
      ) => {
        opts?.onSuccess?.();
      },
    ),
    isPending: false,
    error: null,
  };
  updateState = {
    mutate: vi.fn(
      (
        _body: unknown,
        opts?: { onSuccess?: () => void },
      ) => {
        opts?.onSuccess?.();
      },
    ),
    isPending: false,
    error: null,
  };
  resourcesState = {
    data: [],
    isLoading: false,
    isError: false,
  };
}

function renderPage(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function registerChatKey() {
  return chatKeyFor("acme", MARKETPLACE_CHAT_PROJECT);
}

async function waitForComposerSeed() {
  await waitFor(() => {
    expect(peekPendingSeed(registerChatKey())).not.toBeNull();
  });
}

function registeredStripe(): ExternalResourceDTO {
  return {
    name: "stripe",
    description: "Stripe payments API",
    consumptionInstructions: "Use the secret key as Bearer.",
    config: [
      { key: "api_key", secret: true, description: "Secret API key" },
      { key: "region", secret: false, description: "Stripe account region" },
    ],
    consumers: [],
    envCells: [
      { environment: "development", key: "api_key", status: "configured" },
      { environment: "staging-local", key: "api_key", status: "configured" },
      {
        environment: "development",
        key: "region",
        status: "configured",
        value: "us",
      },
      {
        environment: "staging-local",
        key: "region",
        status: "configured",
        value: "eu",
      },
    ],
    resourceDocs: [
      { type: "openapi", url: "https://example.com/stripe/openapi.yaml" },
    ],
  };
}

function renderEdit() {
  resourcesState = {
    data: [registeredStripe()],
    isLoading: false,
    isError: false,
  };
  return renderPage(<RegisterFormPage prompt="" name="stripe" />);
}

function fillRequired() {
  fireEvent.change(screen.getByLabelText(/^Name/), {
    target: { value: "twilio" },
  });
  fireEvent.change(screen.getAllByLabelText(/^Description/)[0]!, {
    target: { value: "Twilio SMS" },
  });
  fireEvent.change(screen.getByLabelText(/Consumption instructions/), {
    target: { value: "Use the auth token as Bearer." },
  });
  fireEvent.change(screen.getByLabelText(/development/i), {
    target: { value: "sk_dev" },
  });
  fireEvent.change(screen.getByLabelText(/staging-local/i), {
    target: { value: "sk_stg" },
  });
}

function submittedBody(): RegisterExternalResourceRequest {
  return registerState.mutate.mock.calls[0]?.[0] as RegisterExternalResourceRequest;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockRotate.mockResolvedValue("fresh-conversation-id");
  resetState();
  consumePendingSeed(registerChatKey());
  clearRegisterDraft(registerChatKey());
  replaceMessages(registerChatKey(), []);
});

describe("RegisterFormPage", () => {
  it("shows field errors on Register instead of disabling the button", () => {
    renderPage(<RegisterFormPage prompt="" />);

    const submit = screen.getByRole("button", { name: "Register" });
    expect(submit).toBeEnabled();
    fireEvent.click(submit);

    expect(registerState.mutate).not.toHaveBeenCalled();
    expect(screen.getAllByText("This field is required").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("development · API_KEY")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
  });

  it("labels env value fields from the environments hook, never a hardcoded Production", () => {
    renderPage(<RegisterFormPage prompt="" />);

    expect(screen.getByLabelText(/development/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/staging-local/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/Production/)).not.toBeInTheDocument();
    expect(screen.getByLabelText("development · API_KEY")).toBeInTheDocument();
    expect(screen.getByLabelText("staging-local · API_KEY")).toBeInTheDocument();
  });

  it("gives each key × environment field a unique accessible name", () => {
    renderPage(<RegisterFormPage prompt="" />);

    fireEvent.click(screen.getByRole("button", { name: "Add key" }));
    fireEvent.change(screen.getAllByLabelText(/^Key/)[1]!, {
      target: { value: "TOKEN" },
    });

    expect(screen.getByLabelText("development · API_KEY")).toBeInTheDocument();
    expect(screen.getByLabelText("development · TOKEN")).toBeInTheDocument();
    expect(screen.getByLabelText("staging-local · API_KEY")).toBeInTheDocument();
    expect(screen.getByLabelText("staging-local · TOKEN")).toBeInTheDocument();
  });

  it("shows a loading indicator while environments are pending", () => {
    environmentsState = {
      isLoading: true,
      isError: false,
      refetch: vi.fn(),
    };

    renderPage(<RegisterFormPage prompt="" />);

    expect(screen.getByLabelText("Loading environments")).toBeInTheDocument();
    expect(screen.queryByLabelText(/development/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register" })).toBeDisabled();
  });

  it("shows an error Alert and disables Register when environments fail to load", () => {
    environmentsState = {
      isLoading: false,
      isError: true,
      error: new Error("boom"),
      refetch: vi.fn(),
    };

    renderPage(<RegisterFormPage prompt="" />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Failed to load environments",
    );
    expect(screen.queryByLabelText(/development/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register" })).toBeDisabled();
  });

  it("shows an empty state when the organization has no environments", () => {
    environmentsState = {
      ...environmentsState,
      data: [],
    };

    renderPage(<RegisterFormPage prompt="" />);

    expect(
      screen.getByRole("heading", { name: "No OpenChoreo Environments" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/environment values cannot be filled/i),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/development/i)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Register" })).toBeDisabled();
  });

  it("shows a register-failure Alert without navigating", () => {
    registerState = {
      ...registerState,
      error: new Error("An external resource named twilio already exists"),
    };

    renderPage(<RegisterFormPage prompt="" />);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "An external resource named twilio already exists",
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it("navigates to /resources after a successful submit", () => {
    renderPage(<RegisterFormPage prompt="" />);

    fireEvent.change(screen.getByLabelText(/^Name/), {
      target: { value: "twilio" },
    });
    fireEvent.change(screen.getAllByLabelText(/^Description/)[0]!, {
      target: { value: "Twilio SMS" },
    });
    fireEvent.change(screen.getByLabelText(/Consumption instructions/), {
      target: { value: "Use the auth token as Bearer." },
    });
    fireEvent.change(screen.getByLabelText(/development/i), {
      target: { value: "sk_dev" },
    });
    fireEvent.change(screen.getByLabelText(/staging-local/i), {
      target: { value: "sk_stg" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    expect(registerState.mutate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith({ to: "/resources" });
  });

  it("Add doc defaults to Documentation", () => {
    renderPage(<RegisterFormPage prompt="" />);

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));

    expect(screen.getByRole("combobox", { name: "Type" })).toHaveTextContent(
      "Documentation",
    );
  });

  it("toggling File hides the URL textbox", () => {
    renderPage(<RegisterFormPage prompt="" />);

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));
    expect(screen.getByLabelText("URL")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "File" }));

    expect(screen.queryByLabelText("URL")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Choose file" })).toBeInTheDocument();
  });

  it("Register mutate sends a URL write row after adding a documentation URL", () => {
    renderPage(<RegisterFormPage prompt="" />);
    fillRequired();

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));
    fireEvent.change(screen.getByLabelText("URL"), {
      target: { value: "https://example.com/docs.md" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    expect(registerState.mutate).toHaveBeenCalledTimes(1);
    expect(submittedBody().resourceDocs).toEqual([
      { type: "documentation", url: "https://example.com/docs.md" },
    ]);
  });

  it("Register mutate sends fileName and content, not url, when a file is chosen", async () => {
    renderPage(<RegisterFormPage prompt="" />);
    fillRequired();

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));
    fireEvent.click(screen.getByRole("button", { name: "File" }));
    const input = document.querySelector<HTMLInputElement>("input[type=file]");
    expect(input).not.toBeNull();
    fireEvent.change(input!, {
      target: { files: [new File(["# Hello\n"], "README.md")] },
    });
    await waitFor(() => {
      expect(screen.getByText("README.md")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    expect(registerState.mutate).toHaveBeenCalledTimes(1);
    expect(submittedBody().resourceDocs).toEqual([
      { type: "documentation", fileName: "README.md", content: "# Hello\n" },
    ]);
  });

  it("rotates the register thread then seeds the composer prompt", async () => {
    const prompt = "Register Stripe as a payments API.";
    renderPage(<RegisterFormPage prompt={prompt} />);
    await waitForComposerSeed();
    expect(mockRotate).toHaveBeenCalledWith(MARKETPLACE_CHAT_PROJECT);
    expect(peekPendingSeed(registerChatKey())).toEqual({
      message: `${REGISTER_EXTERNAL_RESOURCE_COMMAND} ${prompt}`,
      guarded: true,
    });
    expect(navigate).toHaveBeenCalledWith({
      to: "/resources/register/form",
      replace: true,
      state: {},
    });
  });

  it("rotates and re-seeds even when the local log still has an old user turn", async () => {
    addMessage(registerChatKey(), {
      role: "user",
      content: `${REGISTER_EXTERNAL_RESOURCE_COMMAND} already sent`,
      status: "completed",
    });
    renderPage(<RegisterFormPage prompt="Register Stripe as a payments API." />);
    await waitForComposerSeed();
    expect(mockRotate).toHaveBeenCalledWith(MARKETPLACE_CHAT_PROJECT);
    expect(getMessages(registerChatKey())).toEqual([]);
  });

  it("does not seed when rotation fails", async () => {
    mockRotate.mockRejectedValueOnce(new Error("Failed to start a new conversation"));
    renderPage(<RegisterFormPage prompt="Register Stripe as a payments API." />);
    await waitFor(() => {
      expect(
        getMessages(registerChatKey()).some((m) => m.role === "error"),
      ).toBe(true);
    });
    expect(peekPendingSeed(registerChatKey())).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
  });

  it("does not rotate or seed without a composer prompt", () => {
    renderPage(<RegisterFormPage prompt="" />);
    expect(mockRotate).not.toHaveBeenCalled();
    expect(peekPendingSeed(registerChatKey())).toBeNull();
  });

  it("opens agent chat on the register form and allows dismiss then reopen", () => {
    renderPage(<RegisterFormPage prompt="Register Twilio for SMS." />);
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close agent chat" }));
    expect(screen.queryByTestId("agent-chat-panel")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open agent chat" }));
    expect(screen.getByTestId("agent-chat-panel")).toBeInTheDocument();
  });

  it("Register stays submittable after the chat is closed", () => {
    renderPage(<RegisterFormPage prompt="" />);
    fireEvent.click(screen.getByRole("button", { name: "Close agent chat" }));
    fillRequired();
    fireEvent.click(screen.getByRole("button", { name: "Register" }));
    expect(registerState.mutate).toHaveBeenCalled();
  });

  it("leaves env value fields unchanged after a chat draft that only patches description and consumption instructions", () => {
    renderPage(<RegisterFormPage prompt="" />);
    const env = screen.getByLabelText("development · API_KEY");
    fireEvent.change(env, { target: { value: "human-secret" } });
    const chatKey = chatKeyFor("acme", MARKETPLACE_CHAT_PROJECT);
    act(() => {
      publishRegisterDraft(chatKey, {
        description: "Patched description",
        consumptionInstructions: "Patched consumption instructions",
      });
    });
    expect(screen.getByLabelText("development · API_KEY")).toHaveValue("human-secret");
    expect(screen.getAllByLabelText(/^Description/)[0]).toHaveValue("Patched description");
    expect(screen.getByLabelText(/Consumption instructions/i)).toHaveValue(
      "Patched consumption instructions",
    );
  });

  it("shows a spinner instead of empty fields while the agent works on a composer prompt", () => {
    renderPage(<RegisterFormPage prompt="an API" />);
    expect(screen.getByLabelText("The agent is working on this resource")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument();
    expect(screen.queryByText("Environment values")).not.toBeInTheDocument();
  });

  it("fills non-secret fields from the draft after answers", () => {
    renderPage(<RegisterFormPage prompt="an API" />);
    act(() => {
      publishRegisterDraft(chatKeyFor("acme", MARKETPLACE_CHAT_PROJECT), {
        name: "stripe",
        description: "Payments API",
        consumptionInstructions: "Use the secret key as Bearer.",
        config: [{ key: "API_KEY", description: "Secret API key", secret: true }],
        resourceDocs: [{ type: "openapi", url: "https://example.com/stripe/openapi.yaml" }],
      });
    });
    expect(screen.getByLabelText(/^Name/)).toHaveValue("stripe");
    expect(screen.getAllByLabelText(/^Description/)[0]).toHaveValue("Payments API");
    expect(screen.getByLabelText(/Consumption instructions/i)).toHaveValue(
      "Use the secret key as Bearer.",
    );
    expect(screen.getByLabelText("development · API_KEY")).toHaveValue("");
  });

  it("does not change a human-typed env value when a later draft patches description only", () => {
    renderPage(<RegisterFormPage prompt="an API" />);
    const chatKey = chatKeyFor("acme", MARKETPLACE_CHAT_PROJECT);
    act(() => {
      publishRegisterDraft(chatKey, {
        name: "stripe",
        description: "Payments API",
        consumptionInstructions: "Use the secret key as Bearer.",
        config: [{ key: "API_KEY", description: "Secret API key", secret: true }],
      });
    });
    fireEvent.change(screen.getByLabelText("development · API_KEY"), {
      target: { value: "human-secret" },
    });
    act(() => {
      publishRegisterDraft(chatKey, {
        description: "Patched after answers",
      });
    });
    expect(screen.getByLabelText("development · API_KEY")).toHaveValue("human-secret");
    expect(screen.getAllByLabelText(/^Description/)[0]).toHaveValue("Patched after answers");
  });
});

describe("RegisterFormPage edit mode", () => {
  it("freezes name and key identity; key descriptions stay editable", () => {
    renderEdit();

    expect(screen.getByLabelText(/^Name/)).toBeDisabled();
    expect(screen.getByLabelText(/^Name/)).toHaveValue("stripe");
    expect(screen.queryByRole("button", { name: "Add key" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove key" })).not.toBeInTheDocument();

    for (const keyField of screen.getAllByLabelText(/^Key/)) {
      expect(keyField).toBeDisabled();
    }
    for (const secret of screen.getAllByRole("checkbox", { name: "Secret" })) {
      expect(secret).toBeDisabled();
    }
    const descriptions = screen.getAllByLabelText(/^Description/);
    expect(descriptions.length).toBeGreaterThan(1);
    for (const field of descriptions) {
      expect(field).toBeEnabled();
    }
  });

  it("shows the keep-secret helper and never a fake mask", () => {
    renderEdit();

    expect(
      screen.getAllByText("Leave blank to keep the current value").length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText(/••••/)).not.toBeInTheDocument();
  });

  it("prefills non-secret env values from envCells", () => {
    renderEdit();

    expect(screen.getByLabelText("development · region")).toHaveValue("us");
    expect(screen.getByLabelText("staging-local · region")).toHaveValue("eu");
    expect(screen.getByLabelText("development · api_key")).toHaveValue("");
    expect(screen.getByLabelText("staging-local · api_key")).toHaveValue("");
  });

  it("Save with empty configured secret calls update, not register, and navigates", () => {
    renderEdit();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(updateState.mutate).toHaveBeenCalledTimes(1);
    expect(registerState.mutate).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith({ to: "/resources" });
  });

  it("does not seed the register command in edit mode", () => {
    renderEdit();
    expect(peekPendingSeed(registerChatKey())).toBeNull();
    expect(mockRotate).not.toHaveBeenCalled();
  });

  it("replaces the first-turn spinner with the question form when the agent asks", async () => {
    renderPage(<RegisterFormPage prompt="github" />);
    await waitForComposerSeed();
    expect(screen.getByLabelText("The agent is working on this resource")).toBeInTheDocument();

    act(() => {
      addMessage(chatKeyFor("acme", MARKETPLACE_CHAT_PROJECT), {
        role: "question",
        turnId: "t1",
        toolCallId: "tc1",
        questions: [
          {
            question: "How should consuming projects authenticate to GitHub?",
            options: [{ label: "PAT" }, { label: "GitHub App" }],
          },
        ],
      });
    });

    expect(screen.getByTestId("chat-question-form")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument();
    expect(
      screen.getByText("How should consuming projects authenticate to GitHub?"),
    ).toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalledWith({ to: "/resources" });
  });

  it("keeps the question pane after Continue until a draft arrives", async () => {
    const chatKey = registerChatKey();
    renderPage(<RegisterFormPage prompt="github" />);
    await waitForComposerSeed();
    act(() => {
      addMessage(chatKey, {
        role: "question",
        turnId: "t1",
        toolCallId: "tc1",
        questions: [
          {
            question: "How should consuming projects authenticate to GitHub?",
            options: [{ label: "PAT" }, { label: "GitHub App" }],
          },
        ],
      });
    });
    fireEvent.click(screen.getByText("PAT"));
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    act(() => {
      addMessage(chatKey, {
        role: "user",
        content: 'Answer to "How should consuming projects authenticate to GitHub?": PAT',
        status: "completed",
      });
    });

    expect(screen.getByTestId("chat-question-form")).toBeInTheDocument();
    expect(screen.getByText("Drafting the catalog form from your answers.")).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument();
  });
});

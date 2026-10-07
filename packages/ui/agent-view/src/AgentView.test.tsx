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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AgentView, type AgentModelConnection } from "./AgentView.js";

const AFM = `---
spec_version: "0.4.0"
name: "booking-agent"
description: "Books hotels by chatting."
max_iterations: 12

model:
  provider: "anthropic"
  name: "\${env:MODEL_NAME}"
  url: "\${env:MODEL_ENDPOINT}"
  authentication:
    type: "api-key"
    api_key: "\${env:MODEL_API_KEY}"

interfaces:
  - type: webchat
    exposure:
      http:
        path: "/chat"

x-aep:
  tools:
    openapi:
      - component: "hotel-api"
        baseUrl: "\${env:HOTEL_API_URL}"
        allow: [listHotels, createReservation]
  memory:
    type: "client"
  identity:
    mode: "on-behalf-of"
---

# Role
You help a traveler book a hotel.

# Style
Short and practical.
`;

const NO_TOOLS_AFM = `---
spec_version: "0.4.0"
name: "faq-agent"
model:
  provider: "anthropic"
  name: "\${env:MODEL_NAME}"
x-aep:
  memory:
    type: "server"
---

# Role
You answer questions from the published policies.
`;

const CONNECTION: AgentModelConnection = {
  model: "claude-sonnet-5",
  format: "Anthropic Messages",
  host: "api.anthropic.com",
};

function openTab(name: RegExp | string) {
  fireEvent.click(screen.getByRole("tab", { name }));
}

describe("AgentView — header and tabs", () => {
  it("names the agent and says what it does above the tabs", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText("ai-agent")).toBeInTheDocument();
    expect(screen.getByText("booking-agent")).toBeInTheDocument();
    expect(screen.getByText("Books hotels by chatting.")).toBeInTheDocument();
  });

  it("splits the spec into Instructions, Tools and Configuration, opening on Instructions", () => {
    render(<AgentView spec={AFM} />);

    const tabs = screen.getAllByRole("tab").map((t) => t.textContent);
    expect(tabs).toEqual(["Instructions", "Tools", "Configuration"]);
    expect(screen.getByRole("tab", { name: "Instructions" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText(/You help a traveler book a hotel/)).toBeInTheDocument();
    expect(screen.queryByText("listHotels")).not.toBeInTheDocument();
  });

  it("shows an alert instead of throwing when the document has no front matter", () => {
    render(<AgentView spec={"# Role\nno front matter here\n"} />);

    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});

describe("AgentView — Instructions", () => {
  it("renders the prompt through a caller-supplied markdown renderer", () => {
    render(<AgentView spec={AFM} renderMarkdown={(md) => <pre data-testid="md">{md}</pre>} />);

    expect(screen.getByTestId("md").textContent).toContain("# Role");
  });

  it("falls back to plain text when no renderer is given", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText(/You help a traveler book a hotel/)).toBeInTheDocument();
    expect(screen.getByText(/Short and practical/)).toBeInTheDocument();
  });

  it("says so when the agent has no instructions yet", () => {
    render(<AgentView spec={`---\nname: "blank-agent"\n---\n`} />);

    expect(screen.getByText("No instructions yet.")).toBeInTheDocument();
  });

  it("stays read-only when no save handler is given", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("edits the body verbatim, not a rebuild of the parsed sections", () => {
    render(<AgentView spec={AFM} onSaveBehaviour={vi.fn().mockResolvedValue(undefined)} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));

    // The box opens on the RAW body, headings and all, because that is what
    // gets written back. A box seeded from the section reader would drop
    // anything it did not recognise.
    const box = screen.getByRole("textbox", { name: "Instructions" }) as HTMLTextAreaElement;
    expect(box.value).toContain("# Role");
    expect(box.value).toContain("# Style");
    // Front matter never reaches the box: it is wiring, not prose.
    expect(box.value).not.toContain("spec_version");
    expect(box.value).not.toContain("listHotels");
  });

  it("hands the edited body to the caller on save", async () => {
    const onSaveBehaviour = vi.fn().mockResolvedValue(undefined);
    render(<AgentView spec={AFM} onSaveBehaviour={onSaveBehaviour} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Instructions" }), {
      target: { value: "# Role\nYou book trains, not hotels.\n" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onSaveBehaviour).toHaveBeenCalledWith("# Role\nYou book trains, not hotels."),
    );
  });

  it("discards the draft on cancel and writes nothing", () => {
    const onSaveBehaviour = vi.fn();
    render(<AgentView spec={AFM} onSaveBehaviour={onSaveBehaviour} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Instructions" }), { target: { value: "throw away" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onSaveBehaviour).not.toHaveBeenCalled();
    expect(screen.getByText(/You help a traveler book a hotel/)).toBeInTheDocument();
  });

  // An edited prompt is baked into generated code at build time, so the running
  // agent keeps its old behaviour until it is rebuilt. Saying so is the whole
  // difference between a useful edit box and one that looks broken.
  it("says the change needs a rebuild before it reaches the running agent", async () => {
    render(<AgentView spec={AFM} onSaveBehaviour={vi.fn().mockResolvedValue(undefined)} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/rebuild/i)).toBeInTheDocument();
  });

  it("surfaces a failed save instead of pretending it worked", async () => {
    const onSaveBehaviour = vi.fn().mockRejectedValue(new Error("collab is offline"));
    render(<AgentView spec={AFM} onSaveBehaviour={onSaveBehaviour} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("collab is offline")).toBeInTheDocument();
    // Still editing, so the draft is not lost to a failed write.
    expect(screen.getByRole("textbox", { name: "Instructions" })).toBeInTheDocument();
  });
});

describe("AgentView — Tools", () => {
  it("lists each allowed operation under the component that provides it", () => {
    render(<AgentView spec={AFM} />);
    openTab("Tools");

    expect(screen.getByText("hotel-api")).toBeInTheDocument();
    expect(screen.getByText("listHotels")).toBeInTheDocument();
    expect(screen.getByText("createReservation")).toBeInTheDocument();
  });

  it("renders without toolStatus: no status chips for callers that don't fetch it", () => {
    render(<AgentView spec={AFM} />);
    openTab("Tools");

    expect(screen.queryByText("Resolved")).not.toBeInTheDocument();
    expect(screen.queryByText("Unresolved")).not.toBeInTheDocument();
  });

  it("surfaces the server's reason when an operation is unresolved", () => {
    render(
      <AgentView
        spec={AFM}
        toolStatus={{
          "hotel-api:listHotels": { status: "resolved" },
          "hotel-api:createReservation": {
            status: "unresolved",
            reason: "createReservation is not an operationId of hotel-api's contract",
          },
        }}
      />,
    );
    openTab(/Tools/);

    expect(screen.getByText("Resolved")).toBeInTheDocument();
    expect(screen.getByText("Unresolved")).toBeInTheDocument();
    expect(
      screen.getByText("createReservation is not an operationId of hotel-api's contract"),
    ).toBeInTheDocument();
  });

  // A broken tool is the one thing on a hidden tab a reviewer must not miss.
  it("counts unresolved operations on the Tools tab itself", () => {
    render(
      <AgentView
        spec={AFM}
        toolStatus={{
          "hotel-api:listHotels": { status: "unresolved" },
          "hotel-api:createReservation": { status: "unresolved" },
        }}
      />,
    );

    expect(screen.getByRole("tab", { name: /Tools/ }).textContent).toContain("2 unresolved");
  });

  it("puts no count on the tab when every operation resolves", () => {
    render(
      <AgentView
        spec={AFM}
        toolStatus={{
          "hotel-api:listHotels": { status: "resolved" },
          "hotel-api:createReservation": { status: "unchecked" },
        }}
      />,
    );

    expect(screen.getByRole("tab", { name: "Tools" }).textContent).toBe("Tools");
  });

  it("says an agent with no tools answers from its instructions alone", () => {
    render(<AgentView spec={NO_TOOLS_AFM} />);
    openTab("Tools");

    expect(screen.getByText(/answers from its instructions alone/)).toBeInTheDocument();
  });
});

describe("AgentView — Configuration", () => {
  it("shows the organisation's model connection, not the AFM's provider", () => {
    render(<AgentView spec={AFM} modelConnection={CONNECTION} />);
    openTab("Configuration");

    expect(screen.getByText("claude-sonnet-5")).toBeInTheDocument();
    expect(screen.getByText("Anthropic Messages")).toBeInTheDocument();
    expect(screen.getByText("api.anthropic.com")).toBeInTheDocument();
    expect(
      screen.getByText("Through the environment's AI gateway (Agent Manager)"),
    ).toBeInTheDocument();
    expect(screen.queryByText("anthropic")).not.toBeInTheDocument();
    expect(screen.queryByText("Provider")).not.toBeInTheDocument();
  });

  it("links to where the connection is changed", () => {
    render(
      <AgentView
        spec={AFM}
        modelConnection={CONNECTION}
        settingsLink={<a href="/settings">Change in Settings</a>}
      />,
    );
    openTab("Configuration");

    expect(screen.getByRole("link", { name: "Change in Settings" })).toBeInTheDocument();
  });

  it("says no model is connected when the organisation has none", () => {
    render(
      <AgentView
        spec={AFM}
        modelConnection={null}
        settingsLink={<a href="/settings">Change in Settings</a>}
      />,
    );
    openTab("Configuration");

    expect(screen.getByText(/No model connected/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Change in Settings" })).toBeInTheDocument();
  });

  it("says the connection is loading while it is", () => {
    render(<AgentView spec={AFM} modelConnection="loading" />);
    openTab("Configuration");

    expect(screen.getByText(/Loading the organisation's model connection/)).toBeInTheDocument();
  });

  it("leaves the Model panel out when the caller has no connection to show", () => {
    render(<AgentView spec={AFM} />);
    openTab("Configuration");

    expect(screen.queryByText("Model")).not.toBeInTheDocument();
  });

  it("never renders the ${env:} placeholders or the key fields", () => {
    render(<AgentView spec={AFM} modelConnection={CONNECTION} />);
    openTab("Configuration");

    expect(screen.queryByText(/\$\{env:/)).not.toBeInTheDocument();
    expect(screen.queryByText("api-key")).not.toBeInTheDocument();
    expect(screen.queryByText(/MODEL_API_KEY/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Never shown/)).not.toBeInTheDocument();
  });

  it("shows how the agent is reached and what that means", () => {
    render(<AgentView spec={AFM} />);
    openTab("Configuration");

    expect(screen.getByText("POST /chat")).toBeInTheDocument();
    expect(screen.getByText(/An HTTP endpoint a web app calls/)).toBeInTheDocument();
  });

  it("says who remembers the conversation when the web app keeps it", () => {
    render(<AgentView spec={AFM} />);
    openTab("Configuration");

    expect(screen.getByText("The web app remembers the conversation")).toBeInTheDocument();
    expect(screen.queryByText("client")).not.toBeInTheDocument();
  });

  it("says who remembers the conversation when the agent keeps it", () => {
    render(<AgentView spec={NO_TOOLS_AFM} />);
    openTab("Configuration");

    expect(screen.getByText("The agent remembers the conversation")).toBeInTheDocument();
  });

  it("leaves out the step cap and the identity mode", () => {
    render(<AgentView spec={AFM} modelConnection={CONNECTION} />);
    openTab("Configuration");

    expect(screen.queryByText(/12/)).not.toBeInTheDocument();
    expect(screen.queryByText(/on-behalf-of/)).not.toBeInTheDocument();
    expect(screen.queryByText(/as the user/i)).not.toBeInTheDocument();
  });

  it("says which files the agent takes, and how many and how large", () => {
    const withFiles = AFM.replace(
      '  memory:\n    type: "client"',
      '  memory:\n    type: "client"\n  attachments:\n    types: [application/pdf, image/png]\n    maxFiles: 3\n    maxFileSizeMB: 5',
    );
    render(<AgentView spec={withFiles} />);
    openTab("Configuration");

    expect(screen.getByText("PDF, PNG")).toBeInTheDocument();
    expect(screen.getByText("Up to 3 files, 5 MB each")).toBeInTheDocument();
  });

  it("says a text-only agent takes no files", () => {
    render(<AgentView spec={AFM} />);
    openTab("Configuration");

    expect(screen.getByText("Text only, no files")).toBeInTheDocument();
  });
});

describe("AgentView — Guardrails", () => {
  const GUARDED = AFM.replace(
    '  memory:\n    type: "client"',
    '  memory:\n    type: "client"\n  guardrails:\n' +
      '    - policy: pii-masking-regex\n      params: { email: true }\n      why: "The model never needs contact details."\n' +
      '    - policy: regex-guardrail\n      params:\n        request: { regex: "casino", invert: true }\n      why: "Gambling is not a business expense."',
  );

  it("lists each declared guardrail with why the agent has it", () => {
    render(<AgentView spec={GUARDED} />);
    openTab("Configuration");

    expect(screen.getByText("pii-masking-regex")).toBeInTheDocument();
    expect(screen.getByText("The model never needs contact details.")).toBeInTheDocument();
    expect(screen.getByText("regex-guardrail")).toBeInTheDocument();
  });

  it("says what the last deploy did with each, per environment", () => {
    render(
      <AgentView
        spec={GUARDED}
        guardrailStatus={{
          "pii-masking-regex": [{ environment: "development", status: "applied" }],
          "regex-guardrail": [
            { environment: "development", status: "invalid", reason: "the regex does not compile" },
          ],
        }}
      />,
    );
    openTab("Configuration");

    expect(screen.getByText("development: applied")).toBeInTheDocument();
    expect(screen.getByText("development: invalid")).toBeInTheDocument();
    expect(screen.getByText("the regex does not compile")).toBeInTheDocument();
  });

  it("says when a declared guardrail has not been deployed yet", () => {
    render(<AgentView spec={GUARDED} guardrailStatus={{}} />);
    openTab("Configuration");

    expect(screen.getAllByText("Not deployed yet")).toHaveLength(2);
  });

  // A live collaboration draft reaches the view before the write gate that
  // refuses a repeated policy, so a repeat must still render as two rows.
  it("renders a repeated policy in an unvalidated draft as separate rows", () => {
    const repeated = GUARDED.replace(
      "    - policy: regex-guardrail",
      '    - policy: pii-masking-regex\n      params: { phone: true }\n      why: "Phone numbers too."\n    - policy: regex-guardrail',
    );
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    render(<AgentView spec={repeated} />);
    openTab("Configuration");

    expect(screen.getAllByText("pii-masking-regex")).toHaveLength(2);
    expect(screen.getByText("Phone numbers too.")).toBeInTheDocument();
    expect(errors.mock.calls.some((c) => String(c[0]).includes("same key"))).toBe(false);
    errors.mockRestore();
  });

  // The panel is about checks the AI gateway applies; with none, there is
  // nothing to show, and an empty "declared" row only reads as a contradiction
  // of rules the agent itself follows.
  it("leaves the panel out when the agent has no AI gateway guardrails", () => {
    render(<AgentView spec={AFM} />);
    openTab("Configuration");

    expect(screen.queryByText("Guardrails")).not.toBeInTheDocument();
    expect(screen.queryByText("None declared")).not.toBeInTheDocument();
  });
});

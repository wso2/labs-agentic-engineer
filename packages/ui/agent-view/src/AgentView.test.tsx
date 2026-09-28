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

import { AgentView } from "./AgentView.js";

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

describe("AgentView", () => {
  it("renders the agent's identity and its prompt sections", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText("booking-agent")).toBeInTheDocument();
    expect(screen.getByText("Books hotels by chatting.")).toBeInTheDocument();
    expect(screen.getByText("Role")).toBeInTheDocument();
    expect(screen.getByText("You help a traveler book a hotel.")).toBeInTheDocument();
    expect(screen.getByText("Style")).toBeInTheDocument();
  });

  it("lists each allowed operation under the component that provides it", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText("hotel-api")).toBeInTheDocument();
    expect(screen.getByText("listHotels")).toBeInTheDocument();
    expect(screen.getByText("createReservation")).toBeInTheDocument();
  });

  it("renders without toolStatus — no status chips for callers that don't fetch it", () => {
    render(<AgentView spec={AFM} />);

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

    expect(screen.getByText("Resolved")).toBeInTheDocument();
    expect(screen.getByText("Unresolved")).toBeInTheDocument();
    expect(
      screen.getByText("createReservation is not an operationId of hotel-api's contract"),
    ).toBeInTheDocument();
  });

  it("does not surface metadata chips — spec version, step cap, memory, identity are noise here", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.queryByText(/AFM 0\.4\.0/)).not.toBeInTheDocument();
    expect(screen.queryByText(/max 12 steps/)).not.toBeInTheDocument();
    expect(screen.queryByText(/client memory/)).not.toBeInTheDocument();
    expect(screen.queryByText("on-behalf-of")).not.toBeInTheDocument();
  });

  it("says where the model comes from rather than showing the ${env:} placeholder", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText("anthropic")).toBeInTheDocument();
    expect(screen.queryByText(/\$\{env:MODEL_NAME\}/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\$\{env:MODEL_ENDPOINT\}/)).not.toBeInTheDocument();
    expect(screen.getAllByText("set by the platform at deploy").length).toBeGreaterThan(0);
  });

  it("never renders the auth or API-key fields", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.queryByText("api-key")).not.toBeInTheDocument();
    expect(screen.queryByText(/MODEL_API_KEY/)).not.toBeInTheDocument();
    expect(screen.queryByText("API key")).not.toBeInTheDocument();
  });

  it("explains what the interface type means", () => {
    render(<AgentView spec={AFM} />);

    // One row in the Configuration table now — type and path read together.
    expect(screen.getByText("webchat · POST /chat")).toBeInTheDocument();
    expect(screen.getByText(/an HTTP endpoint a web app calls/i)).toBeInTheDocument();
  });

  it("shows an alert instead of throwing when the document has no front matter", () => {
    render(<AgentView spec={"# Role\nno front matter here\n"} />);

    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});

describe("AgentView — editing the behaviour prompt", () => {
  it("stays read-only when no save handler is given", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("edits the body verbatim, not a rebuild of the parsed sections", async () => {
    const onSaveBehaviour = vi.fn().mockResolvedValue(undefined);
    render(<AgentView spec={AFM} onSaveBehaviour={onSaveBehaviour} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));

    // The textarea opens on the RAW body — headings and all — because that is
    // what gets written back. A box seeded from the section reader would drop
    // anything it did not recognise.
    const box = screen.getByLabelText("Behaviour") as HTMLTextAreaElement;
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
    fireEvent.change(screen.getByLabelText("Behaviour"), {
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
    fireEvent.change(screen.getByLabelText("Behaviour"), { target: { value: "throw away" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onSaveBehaviour).not.toHaveBeenCalled();
    expect(screen.getByText("You help a traveler book a hotel.")).toBeInTheDocument();
  });

  // An edited prompt is baked into generated code at build time, so the running
  // agent keeps its old behaviour until it is rebuilt. Saying so is the whole
  // difference between a useful edit box and one that looks broken.
  it("says the change needs a rebuild before it reaches the running agent", async () => {
    const onSaveBehaviour = vi.fn().mockResolvedValue(undefined);
    render(<AgentView spec={AFM} onSaveBehaviour={onSaveBehaviour} />);

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
    expect(screen.getByLabelText("Behaviour")).toBeInTheDocument();
  });
});

describe("AgentView — reading order", () => {
  /** Rendered section headings, in document order. */
  function headings() {
    return Array.from(document.querySelectorAll("h1,h2,h3,h4,h5,h6,.MuiTypography-overline"))
      .map((el) => el.textContent?.trim())
      .filter((t): t is string => Boolean(t));
  }

  // The prompt IS the agent — the skills say the body becomes the system prompt
  // verbatim — so it is what a reviewer came to read. The wiring below it is
  // mostly `${env:}` placeholders the platform fills in.
  it("leads with the prompt, not the wiring", () => {
    render(<AgentView spec={AFM} />);

    const order = headings();
    expect(order.indexOf("Behaviour")).toBeLessThan(order.indexOf("Configuration"));
    expect(order.indexOf("Behaviour")).toBeLessThan(order.indexOf("Tools"));
  });

  // Tools keep their own section between the prompt and the wiring. The
  // allow-list is the security boundary and carries resolution status, so it
  // must not read as configuration trivia in a table of env placeholders.
  it("keeps Tools between the prompt and the configuration table", () => {
    render(<AgentView spec={AFM} />);

    const order = headings();
    expect(order.indexOf("Tools")).toBeGreaterThan(order.indexOf("Behaviour"));
    expect(order.indexOf("Tools")).toBeLessThan(order.indexOf("Configuration"));
  });

  it("puts the wiring in one labelled table rather than three sections", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText("Configuration")).toBeInTheDocument();
    expect(screen.getByText("Model")).toBeInTheDocument();
    expect(screen.getByText("Interface")).toBeInTheDocument();
    expect(screen.getByText("Memory")).toBeInTheDocument();
    // The fixture declares client memory; the point is the value is shown as a
    // labelled row in the table, not which value it happens to be.
    expect(screen.getByText("client")).toBeInTheDocument();
  });

  // The package carries no markdown dependency: the console passes its own
  // renderer so the PRD, alerts and this all look like one product.
  it("renders the prompt through a caller-supplied markdown renderer", () => {
    render(
      <AgentView
        spec={AFM}
        renderMarkdown={(md) => <pre data-testid="md">{md}</pre>}
      />,
    );

    expect(screen.getByTestId("md").textContent).toContain("# Role");
  });

  it("falls back to plain text when no renderer is given", () => {
    render(<AgentView spec={AFM} />);

    expect(screen.getByText(/You help a traveler book a hotel/)).toBeInTheDocument();
  });
});

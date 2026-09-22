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

// The page against the console's own mock layer: the real project handlers
// serve the files, so "canonical rendering" is exactly what mock mode shows.

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import createClient from "openapi-fetch";
import { stablePrototypeJson } from "@aep/prototype-model";
import type { paths } from "../../../generated/aep-api";
import { agentChatHandlers } from "../../../mocks/handlers/agent-chat";
import { projectHandlers } from "../../../mocks/handlers/project";
import { expenseApproval } from "../testing/fixtures";
import type { PrototypeViewRequest } from "../model/viewState";
import { chatKeyFor, consumePendingSeed, getMessages, hasLocalTurnActivity, replaceMessages } from "../../agent-chat/chatStore";

vi.mock("../../../api/client", () => ({
  // Resolve fetch per call: MSW patches it when the server starts listening.
  client: createClient<paths>({ baseUrl: "http://localhost/api/v1", fetch: (req) => globalThis.fetch(req) }),
}));
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to }: { children?: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
  useNavigate: () => mockNavigate,
}));
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ orgHandle: "acme" }),
}));
vi.mock("../../agent-chat/currentUser", () => ({
  useCurrentAuthor: () => ({ id: "ann@example.com", displayName: "Ann" }),
}));

const { ComponentPrototypePage } = await import("./ComponentPrototypePage");

const server = setupServer(...projectHandlers, ...agentChatHandlers);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  cleanup();
});
afterAll(() => server.close());
beforeEach(() => {
  localStorage.clear();
  mockNavigate.mockReset();
  consumePendingSeed(chatKeyFor("acme", "demo-shop"));
  replaceMessages(chatKeyFor("acme", "demo-shop"), []);
});

const FILE = "*/api/v1/projects/:projectName/files/specs/design/components/storefront/prototype.json";

function renderPage(component = "storefront", search: PrototypeViewRequest = {}) {
  const onSearchChange = vi.fn();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QueryClientProvider client={queryClient}>
        <ComponentPrototypePage projectName="demo-shop" component={component} search={search} onSearchChange={onSearchChange} />
      </QueryClientProvider>
    </OxygenUIThemeProvider>,
  );
  return { onSearchChange };
}

const application = () => screen.queryByRole("region", { name: /prototype$/ });

describe("ComponentPrototypePage", () => {
  it("shows a loading state while the file is read", () => {
    renderPage();
    expect(screen.getByRole("progressbar", { name: "Loading prototype" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Back to Spec/ })).toHaveAttribute("href", "/projects/$projectName/spec");
  });

  it("renders the mock project's prototype: review bar, read-only tag, the application in its window", async () => {
    const { onSearchChange } = renderPage();

    expect(await screen.findByRole("heading", { name: "Approval queue", level: 2 })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Expense approvals", level: 1 })).toBeInTheDocument();
    expect(screen.getByText("Prototype · read-only")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Back to Spec/ })).toBeInTheDocument();
    expect(within(application()!).getByLabelText("Address")).toHaveTextContent("storefront.example.com/queue");
    expect(screen.getByRole("button", { name: "Preview" })).toHaveAttribute("aria-pressed", "true");
    // The resolved view is written back to the URL once, whole.
    expect(onSearchChange).toHaveBeenLastCalledWith({ screen: "screen.queue", state: "state.default" });
  });

  it("opens where the URL points", async () => {
    renderPage("storefront", { screen: "screen.new", flow: "flow.submit", state: "state.invalid" });
    expect(await screen.findByRole("heading", { name: "New expense", level: 2 })).toBeInTheDocument();
    expect(screen.getByText("Amount must be greater than zero")).toBeInTheDocument();
  });

  it("reports every view change with the whole view, so nothing drops out of the URL", async () => {
    const { onSearchChange } = renderPage("storefront", { state: "state.failed" });
    await screen.findByRole("heading", { name: "Approval queue", level: 2 });

    fireEvent.click(document.querySelector('[data-prototype-component-id="expense.1042"]')!);
    expect(await screen.findByRole("heading", { name: "Expense #1042 · Maya Fernando" })).toBeInTheDocument();
    expect(onSearchChange).toHaveBeenLastCalledWith({ screen: "screen.detail", state: "state.failed" });

    fireEvent.click(screen.getByRole("button", { name: "Annotate" }));
    expect(onSearchChange).toHaveBeenLastCalledWith({ screen: "screen.detail", state: "state.failed", mode: "annotate" });
  });

  it("lists the coded issues of an invalid file and renders nothing of it", async () => {
    server.use(
      http.get(FILE, () =>
        HttpResponse.json({
          path: "specs/design/components/storefront/prototype.json",
          sha: "f00",
          content: stablePrototypeJson(expenseApproval).replace('"schemaVersion": 1', '"schemaVersion": 2'),
        }),
      ),
    );
    renderPage();

    expect(await screen.findByText("This prototype can't be shown")).toBeInTheDocument();
    expect(screen.getByText("UNSUPPORTED_VERSION")).toBeInTheDocument();
    expect(application()).toBeNull();
    expect(screen.queryByText("Prototype · read-only")).toBeNull();
  });

  it("names a component mismatch as such", async () => {
    server.use(
      http.get(FILE, () =>
        HttpResponse.json({
          path: "specs/design/components/storefront/prototype.json",
          sha: "f00",
          content: stablePrototypeJson(expenseApproval),
        }),
      ),
    );
    renderPage();
    expect(await screen.findByText("PROTOTYPE_COMPONENT_MISMATCH")).toBeInTheDocument();
    expect(application()).toBeNull();
  });

  it("explains a failed read and offers a retry", async () => {
    server.use(http.get(FILE, () => HttpResponse.json({ code: "internal", message: "git is down" }, { status: 500 })));
    renderPage();

    expect(await screen.findByText("Couldn't load the prototype")).toBeInTheDocument();
    expect(screen.getByText("git is down")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(application()).toBeNull();
  });

  it("says so when the component has no prototype", async () => {
    renderPage("catalog-api");
    expect(await screen.findByText("catalog-api has no prototype yet.")).toBeInTheDocument();
    expect(application()).toBeNull();
  });
});

// #818: the design moved after the prototype was generated.
describe("ComponentPrototypePage — outdated", () => {
  it("shows no banner while the prototype is current", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "Approval queue", level: 2 });
    expect(screen.queryByText("Outdated")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Regenerate prototype" })).not.toBeInTheDocument();
  });

  it("shows an Outdated banner whose Regenerate prototype sends /prototype and returns to the Spec", async () => {
    localStorage.setItem("aep:mock:prototype", "outdated");
    renderPage();
    await screen.findByRole("heading", { name: "Approval queue", level: 2 });

    expect(await screen.findByText("Outdated")).toBeInTheDocument();
    expect(screen.getByText(/The design has changed since this prototype was generated/)).toBeInTheDocument();
    // The prototype stays reviewable underneath.
    expect(application()).not.toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Regenerate prototype" }));
    expect(consumePendingSeed(chatKeyFor("acme", "demo-shop"))).toEqual({ message: "/prototype", guarded: true });
    expect(mockNavigate).toHaveBeenCalledWith({ to: "/projects/$projectName/spec", params: { projectName: "demo-shop" } });
  });
});

// #817: Annotate queues requests; one Send all posts them as ONE /prototype
// turn; the prototype refreshes once when that turn completes.
describe("ComponentPrototypePage — feedback", () => {
  async function annotate() {
    const reads: string[] = [];
    const posts: unknown[] = [];
    server.events.on("request:start", ({ request }) => {
      if (request.method === "GET" && request.url.includes("/prototype.json")) reads.push(request.url);
    });
    server.use(
      http.post("*/api/v1/projects/:projectName/agents/:conversationId/messages", async ({ request }) => {
        posts.push(await request.clone().json());
        return undefined; // fall through to the mock turn handler
      }),
    );
    renderPage();
    await screen.findByRole("heading", { name: "Approval queue", level: 2 });
    fireEvent.click(screen.getByRole("button", { name: "Annotate" }));
    const inspector = await screen.findByRole("complementary", { name: "Feedback" });
    return { reads, posts, inspector };
  }

  function queue(inspector: HTMLElement, text: string) {
    fireEvent.change(within(inspector).getByRole("textbox", { name: "Request" }), { target: { value: text } });
    fireEvent.click(within(inspector).getByRole("button", { name: "Add request" }));
  }

  afterEach(() => server.events.removeAllListeners());

  it("sends the queue as one /prototype turn, refreshes once on completion, and clears the queue", async () => {
    const { reads, posts, inspector } = await annotate();
    fireEvent.click(document.querySelector('[data-prototype-component-id="btn.export"]')!);
    queue(inspector, "Rename to Download");
    queue(inspector, "Too busy");
    expect(within(inspector).getAllByRole("listitem")).toHaveLength(2);
    const readsBefore = reads.length;

    fireEvent.click(within(inspector).getByRole("button", { name: "Send all (2)" }));

    await waitFor(() => expect(within(inspector).queryAllByRole("listitem")).toHaveLength(0));
    await waitFor(() => expect(reads.length).toBe(readsBefore + 1));
    expect(posts).toHaveLength(1);
    expect(posts[0]).toMatchObject({
      instruction: "/prototype",
      prototypeFeedback: {
        prototypePath: "specs/design/components/storefront/prototype.json",
        annotations: [
          { screenId: "screen.queue", componentIds: ["btn.export"], request: "Rename to Download" },
          { screenId: "screen.queue", componentIds: [], request: "Too busy" },
        ],
      },
    });
    // The turn is in the project's chat like any other.
    expect(getMessages(chatKeyFor("acme", "demo-shop")).some((m) => m.role === "user" && m.content === "/prototype")).toBe(true);
    // Settled: nothing else refreshes it.
    await waitFor(() => expect(hasLocalTurnActivity(chatKeyFor("acme", "demo-shop"))).toBe(false));
    expect(reads.length).toBe(readsBefore + 1);
  });

  it("keeps the queue and refreshes nothing when the turn fails", async () => {
    const { reads, inspector } = await annotate();
    queue(inspector, "please fail");
    const readsBefore = reads.length;
    fireEvent.click(within(inspector).getByRole("button", { name: "Send all (1)" }));

    expect(await within(inspector).findByText(/still queued/)).toBeInTheDocument();
    expect(within(inspector).getAllByRole("listitem")).toHaveLength(1);
    expect(reads.length).toBe(readsBefore);
  });
});

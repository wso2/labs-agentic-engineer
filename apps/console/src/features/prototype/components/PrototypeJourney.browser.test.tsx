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

// The reviewer's whole journey through the prototype (#819), in a real
// browser with real input, against the console's own mock layer served by a
// real MSW service worker: the Spec rail's Review prototype entry opens the
// review page on its real route, Preview navigates (side nav, row → detail, a
// dialog), Annotate selects two components, one component request and one
// whole-screen request are queued, and ONE Send all posts both as one
// /prototype turn — after which the prototype is read exactly once more and
// the queue is empty.
//
// The rail is SpecFileList, wired to the route the way SpecView wires it; the
// rest of SpecView (collab, file panes) is not what this journey is about.

import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import { page, userEvent } from "vitest/browser";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useNavigate,
} from "@tanstack/react-router";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setupWorker } from "msw/browser";
import { http } from "msw";
import createClient from "openapi-fetch";
import type { paths } from "../../../generated/aep-api";
import { agentChatHandlers } from "../../../mocks/handlers/agent-chat";
import { projectHandlers } from "../../../mocks/handlers/project";
import { railSections } from "../../spec/lib/railSections";
import { chatKeyFor, hasLocalTurnActivity, replaceMessages } from "../../agent-chat/chatStore";

vi.mock("../../../api/client", () => ({
  // Same origin as the page, so the service worker answers; no auth wrapper.
  client: createClient<paths>({ baseUrl: `${location.origin}/api/v1`, fetch: (req) => globalThis.fetch(req) }),
}));
vi.mock("../../../auth/SessionContext", () => ({
  useSession: () => ({ orgHandle: "acme" }),
}));
vi.mock("../../agent-chat/currentUser", () => ({
  useCurrentAuthor: () => ({ id: "ann@example.com", displayName: "Ann" }),
}));

const { SpecFileList } = await import("../../spec/components/SpecFileList");
const { Route: prototypeRoute } = await import("../../../routes/projects.$projectName_.prototype.$component");

const PROJECT = "demo-shop";
const COMPONENT = "storefront";
const PROTOTYPE_PATH = `specs/design/components/${COMPONENT}/prototype.json`;

const worker = setupWorker(...projectHandlers, ...agentChatHandlers);
beforeAll(async () => {
  await worker.start({ quiet: true, onUnhandledRequest: "error" });
});
afterAll(() => worker.stop());

/** The Spec rail's Prototype stage, navigating as SpecView does. */
function SpecRail() {
  const navigate = useNavigate();
  return (
    <SpecFileList
      files={[{ path: "specs/design/design.cell", sha: "sha", group: "designs" }]}
      selection={null}
      onSelect={() => {}}
      onRegenerateDesign={() => {}}
      sections={railSections({
        hasRequirements: true,
        hasDesign: true,
        hasValidation: true,
        agentWorking: false,
        agentFlow: "",
        designOutdated: false,
        assumptions: 0,
        openQuestions: 0,
        planEntries: [],
        planWreckage: false,
        webApplications: [COMPONENT],
        prototypes: [COMPONENT],
        prototypeOutdated: false,
      })}
      onReason={() => {}}
      onPrototypeAction={() => {}}
      onReviewPrototype={(component) =>
        void navigate({
          to: "/projects/$projectName/prototype/$component",
          params: { projectName: PROJECT, component },
        })
      }
    />
  );
}

/** A router holding the Spec page and the REAL review route. */
function renderApp() {
  const rootRoute = createRootRoute({ component: Outlet });
  const specRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/projects/$projectName/spec",
    component: SpecRail,
  });
  // Re-parent the real file route under this root, as the generated route
  // tree does under the app's.
  prototypeRoute.update({
    id: "/projects/$projectName_/prototype/$component",
    path: "/projects/$projectName/prototype/$component",
    getParentRoute: () => rootRoute,
  } as unknown as Parameters<typeof prototypeRoute.update>[0]);
  const router = createRouter({
    routeTree: rootRoute.addChildren([specRoute, prototypeRoute]),
    history: createMemoryHistory({ initialEntries: [`/projects/${PROJECT}/spec`] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </OxygenUIThemeProvider>,
  );
  return router;
}

const byId = (id: string) => document.querySelector<HTMLElement>(`[data-prototype-component-id="${id}"]`)!;

beforeEach(async () => {
  await page.viewport(1440, 900);
  localStorage.clear();
  replaceMessages(chatKeyFor("acme", PROJECT), []);
});
afterEach(() => {
  worker.events.removeAllListeners();
  worker.resetHandlers();
  cleanup();
});

describe("the prototype review journey", () => {
  it("opens from the Spec rail, previews, annotates two components, and sends both requests as one turn that refreshes once", async () => {
    const reads: string[] = [];
    const posts: unknown[] = [];
    worker.events.on("request:start", ({ request }) => {
      if (request.method === "GET" && request.url.endsWith(`/files/${PROTOTYPE_PATH}`)) reads.push(request.url);
    });
    worker.use(
      http.post("*/api/v1/projects/:projectName/agents/:conversationId/messages", async ({ request }) => {
        posts.push(await request.clone().json());
        return undefined; // fall through to the mock turn handler
      }),
    );
    const router = renderApp();

    // Spec rail → Review prototype.
    await userEvent.click(await screen.findByRole("button", { name: `Review prototype: ${COMPONENT}` }));
    expect(await screen.findByRole("heading", { name: "Approval queue", level: 2 })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe(`/projects/${PROJECT}/prototype/${COMPONENT}`);
    expect(reads).toHaveLength(1);

    // Preview: a row opens the detail screen, and the URL follows.
    await userEvent.click(byId("expense.1042"));
    expect(await screen.findByRole("heading", { name: "Expense #1042 · Maya Fernando" })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toMatchObject({ screen: "screen.detail" }));

    // A dialog opens and closes.
    await userEvent.click(within(byId("btn.approve")).getByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog", { name: "Approve expense #1042?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Approve expense #1042?" })).toBeNull());

    // The side nav returns to the queue.
    await userEvent.click(within(byId("nav.main")).getByRole("button", { name: "Approval queue" }));
    expect(await screen.findByRole("heading", { name: "Approval queue", level: 2 })).toBeInTheDocument();
    await waitFor(() => expect(router.state.location.search).toMatchObject({ screen: "screen.queue" }));

    // Annotate: two components, one request on them; then one on the whole screen.
    await userEvent.click(screen.getByRole("button", { name: "Annotate" }));
    const inspector = await screen.findByRole("complementary", { name: "Feedback" });
    const request = () => within(inspector).getByRole("textbox", { name: "Request" });
    await userEvent.click(byId("btn.export"));
    await userEvent.click(byId("stat.overdue"));
    await userEvent.fill(request(), "Call Export Download CSV, and link Overdue to the filtered queue.");
    await userEvent.click(within(inspector).getByRole("button", { name: "Add request" }));
    await userEvent.keyboard("{Escape}");
    await userEvent.fill(request(), "The screen needs a filter by department.");
    await userEvent.click(within(inspector).getByRole("button", { name: "Add request" }));
    expect(within(inspector).getAllByRole("listitem")).toHaveLength(2);
    expect(reads).toHaveLength(1);

    // One Send all.
    await userEvent.click(within(inspector).getByRole("button", { name: "Send all (2)" }));

    await waitFor(() => expect(within(inspector).queryAllByRole("listitem")).toHaveLength(0), { timeout: 10_000 });
    await waitFor(() => expect(reads).toHaveLength(2));
    expect(posts).toHaveLength(1);
    expect(posts[0]).toMatchObject({
      instruction: "/prototype",
      prototypeFeedback: {
        prototypePath: PROTOTYPE_PATH,
        annotations: [
          {
            screenId: "screen.queue",
            componentIds: ["btn.export", "stat.overdue"],
            request: "Call Export Download CSV, and link Overdue to the filtered queue.",
          },
          { screenId: "screen.queue", componentIds: [], request: "The screen needs a filter by department." },
        ],
      },
    });

    // Settled: the turn is over and nothing reads the prototype again.
    await waitFor(() => expect(hasLocalTurnActivity(chatKeyFor("acme", PROJECT))).toBe(false));
    expect(reads).toHaveLength(2);
    expect(posts).toHaveLength(1);
  });
});

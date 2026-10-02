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

import type { ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";

type AeStudioState = components["schemas"]["AeStudio"]["state"];

const BASE = "http://localhost/api/v1";

// The real typed client, minus the session: an absolute base the MSW server
// can match, and no auth wrapper (the gate never sees a 401). `fetch` is read
// per call, because openapi-fetch would otherwise capture the global before
// MSW patches it.
vi.mock("../../../api/client", async () => {
  const { default: createClient } = await import("openapi-fetch");
  return {
    client: createClient({ baseUrl: BASE, fetch: (request) => globalThis.fetch(request) }),
  };
});

// The gate reads only the pathname; the failed page links to Settings. A
// plain anchor and a settable path keep the RouterProvider out of the test.
let currentPath = "/";
vi.mock("@tanstack/react-router", () => ({
  useRouterState: ({ select }: { select: (s: { location: { pathname: string } }) => unknown }) =>
    select({ location: { pathname: currentPath } }),
  Link: ({ to, children, ...rest }: { to: string; children?: ReactNode }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}));

const { AeStudioGate } = await import("./AeStudioGate");
const { AeStudioBanner } = await import("./AeStudioBanner");
const { aeStudioKeys } = await import("../api/queries");
const { useConnectGitHubPat, useDisconnectGitProvider, useSaveAiSettings } = await import(
  "../../settings/api/queries"
);

const server = setupServer();

// Answers GET /ae-studio with each state in turn, then keeps answering the
// last one.
function mockAeStudio(states: AeStudioState[]) {
  let call = 0;
  server.use(
    http.get(`${BASE}/ae-studio`, () => {
      const state = states[Math.min(call++, states.length - 1)];
      return HttpResponse.json(
        state === "ready"
          ? {
              state,
              urls: {
                designAgent: "http://ae-design-agent.mock",
                collab: "ws://ae-collab.mock",
                tools: "http://ae-studio-tools.mock",
              },
            }
          : { state },
      );
    }),
  );
}

// The client of the latest render, so a test can drive a refetch.
let queryClient: QueryClient;

function renderWithProviders(ui: ReactNode, { route = "/" }: { route?: string } = {}) {
  currentPath = route;
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    </OxygenUIThemeProvider>,
  );
}

// One provisioning poll interval (2 s) on the fake clock, plus the answer.
async function pollOnce() {
  await act(() => vi.advanceTimersByTimeAsync(2000));
}

// Resolves once the first GET /ae-studio has answered. The console shows while
// it is in flight, so a refetch before this would cancel the first read.
async function firstAnswer() {
  await waitFor(() => expect(queryClient.getQueryData(aeStudioKeys.all)).toBeDefined());
}

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  vi.useRealTimers();
  cleanup();
  queryClient.clear();
  server.resetHandlers();
});
afterAll(() => server.close());

describe("AeStudioGate", () => {
  it("holds the whole console on the session's first provisioning, then shows it at ready", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["provisioning", "provisioning", "ready"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    expect(await screen.findByText("Upgrading AE Studio")).toBeInTheDocument();
    expect(screen.getByText("This takes a minute or two.")).toBeInTheDocument();
    expect(screen.queryByText("console")).not.toBeInTheDocument();
    await pollOnce();
    expect(screen.getByText("Upgrading AE Studio")).toBeInTheDocument();
    await pollOnce();
    expect(await screen.findByText("console")).toBeInTheDocument();
  });

  it("a first provisioning that fails ends the hold on the failed page", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["provisioning", "failed"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>, { route: "/projects" });
    expect(await screen.findByText("Upgrading AE Studio")).toBeInTheDocument();
    await pollOnce();
    expect(await screen.findByText("AE Studio couldn't start")).toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
  });

  it("a later provisioning shows a banner, not a hold", async () => {
    mockAeStudio(["ready", "provisioning"]);
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>);
    expect(await screen.findByText("console")).toBeInTheDocument();
    await firstAnswer();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    await act(() => queryClient.refetchQueries({ queryKey: aeStudioKeys.all }));
    expect(await screen.findByText("AE Studio is restarting…")).toBeInTheDocument();
    expect(screen.getByText("console")).toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
  });

  it("absent never holds", async () => {
    mockAeStudio(["absent"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    expect(await screen.findByText("console")).toBeInTheDocument();
    await waitFor(() => expect(queryClient.getQueryData(aeStudioKeys.all)).toEqual({ state: "absent" }));
    expect(screen.getByText("console")).toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
  });

  it("failed is a full page with Try again, but Settings stays reachable", async () => {
    mockAeStudio(["failed"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>, { route: "/projects" });
    expect(await screen.findByText("AE Studio couldn't start")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Settings" })).toHaveAttribute("href", "/settings");
    expect(screen.queryByText("console")).not.toBeInTheDocument();
    cleanup();
    renderWithProviders(<AeStudioGate><div>settings</div></AeStudioGate>, { route: "/settings/credentials" });
    expect(await screen.findByText("settings")).toBeInTheDocument();
  });
});

// Each org config write rolls AE Studio. The write must re-read its state, or
// the cached `ready` stands for up to 30 s and the restart is never shown.
describe("after the user's own config write", () => {
  function SaveConnection() {
    const save = useSaveAiSettings();
    return <button onClick={() => save.mutate({ agents: { runtime: "claude-code" } })}>write</button>;
  }
  function SubmitToken() {
    const connect = useConnectGitHubPat();
    return <button onClick={() => connect.mutate({ pat: "test-token" })}>write</button>;
  }
  function Disconnect() {
    const disconnect = useDisconnectGitProvider();
    return <button onClick={() => disconnect.mutate()}>write</button>;
  }
  const writes = {
    "a model connection save": SaveConnection,
    "a GitHub token submit": SubmitToken,
    "a GitHub disconnect": Disconnect,
  };

  it.each(Object.keys(writes) as (keyof typeof writes)[])(
    "%s re-reads AE Studio, so a provisioning answer shows the restart banner",
    async (write) => {
      mockAeStudio(["ready", "provisioning"]);
      server.use(
        http.patch(`${BASE}/config`, () => HttpResponse.json({})),
        http.post(`${BASE}/config/git-provider/disconnect`, () => new HttpResponse(null, { status: 204 })),
        http.get(`${BASE}/config`, () => HttpResponse.json({})),
      );
      const Writer = writes[write];
      renderWithProviders(
        <AeStudioGate>
          <AeStudioBanner />
          <Writer />
        </AeStudioGate>,
      );
      await firstAnswer();
      expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "write" }));
      expect(await screen.findByText("AE Studio is restarting…")).toBeInTheDocument();
    },
  );
});

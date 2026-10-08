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

type AeStudio = components["schemas"]["AeStudio"];
type AeStudioState = AeStudio["state"];

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

const { AeStudioGate, AE_STUDIO_HOLD_CAP_MS } = await import("./AeStudioGate");
const { AeStudioBanner } = await import("./AeStudioBanner");
const { aeStudioKeys } = await import("../api/queries");
const { AeStudioNotReadyError, PodRequestError, designAgent, designAgentCall, setAeStudioUrls } = await import(
  "../../../api/aeStudio"
);
const { ApiRequestError } = await import("../../../api/errors");
const { useConnectGitHubPat, useDisconnectGitProvider, useSaveAiSettings } = await import(
  "../../settings/api/queries"
);

const server = setupServer();

// Answers GET /ae-studio with each state (or whole answer) in turn, then keeps
// answering the last one. Returns how many reads it has answered.
function mockAeStudio(states: (AeStudioState | AeStudio)[]) {
  let call = 0;
  server.use(
    http.get(`${BASE}/ae-studio`, () => {
      const next = states[Math.min(call++, states.length - 1)];
      if (typeof next !== "string") return HttpResponse.json(next);
      const state = next;
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
  return () => call;
}

const SLOW_START_TITLE = "Starting AE Studio is taking longer than usual.";
const SLOW_START_DETAIL =
  "This often means the cluster is short on room. It keeps trying by itself; if this lasts, contact your platform administrator.";

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
  setAeStudioUrls(null);
});
afterAll(() => server.close());

describe("AeStudioGate", () => {
  it("holds the whole console on the session's first provisioning, then shows it at ready", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["provisioning", "provisioning", "ready"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    expect(await screen.findByText("Upgrading AE Studio")).toBeInTheDocument();
    // A first install on a busy cluster can take far longer than a minute or
    // two, so the hold names no duration.
    expect(screen.queryByText(/minute|second|hour/i)).not.toBeInTheDocument();
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

  it("Settings stays reachable during the first-visit hold", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["provisioning"]);
    renderWithProviders(<AeStudioGate><div>settings</div></AeStudioGate>, { route: "/settings" });
    await firstAnswer();
    expect(await screen.findByText("settings")).toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
  });

  it("a hold past its cap gives way to the console and the starting banner while GET still says provisioning", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["provisioning"]);
    renderWithProviders(
      <AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>,
      { route: "/projects" },
    );
    expect(await screen.findByText("Upgrading AE Studio")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(AE_STUDIO_HOLD_CAP_MS - 2000));
    expect(screen.getByText("Upgrading AE Studio")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(2000));
    expect(await screen.findByText("console")).toBeInTheDocument();
    expect(screen.getByText("AE Studio is starting…")).toBeInTheDocument();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    expect(screen.queryByText("AE Studio couldn't start")).not.toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
    mockAeStudio(["ready"]);
    await pollOnce();
    await waitFor(() => expect(screen.queryByText("AE Studio is starting…")).not.toBeInTheDocument());
    expect(screen.getByText("console")).toBeInTheDocument();
  });

  it("Try again from failed into provisioning shows the console with the starting banner, never the failed page again", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockAeStudio(["failed"]);
    renderWithProviders(
      <AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>,
      { route: "/projects" },
    );
    expect(await screen.findByText("AE Studio couldn't start")).toBeInTheDocument();
    mockAeStudio(["provisioning"]);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("console")).toBeInTheDocument();
    expect(screen.getByText("AE Studio is starting…")).toBeInTheDocument();
    // Every later provisioning poll keeps the console and the banner up.
    for (let i = 0; i < 3; i++) {
      await pollOnce();
      expect(screen.queryByText("AE Studio couldn't start")).not.toBeInTheDocument();
      expect(screen.getByText("AE Studio is starting…")).toBeInTheDocument();
    }
  });

  it("ready shows the console and no banner", async () => {
    mockAeStudio(["ready"]);
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>);
    await firstAnswer();
    expect(screen.getByText("console")).toBeInTheDocument();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    expect(screen.queryByText("AE Studio is starting…")).not.toBeInTheDocument();
    expect(screen.queryByText("AE Studio couldn't start")).not.toBeInTheDocument();
  });

  it("a failed read shows the console and is retried every 5 s", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let calls = 0;
    server.use(
      http.get(`${BASE}/ae-studio`, () => {
        calls++;
        return HttpResponse.json({ code: "internal_error", message: "boom" }, { status: 500 });
      }),
    );
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    expect(await screen.findByText("console")).toBeInTheDocument();
    await waitFor(() => expect(calls).toBe(1));
    await act(() => vi.advanceTimersByTimeAsync(4900));
    expect(calls).toBe(1);
    await act(() => vi.advanceTimersByTimeAsync(200));
    await waitFor(() => expect(calls).toBe(2));
    mockAeStudio(["ready"]);
    await act(() => vi.advanceTimersByTimeAsync(5000));
    await waitFor(() => expect(queryClient.getQueryData(aeStudioKeys.all)).toMatchObject({ state: "ready" }));
  });

  // The console is already up after a failed first read, so a provisioning
  // answer behind it is a restart to show, not a hold to pull over it.
  it("a failed first read then provisioning shows the starting banner, not the hold", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let calls = 0;
    server.use(
      http.get(`${BASE}/ae-studio`, () => {
        calls++;
        return HttpResponse.json({ code: "internal_error", message: "boom" }, { status: 500 });
      }),
    );
    renderWithProviders(
      <AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>,
      { route: "/projects" },
    );
    expect(await screen.findByText("console")).toBeInTheDocument();
    await waitFor(() => expect(calls).toBe(1));
    mockAeStudio(["provisioning"]);
    await act(() => vi.advanceTimersByTimeAsync(5000));
    await waitFor(() =>
      expect(queryClient.getQueryData(aeStudioKeys.all)).toMatchObject({ state: "provisioning" }),
    );
    expect(screen.getByText("console")).toBeInTheDocument();
    expect(screen.getByText("AE Studio is starting…")).toBeInTheDocument();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    expect(screen.queryByText("Upgrading AE Studio")).not.toBeInTheDocument();
  });

  it("a later provisioning, after ready, shows the restarting banner, not a hold", async () => {
    mockAeStudio(["ready", "provisioning"]);
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>);
    expect(await screen.findByText("console")).toBeInTheDocument();
    await firstAnswer();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    await act(() => queryClient.refetchQueries({ queryKey: aeStudioKeys.all }));
    expect(await screen.findByText("AE Studio is restarting…")).toBeInTheDocument();
    expect(screen.queryByText("AE Studio is starting…")).not.toBeInTheDocument();
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
    renderWithProviders(<AeStudioGate><div>settings</div></AeStudioGate>, { route: "/settings" });
    expect(await screen.findByText("settings")).toBeInTheDocument();
  });

  it.each([
    ["error", { state: "failed", reason: "error" }],
    ["no reason (an older server)", { state: "failed" }],
  ] as [string, AeStudio][])("failed with %s keeps the couldn't-start page and stops reading", async (_, answer) => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const reads = mockAeStudio([answer]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>, { route: "/projects" });
    expect(await screen.findByText("AE Studio couldn't start")).toBeInTheDocument();
    expect(screen.queryByText(SLOW_START_TITLE)).not.toBeInTheDocument();
    expect(screen.queryByText(/short on room/)).not.toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(5 * 60_000));
    expect(reads()).toBe(1);
  });

  // A timeout is a start the platform cannot explain: commonly no room in the
  // cluster, which frees up on its own. The page says so and keeps reading,
  // so it turns ready without a click.
  it("failed with timeout says it is taking longer than usual and re-reads every 30 s until ready", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const reads = mockAeStudio([{ state: "failed", reason: "timeout" }]);
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>, { route: "/projects" });
    expect(await screen.findByText(SLOW_START_TITLE)).toBeInTheDocument();
    expect(screen.getByText(SLOW_START_DETAIL)).toBeInTheDocument();
    expect(screen.queryByText("AE Studio couldn't start")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Settings" })).toHaveAttribute("href", "/settings");
    expect(screen.queryByText("console")).not.toBeInTheDocument();
    expect(reads()).toBe(1);
    await act(() => vi.advanceTimersByTimeAsync(29_000));
    expect(reads()).toBe(1);
    await act(() => vi.advanceTimersByTimeAsync(1_000));
    await waitFor(() => expect(reads()).toBe(2));
    expect(screen.getByText(SLOW_START_TITLE)).toBeInTheDocument();
    mockAeStudio(["ready"]);
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(await screen.findByText("console")).toBeInTheDocument();
    expect(screen.queryByText(SLOW_START_TITLE)).not.toBeInTheDocument();
    expect(screen.queryByText("AE Studio is restarting…")).not.toBeInTheDocument();
    expect(screen.queryByText("AE Studio is starting…")).not.toBeInTheDocument();
  });

  it("failed with timeout leaves Settings reachable", async () => {
    mockAeStudio([{ state: "failed", reason: "timeout" }]);
    renderWithProviders(<AeStudioGate><div>settings</div></AeStudioGate>, { route: "/settings" });
    await firstAnswer();
    expect(screen.getByText("settings")).toBeInTheDocument();
    expect(screen.queryByText(SLOW_START_TITLE)).not.toBeInTheDocument();
  });
});

// The design-agent client exists only while the latest answer is `ready`: set
// from that answer before any consumer sees it, cleared by any other answer.
describe("the design-agent accessor", () => {
  it("is set from a ready answer and cleared by a later provisioning one", async () => {
    mockAeStudio(["ready", "provisioning"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
    await firstAnswer();
    expect(() => designAgent()).not.toThrow();
    await act(() => queryClient.refetchQueries({ queryKey: aeStudioKeys.all }));
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
  });

  it("is never set from a failed answer", async () => {
    mockAeStudio(["failed"]);
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>, { route: "/projects" });
    await firstAnswer();
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
  });
});

// An AE Studio that stops answering is news about AE Studio, wherever the
// request went: the gate re-reads its state, so a restart shows as the banner
// instead of as broken panes.
describe("an AE Studio outage", () => {
  async function failARead(failure: unknown) {
    await act(() =>
      queryClient
        .fetchQuery({ queryKey: ["a-read"], queryFn: () => Promise.reject(failure), retry: false })
        .catch(() => undefined),
    );
  }

  it.each([
    ["a pod 503", new PodRequestError({ code: "shutting_down", title: "Service Unavailable" }, "x", { status: 503 })],
    ["no answer from the pod", new PodRequestError(undefined, "x", {})],
    ["aep-api's 503 ae_studio_unavailable", new ApiRequestError({ code: "ae_studio_unavailable", message: "AE Studio is restarting" }, "x", { retryAfterMs: 5000 })],
  ])("%s from a read re-reads AE Studio", async (_, failure) => {
    mockAeStudio(["ready", "provisioning"]);
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>);
    await firstAnswer();
    await failARead(failure);
    expect(await screen.findByText("AE Studio is restarting…")).toBeInTheDocument();
  });

  // The chat's calls are not queries: they reach the gate through the pod
  // client's outage channel.
  it("a design-agent 503 outside any query re-reads AE Studio", async () => {
    mockAeStudio(["ready", "provisioning"]);
    server.use(
      http.get("http://ae-design-agent.mock/v1/projects/p/turns/active", () =>
        HttpResponse.json({ type: "about:blank", title: "Service Unavailable", status: 503, code: "shutting_down" }, { status: 503 }),
      ),
    );
    renderWithProviders(<AeStudioGate><AeStudioBanner /><div>console</div></AeStudioGate>);
    await firstAnswer();
    await act(() =>
      designAgentCall("x", (agent) => agent.GET("/projects/{projectName}/turns/active", { params: { path: { projectName: "p" } } })),
    );
    expect(await screen.findByText("AE Studio is restarting…")).toBeInTheDocument();
  });

  it.each([
    ["a pod 404", new PodRequestError({ code: "turn_unknown", title: "Not Found" }, "x", { status: 404 })],
    ["any other aep-api refusal", new ApiRequestError({ code: "not_found", message: "no" }, "x")],
  ])("%s does not", async (_, failure) => {
    let calls = 0;
    server.use(
      http.get(`${BASE}/ae-studio`, () => {
        calls++;
        return HttpResponse.json({ state: "ready", urls: { designAgent: "d", collab: "c", tools: "http://t" } });
      }),
    );
    renderWithProviders(<AeStudioGate><div>console</div></AeStudioGate>);
    await firstAnswer();
    await failARead(failure);
    expect(queryClient.getQueryState(aeStudioKeys.all)?.isInvalidated).toBe(false);
    expect(calls).toBe(1);
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

  // The server wrote the settings, then failed a follow-up step: the roll is
  // under way all the same. A refusal before the write rolls nothing.
  it.each([
    ["agent_manager_not_updated", true],
    ["invalid_request", false],
  ] as const)("a model connection save refused with %s re-reads AE Studio: %s", async (code, reReads) => {
    let reads = 0;
    server.use(
      http.get(`${BASE}/ae-studio`, () => {
        reads++;
        return HttpResponse.json({ state: "ready", urls: { designAgent: "d", collab: "c", tools: "t" } });
      }),
      http.patch(`${BASE}/config`, () => HttpResponse.json({ code, message: "refused" }, { status: 502 })),
    );
    renderWithProviders(
      <AeStudioGate>
        <SaveConnection />
      </AeStudioGate>,
    );
    await firstAnswer();
    fireEvent.click(screen.getByRole("button", { name: "write" }));
    if (reReads) await waitFor(() => expect(reads).toBe(2));
    else {
      await act(() => new Promise((r) => setTimeout(r, 50)));
      expect(reads).toBe(1);
    }
  });
});


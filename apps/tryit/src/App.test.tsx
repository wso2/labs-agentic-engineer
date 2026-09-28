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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// The session is the one module that leaves the page; everything else renders.
const signIn = vi.fn(async () => {});
const handleCallback = vi.fn(async () => {});
let storedToken: string | null = null;
vi.mock("./session", () => ({
  createSession: () => ({
    signIn,
    handleCallback,
    accessToken: async () => storedToken,
    username: async () => (storedToken ? "test-engineer" : null),
    signOut: async () => {
      storedToken = null;
    },
  }),
}));

import { App } from "./App";

const launchHash =
  "#/agent?project=small-call-triage&component=incident-triage&issuer=http%3A%2F%2Fidp&client_id=k" +
  "&resource=r&scopes=openid+triage%3Ause&endpoint=http%3A%2F%2Fgw.openchoreoapis.localhost%2Fincident-triage-http";

beforeEach(() => {
  sessionStorage.clear();
  storedToken = null;
  signIn.mockClear();
  handleCallback.mockClear();
  window.history.replaceState(null, "", "/");
  vi.unstubAllGlobals();
});

describe("App", () => {
  it("explains itself when opened without a launch", () => {
    render(<App />);
    expect(screen.getByText("Open this app from the console")).toBeInTheDocument();
  });

  it("refuses to sign in when the launch names an endpoint outside the gateway allowlist", async () => {
    window.location.hash = launchHash.replace("http%3A%2F%2Fgw.openchoreoapis.localhost%2F", "https%3A%2F%2Fattacker.example%2F");
    render(<App />);

    expect(await screen.findByText(/will not send a token to/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sign in as a test user" })).toBeNull();
    expect(signIn).not.toHaveBeenCalled();
  });

  it("offers to sign in as a test user when launched, naming the agent", async () => {
    window.location.hash = launchHash;
    render(<App />);

    const button = await screen.findByRole("button", { name: "Sign in as a test user" });
    expect(screen.getByRole("heading", { name: "incident-triage" })).toBeInTheDocument();
    fireEvent.click(button);
    expect(signIn).toHaveBeenCalledOnce();
    // The launch survives the redirect the click starts.
    expect(sessionStorage.getItem("tryit:launch")).toContain("incident-triage");
  });

  it("finishes the callback, drops the code from the URL, and shows the agent", async () => {
    sessionStorage.setItem(
      "tryit:launch",
      JSON.stringify({
        project: "small-call-triage",
        component: "incident-triage",
        issuer: "http://idp",
        clientId: "k",
        resource: "r",
        scopes: ["openid"],
        endpoint: "http://gw.openchoreoapis.localhost/incident-triage-http",
      }),
    );
    window.history.replaceState(null, "", "/callback?code=abc&state=xyz");
    handleCallback.mockImplementationOnce(async () => {
      storedToken = "tok";
    });
    render(<App />);

    expect(await screen.findByText(/signed in as test-engineer/)).toBeInTheDocument();
    expect(handleCallback).toHaveBeenCalledOnce();
    expect(window.location.pathname).toBe("/");
    expect(window.location.search).toBe("");
  });

  it("sends a turn to the agent's gateway with the bearer and shows the reply and its tools", async () => {
    window.location.hash = launchHash;
    storedToken = "tok";
    const fetchMock = vi.fn(async () =>
      new Response(
        JSON.stringify({ conversationId: "c1", text: "Severity: SEV1", toolCalls: [{ toolName: "getOnCall" }] }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    render(<App />);

    const input = await screen.findByRole("textbox", { name: "Message" });
    fireEvent.change(input, { target: { value: "Payments is down for everyone." } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("Severity: SEV1")).toBeInTheDocument();
    expect(screen.getByText("called getOnCall")).toBeInTheDocument();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://gw.openchoreoapis.localhost/incident-triage-http/chat");
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer tok");
  });

  it("says so when the agent refuses the token", async () => {
    window.location.hash = launchHash;
    storedToken = "tok";
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));
    render(<App />);

    fireEvent.change(await screen.findByRole("textbox", { name: "Message" }), { target: { value: "hi" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText(/refused the test user's token/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Send" })).toBeDisabled());
  });
});

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

import { Profiler } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorBoundary } from "./ErrorBoundary";

// Throws on every render while the flag is up; renders its content once the
// flag drops. Module-scoped so a retry (a fresh mount of the same element)
// sees the CURRENT flag, the way a transient race clears between renders.
let failing = true;
function Flaky({ text = "content" }: { text?: string }) {
  if (failing) throw new Error("canvas exploded");
  return <div>{text}</div>;
}

beforeEach(() => {
  failing = true;
  vi.useFakeTimers();
  // React reports every caught throw through console.error; keep the run quiet
  // and let the tests assert the boundary's OWN record.
  vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("ErrorBoundary", () => {
  it("contains the throw to a fallback that names the section and records the stack", () => {
    render(
      <ErrorBoundary label="The architecture diagram">
        <Flaky />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(/The architecture diagram hit an error/);
    expect(console.error).toHaveBeenCalledWith(
      "[console] The architecture diagram failed to render",
      expect.objectContaining({ message: "canvas exploded" }),
      expect.any(String),
    );

    // Details are collapsed until asked for, then show the stack.
    expect(screen.queryByText(/canvas exploded/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByText(/Error: canvas exploded/)).toBeInTheDocument();
    expect(screen.getByText(/Component stack:[\s\S]*Flaky/)).toBeInTheDocument();
  });

  it("commits once per catch: componentDidCatch never re-renders the boundary", () => {
    // React DevTools' "force error" re-raises on the BOUNDARY each time it
    // renders, so a catch that setStates the boundary loops into "Maximum
    // update depth exceeded" and escapes to the router's catch. Guarded by
    // counting commits: a catch must cost exactly one.
    const onRender = vi.fn();
    render(
      <Profiler id="boundary" onRender={onRender}>
        <ErrorBoundary label="The chat panel">
          <Flaky />
        </ErrorBoundary>
      </Profiler>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
    expect(onRender).toHaveBeenCalledTimes(1);
  });

  it("retries on its own after a wait, and stops once the attempts run out", () => {
    render(
      <ErrorBoundary label="The chat panel">
        <Flaky />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);

    // First attempt (2s) fails again — still retrying.
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);

    // Second attempt (5s) fails again — the boundary gives up and says so.
    act(() => vi.advanceTimersByTime(5_000));
    expect(screen.getByRole("alert")).not.toHaveTextContent(/Retrying automatically/);
    expect(screen.getByRole("alert")).toHaveTextContent(/Try again, or reload/);
    act(() => vi.advanceTimersByTime(60_000));
    expect(screen.getByRole("alert")).toBeInTheDocument();
    // Three renders were caught: mount + two retries. Not a fourth.
    expect(console.error).toHaveBeenCalledTimes(3 * 2); // boundary log + React's report, per catch
  });

  it("recovers when the automatic retry finds the race cleared", () => {
    render(
      <ErrorBoundary label="The chat panel">
        <Flaky />
      </ErrorBoundary>,
    );
    failing = false;
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByText("content")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("gives the automatic attempts back once a recovered section has stayed up", () => {
    const { rerender } = render(
      <ErrorBoundary label="The chat panel">
        <Flaky />
      </ErrorBoundary>,
    );
    // The first attempt recovers: one attempt spent.
    failing = false;
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByText("content")).toBeInTheDocument();

    // Stays up past the settle window, then fails again for an unrelated
    // reason: it must get the full two attempts, not the one left over.
    act(() => vi.advanceTimersByTime(10_000));
    failing = true;
    rerender(
      <ErrorBoundary label="The chat panel">
        <Flaky text="again" />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
    act(() => vi.advanceTimersByTime(2_000));
    // Still retrying after the first wait: the second attempt was available.
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
  });

  it("does not give attempts back while the section keeps failing inside the settle window", () => {
    const { rerender } = render(
      <ErrorBoundary label="The chat panel">
        <Flaky />
      </ErrorBoundary>,
    );
    failing = false;
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByText("content")).toBeInTheDocument();
    // Fails again before the window closes: same run, one attempt left.
    act(() => vi.advanceTimersByTime(3_000));
    failing = true;
    rerender(
      <ErrorBoundary label="The chat panel">
        <Flaky text="again" />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
    act(() => vi.advanceTimersByTime(5_000));
    expect(screen.getByRole("alert")).not.toHaveTextContent(/Retrying automatically/);
  });

  it("new input inside the settle window also gives the attempts back", () => {
    const { rerender } = render(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-1">
        <Flaky />
      </ErrorBoundary>,
    );
    // Recovers on the first attempt, so one is spent and the settle window is open.
    failing = false;
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByText("content")).toBeInTheDocument();

    // A new scene lands before the window closes, then fails: full attempts.
    rerender(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-2">
        <Flaky text="scene two" />
      </ErrorBoundary>,
    );
    failing = true;
    rerender(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-3">
        <Flaky />
      </ErrorBoundary>,
    );
    act(() => vi.advanceTimersByTime(2_000));
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
  });

  it("clears on new input via resetKey, with the automatic attempts restored", () => {
    const { rerender } = render(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-1">
        <Flaky />
      </ErrorBoundary>,
    );
    // Exhaust the automatic attempts on the first scene.
    act(() => vi.advanceTimersByTime(2_000));
    act(() => vi.advanceTimersByTime(5_000));
    expect(screen.getByRole("alert")).not.toHaveTextContent(/Retrying automatically/);

    // A new scene arrives and renders fine.
    failing = false;
    rerender(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-2">
        <Flaky text="scene two" />
      </ErrorBoundary>,
    );
    expect(screen.getByText("scene two")).toBeInTheDocument();

    // And a later throw on the new scene gets its own automatic attempts.
    failing = true;
    rerender(
      <ErrorBoundary label="The architecture diagram" resetKey="scene-3">
        <Flaky />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(/Retrying automatically/);
  });

  it("says what the caller wants once the attempts are spent, and can offer a reload", () => {
    // The app-level boundary: nothing above it can recover, so the exhausted
    // line hands the reader to an administrator and a reload is on offer.
    const reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
    try {
      render(
        <ErrorBoundary label="The console" exhaustedMessage="Unable to recover. Please contact your administrator." offerReload>
          <Flaky />
        </ErrorBoundary>,
      );
      expect(screen.queryByText(/Unable to recover/)).not.toBeInTheDocument();
      act(() => vi.advanceTimersByTime(2_000));
      act(() => vi.advanceTimersByTime(5_000));
      expect(screen.getByRole("alert")).toHaveTextContent(/Unable to recover\. Please contact your administrator\./);

      fireEvent.click(screen.getByRole("button", { name: "Reload page" }));
      expect(reload).toHaveBeenCalledTimes(1);

      // Manual retry still works after the automatic attempts are spent.
      failing = false;
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      expect(screen.getByText("content")).toBeInTheDocument();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("offers no reload for a section", () => {
    render(
      <ErrorBoundary label="The page">
        <Flaky />
      </ErrorBoundary>,
    );
    expect(screen.queryByRole("button", { name: "Reload page" })).not.toBeInTheDocument();
  });

  it("Try again re-renders the children on demand", () => {
    render(
      <ErrorBoundary label="The page">
        <Flaky />
      </ErrorBoundary>,
    );
    failing = false;
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(screen.getByText("content")).toBeInTheDocument();
  });

  it("cancels a pending retry when unmounted", () => {
    const { unmount } = render(
      <ErrorBoundary label="The page">
        <Flaky />
      </ErrorBoundary>,
    );
    unmount();
    // No state update on an unmounted component — nothing to assert but the
    // absence of a React warning, which the console.error spy would receive.
    const before = (console.error as ReturnType<typeof vi.fn>).mock.calls.length;
    act(() => vi.advanceTimersByTime(10_000));
    expect(console.error).toHaveBeenCalledTimes(before);
  });
});

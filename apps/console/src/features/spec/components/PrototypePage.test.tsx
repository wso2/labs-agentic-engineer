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

import { useEffect } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PrototypePage } from "./PrototypePage";

// Router replaced so the back-link renders as a plain anchor — no
// RouterProvider needed (mirrors ValidationPage.test.tsx / WireframePanel.test.tsx).
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: React.ReactNode }) => <a>{children}</a>,
}));

// Counts how many times the mocked PrototypeView actually MOUNTS (as opposed
// to just re-rendering with new props) — React strips `key` from props, so
// this is the only reliable way to assert PrototypePage honors PrototypeView's
// remount-on-`key` contract from JS assertions alone.
let prototypeMountCount = 0;

// The heavy lazy canvas is irrelevant here — record what model/initialScreen it receives.
vi.mock("@aep/ui-excalidraw-view", () => ({
  PrototypeView: (p: { model: { screens: unknown[] }; initialScreen?: string; initialFlow?: string }) => {
    useEffect(() => {
      prototypeMountCount += 1;
    }, []);
    return (
      <div
        data-testid="prototype"
        data-initial={p.initialScreen ?? ""}
        data-flow={p.initialFlow ?? ""}
      />
    );
  },
}));

const mockFiles = vi.fn();
vi.mock("../api/queries", () => ({
  useSpecFiles: (...args: unknown[]) => mockFiles(...args),
}));

const mockDerivedPrototype = vi.fn();
vi.mock("../api/useDerivedDesign", () => ({
  useDerivedPrototype: (...args: unknown[]) => mockDerivedPrototype(...args),
}));

const FILES = [
  { path: "specs/design/components/shop/wireframes.dsl", sha: "abc", group: "designs" as const },
];
const MODEL = {
  screens: [{ name: "Login", width: 1280, height: 800, sceneJson: "{}", hotspots: [] }],
  flows: [{ name: "Admin path", screens: ["Login"] }],
};

beforeEach(() => {
  mockFiles.mockReset();
  mockDerivedPrototype.mockReset();
  prototypeMountCount = 0;
});

describe("PrototypePage", () => {
  it("resolves the component's wireframe and renders the prototype with the deep-linked screen", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: MODEL, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        screen="Login"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId("prototype")).toHaveAttribute("data-initial", "Login");
  });

  it("explains when the component has no wireframes", () => {
    mockFiles.mockReturnValue({ data: [], isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: null, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/no wireframes/i)).toBeInTheDocument();
  });

  it("shows a spinner while the spec files are loading", () => {
    mockFiles.mockReturnValue({ data: undefined, isPending: true, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: null, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByLabelText(/loading/i)).toBeInTheDocument();
  });

  it("shows an error alert when the spec files fail to load", () => {
    mockFiles.mockReturnValue({ data: undefined, isPending: false, isError: true });
    mockDerivedPrototype.mockReturnValue({ model: null, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it("shows a spinner while the prototype model is deriving", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: null, isPending: true, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByLabelText(/loading/i)).toBeInTheDocument();
  });

  it("explains when the wireframe could not be rendered as a prototype", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: null, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/could not be rendered/i)).toBeInTheDocument();
  });

  it("remounts PrototypeView (not just re-renders) when a new wireframe commit changes the sha", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: MODEL, isPending: false, isError: false });
    const { rerender } = render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(prototypeMountCount).toBe(1);

    // A new commit lands: same path, new sha, new (derived) model instance.
    mockFiles.mockReturnValue({
      data: [{ ...FILES[0], sha: "def" }],
      isPending: false,
      isError: false,
    });
    mockDerivedPrototype.mockReturnValue({
      model: { screens: [...MODEL.screens], flows: [...MODEL.flows] },
      isPending: false,
      isError: false,
    });
    rerender(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );

    expect(prototypeMountCount).toBe(2);
  });

  it("passes the deep-linked flow through to the prototype", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: MODEL, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        flow="Admin path"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId("prototype")).toHaveAttribute("data-flow", "Admin path");
  });

  it("omits the flow when the link carries none, letting the viewer pick the first", () => {
    mockFiles.mockReturnValue({ data: FILES, isPending: false, isError: false });
    mockDerivedPrototype.mockReturnValue({ model: MODEL, isPending: false, isError: false });
    render(
      <PrototypePage
        projectName="p"
        component="shop"
        onScreenChange={vi.fn()}
        onFlowChange={vi.fn()}
      />,
    );
    expect(screen.getByTestId("prototype")).toHaveAttribute("data-flow", "");
  });
});

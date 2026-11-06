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

import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { compileProject } from "../compiler/compileProject";

export const fitViewSpy = vi.fn();
export const zoomInSpy = vi.fn();
export const zoomOutSpy = vi.fn();

vi.mock("@xyflow/react", async () => {
  const actual = await vi.importActual<typeof import("@xyflow/react")>("@xyflow/react");
  return {
    ...actual,
    useReactFlow: () => ({ fitView: fitViewSpy, zoomIn: zoomInSpy, zoomOut: zoomOutSpy }),
    useViewport: () => ({ x: 0, y: 0, zoom: 1 })
  };
});

import { buildFitPadding, DiagramCanvas } from "./DiagramCanvas";

function buildModel(source: string) {
  const compiled = compileProject(source);
  if (!compiled.model) {
    throw new Error("expected a valid model");
  }
  return compiled.model;
}

/** jsdom reports no layout, so give the canvas the pane size React Flow will measure. */
function withPaneSize(width: number, height: number, run: () => void) {
  const proto = HTMLElement.prototype;
  const prevWidth = Object.getOwnPropertyDescriptor(proto, "offsetWidth");
  const prevHeight = Object.getOwnPropertyDescriptor(proto, "offsetHeight");
  Object.defineProperty(proto, "offsetWidth", { configurable: true, get: () => width });
  Object.defineProperty(proto, "offsetHeight", { configurable: true, get: () => height });
  try {
    run();
  } finally {
    if (prevWidth) {
      Object.defineProperty(proto, "offsetWidth", prevWidth);
    }
    if (prevHeight) {
      Object.defineProperty(proto, "offsetHeight", prevHeight);
    }
  }
}

describe("DiagramCanvas insets", () => {
  beforeEach(() => {
    fitViewSpy.mockClear();
    zoomInSpy.mockClear();
    zoomOutSpy.mockClear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("defaults to zero horizontal padding and keeps the full clearance on a tall pane", () => {
    const model = buildModel("component API service\nnorth -> API");
    withPaneSize(1600, 1200, () => {
      render(<DiagramCanvas model={model} />);
    });

    expect(fitViewSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ top: "112px", bottom: "112px", left: "0px", right: "0px" })
      })
    );
  });

  it("adds left breathing room on top of the open editor inset", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(<DiagramCanvas model={model} insets={{ left: 260, right: 220 }} />);

    expect(fitViewSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ left: "372px", right: "220px" })
      })
    );
  });

  it("re-fits with new padding when insets change", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { rerender } = render(<DiagramCanvas model={model} insets={{ left: 260, right: 0 }} />);
    fitViewSpy.mockClear();

    rerender(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} />);

    expect(fitViewSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ left: "0px", right: "0px" })
      })
    );
  });

  it("does not re-fit on a re-render where the model and insets are unchanged", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { rerender } = render(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} />);
    fitViewSpy.mockClear();

    rerender(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} />);

    expect(fitViewSpy).not.toHaveBeenCalled();
  });

  it("re-fits when the visible layout key changes", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { rerender } = render(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} fitKey="mobile-code" />);
    fitViewSpy.mockClear();

    rerender(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} fitKey="mobile-diagram" />);

    expect(fitViewSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ left: "0px", right: "0px" })
      })
    );
  });

  it("schedules a post-paint re-fit when the visible layout key changes", () => {
    const model = buildModel("component API service\nnorth -> API");
    let postPaintFit: FrameRequestCallback | undefined;
    const requestAnimationFrameSpy = vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      postPaintFit = callback;
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);

    const { rerender } = render(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} fitKey="mobile-code" />);
    fitViewSpy.mockClear();
    requestAnimationFrameSpy.mockClear();

    rerender(<DiagramCanvas model={model} insets={{ left: 0, right: 0 }} fitKey="mobile-diagram" />);

    expect(requestAnimationFrameSpy).toHaveBeenCalledTimes(1);
    expect(fitViewSpy).toHaveBeenCalledTimes(1);

    postPaintFit?.(0);

    expect(fitViewSpy).toHaveBeenCalledTimes(2);
  });

  it("scales vertical fit padding with a short pane, including the fit button", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);

    const model = buildModel("component API service\nnorth -> API");
    // Issue #555: a 936×414 spec-view pane. 112px top and bottom leaves 190px
    // of usable height and fit-to-view clamps to minZoom.
    withPaneSize(936, 414, () => {
      render(<DiagramCanvas model={model} />);
      for (const frame of frames) {
        frame(0);
      }
      fireEvent.click(screen.getByRole("button", { name: "Fit diagram to view" }));
    });

    expect(fitViewSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ top: "50px", bottom: "50px", left: "0px", right: "0px" })
      })
    );
  });
});

/**
 * The zoom React Flow applies for a padding box. Mirrors `getViewportForBounds`:
 * pixel padding is floored, then the scale is the tighter axis, clamped to minZoom.
 */
function appliedFitZoom(
  pane: { width: number; height: number },
  bounds: { width: number; height: number },
  padding: { top: string; bottom: string; left: string; right: string },
  minZoom = 0.25,
  maxZoom = 1.35
) {
  const parse = (value: string, axis: number) => {
    if (value.endsWith("%")) {
      return Math.floor(axis * parseFloat(value) * 0.01);
    }
    return Math.floor(parseFloat(value));
  };
  const zoom = Math.min(
    (pane.width - parse(padding.left, pane.width) - parse(padding.right, pane.width)) / bounds.width,
    (pane.height - parse(padding.top, pane.height) - parse(padding.bottom, pane.height)) / bounds.height
  );
  return Math.min(maxZoom, Math.max(minZoom, zoom));
}

describe("buildFitPadding", () => {
  const insets = { left: 0, right: 0 };
  // Measured on the spec view for internal-expense-approval (issue #555).
  const pane = { width: 936, height: 414 };
  const bounds = { width: 1037, height: 984 };

  it("keeps the 112px clearance on a full workspace and before the pane is measured", () => {
    expect(buildFitPadding(insets).top).toBe("112px");
    expect(buildFitPadding(insets, false, 0).bottom).toBe("112px");
    expect(buildFitPadding(insets, false, 2000)).toEqual(
      expect.objectContaining({ top: "112px", bottom: "112px" })
    );
  });

  it("shrinks on a short pane so the reported diagram fits above minZoom", () => {
    const padding = buildFitPadding(insets, false, pane.height);

    expect(padding).toEqual({ top: "50px", bottom: "50px", left: "0px", right: "0px" });
    expect(appliedFitZoom(pane, bounds, { top: "112px", bottom: "112px", left: "0px", right: "0px" })).toBe(0.25);
    expect(appliedFitZoom(pane, bounds, padding)).toBeGreaterThan(0.3);
  });

  it("clamps the low end, then refuses to spend more than a fifth of a tiny pane", () => {
    expect(buildFitPadding(insets, false, 200).top).toBe("24px");
    expect(buildFitPadding(insets, false, 80).top).toBe("16px");
    expect(buildFitPadding(insets, false, 40).top).toBe("8px");
  });

  it("leaves a compact embed on its fixed sliver, and still adds the editor inset", () => {
    expect(buildFitPadding(insets, true, pane.height).top).toBe("16px");
    expect(buildFitPadding({ left: 260, right: 220 }, false, pane.height)).toEqual(
      expect.objectContaining({ top: "50px", bottom: "50px", left: "372px", right: "220px" })
    );
  });
});

describe("DiagramCanvas component styling", () => {
  it("does not mark API components with a special color class", () => {
    const model = buildModel("component books api");
    render(<DiagramCanvas model={model} />);

    expect(screen.getByText("books").closest(".component-node")).not.toHaveClass("component-node--api");
  });

  it("renders internal component subtype text inside the component circle", () => {
    const model = buildModel("component books api");
    render(<DiagramCanvas model={model} />);

    expect(screen.getByText("api").closest(".component-node")).toBeInTheDocument();
  });

  it("still renders boundary dependency subtype text inside external circles", () => {
    const model = buildModel("component books\neast users api\nbooks -> users");
    render(<DiagramCanvas model={model} />);

    expect(screen.getByText("api").closest(".external-node")).toBeInTheDocument();
  });
});

describe("DiagramCanvas live-edit motion", () => {
  it("keeps the initial graph still, then animates only a newly added circle", () => {
    const initialModel = buildModel("component API service");
    const { rerender } = render(<DiagramCanvas model={initialModel} motionContextKey="doc-1" />);

    expect(screen.getByText("API").closest(".react-flow__node")).not.toHaveClass("diagram-node--entering");

    const nextModel = buildModel("component API service\ncomponent Worker service");
    rerender(<DiagramCanvas model={nextModel} motionContextKey="doc-1" />);

    expect(screen.getByText("Worker").closest(".react-flow__node")).toHaveClass("diagram-node--entering");
    expect(screen.getByText("API").closest(".react-flow__node")).toHaveClass(
      "diagram-node--position-animated"
    );
  });

  it("updates labels quietly without replaying an entrance", () => {
    const { rerender } = render(
      <DiagramCanvas model={buildModel('component api as "Orders API" service')} motionContextKey="doc-1" />
    );

    rerender(
      <DiagramCanvas model={buildModel('component api as "Checkout API" service')} motionContextKey="doc-1" />
    );

    expect(screen.getByText("Checkout API").closest(".react-flow__node")).not.toHaveClass("diagram-node--entering");
  });

  it("updates an identifier on the same source line without replaying an entrance", () => {
    const { rerender } = render(
      <DiagramCanvas model={buildModel("component W service")} motionContextKey="doc-1" />
    );

    rerender(<DiagramCanvas model={buildModel("component Wo service")} motionContextKey="doc-1" />);

    expect(screen.getByText("Wo").closest(".react-flow__node")).not.toHaveClass("diagram-node--entering");
  });

  it("does not animate the incoming graph when the document context changes", () => {
    const { rerender } = render(
      <DiagramCanvas model={buildModel("component API service")} motionContextKey="doc-1" />
    );

    rerender(
      <DiagramCanvas
        model={buildModel("component API service\ncomponent Worker service")}
        motionContextKey="doc-2"
      />
    );

    expect(screen.getByText("Worker").closest(".react-flow__node")).not.toHaveClass("diagram-node--entering");
  });
});

describe("DiagramCanvas zoom controls", () => {
  it("shows the current zoom level as a percentage", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(<DiagramCanvas model={model} />);

    expect(screen.getByText("100%")).toBeInTheDocument();
  });

  it("wires the zoom in, zoom out, and fit buttons to the React Flow instance", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(<DiagramCanvas model={model} insets={{ left: 10, right: 20 }} />);
    fitViewSpy.mockClear();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    fireEvent.click(screen.getByRole("button", { name: "Fit diagram to view" }));

    expect(zoomInSpy).toHaveBeenCalledTimes(1);
    expect(zoomOutSpy).toHaveBeenCalledTimes(1);
    expect(fitViewSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        padding: expect.objectContaining({ left: "122px", right: "20px" })
      })
    );
  });

  it("no longer renders the default React Flow controls widget", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { container } = render(<DiagramCanvas model={model} />);

    expect(container.querySelector(".react-flow__controls")).not.toBeInTheDocument();
  });

  // TODO: restore this assertion when the PNG/SVG image export feature is
  // re-enabled in DiagramCanvas (ExportControls is currently commented out).
  it("does not render image export controls while the feature is disabled", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(<DiagramCanvas model={model} />);
    expect(screen.queryByRole("button", { name: /export png/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /export svg/i })).not.toBeInTheDocument();
  });

  // AEP divergence from upstream: the Auto arrange button is removed, and the
  // remaining zoom controls carry matching title tooltips.
  it("renders no Auto arrange control", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(
      <DiagramCanvas
        model={model}
        customLayout={{
          version: 1,
          sourceFingerprint: "test",
          nodes: { API: { kind: "component", cellId: "main", x: 200, y: 200 } }
        }}
      />
    );

    expect(screen.queryByRole("button", { name: /auto arrange/i })).not.toBeInTheDocument();
  });

  it("gives each zoom control a title tooltip", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(<DiagramCanvas model={model} />);

    for (const name of ["Zoom out", "Zoom in", "Fit diagram to view"]) {
      expect(screen.getByRole("button", { name })).toHaveAttribute("title", name);
    }
  });

  it("renders a supplied canvas message in the shared slot", () => {
    const model = buildModel("component API service\nnorth -> API");
    render(
      <DiagramCanvas
        model={model}
        canvasMessage={{ id: 1, tone: "warning", text: "Manual arrangement is temporary." }}
      />
    );

    const message = screen.getByRole("status");
    expect(message).toHaveTextContent("Manual arrangement is temporary.");
    expect(message).toHaveClass("canvas-notification");
    expect(message).toHaveAttribute("data-tone", "warning");
    expect(document.querySelectorAll(".canvas-notification")).toHaveLength(1);
  });

  it("uses the shared slot for the resting focus hint", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { container } = render(<DiagramCanvas model={model} />);

    const slot = screen.getByText("Click a component to focus its connections.");
    expect(slot).toHaveClass("canvas-notification");
    expect(slot).toHaveAttribute("data-mode", "hint");
    expect(container.querySelectorAll(".canvas-notification")).toHaveLength(1);
  });
});

describe("DiagramCanvas theming", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("defaults to the light theme when no theme is supplied", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { container } = render(<DiagramCanvas model={model} />);

    expect(container.querySelector(".cell-diagram-root")).toHaveAttribute("data-cd-theme", "light");
  });

  it("stamps the dark theme onto the diagram root", () => {
    const model = buildModel("component API service\nnorth -> API");
    const { container } = render(<DiagramCanvas model={model} theme="dark" />);

    expect(container.querySelector(".cell-diagram-root")).toHaveAttribute("data-cd-theme", "dark");
  });

  it("themes the empty-state placeholder too", () => {
    const { container } = render(<DiagramCanvas model={null} theme="dark" />);

    const root = container.querySelector(".cell-diagram-root");
    expect(root).toHaveClass("empty-canvas");
    expect(root).toHaveAttribute("data-cd-theme", "dark");
  });
});

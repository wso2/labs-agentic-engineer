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

// The overview's split, measured rather than asserted from the source.
//
// This lives in the BROWSER lane because there is nothing here jsdom can
// answer. The split is a CONTAINER QUERY — `@container (min-width: 1100px)` on
// the overview's own body — so the thing under test is a layout computation,
// and jsdom has no layout engine to compute it. A unit test could only re-read
// the `sx` object this file is meant to be checking.
//
// It is measured on the PAGE's width, not the viewport's, which is the whole
// point of the change and the reason the wrapper below sets a width rather than
// resizing the window: the nav rail takes ~280px before the page sees any of
// it, and the agent chat panel can take half of what is left, so a viewport
// breakpoint put the diagram in a ~480px column on a 1440px screen and cropped
// it. These two widths are the states either side of that threshold.

import { afterEach, describe, expect, it, vi } from "vitest";
import type { ElementType } from "react";
import { cleanup, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

// Every read is left PENDING on purpose. The panels then render their
// skeletons, and the skeletons sit inside the very boxes under test — the grid
// container and the architecture card both lay out identically whether or not
// their contents arrived. Mocking data in would add fixtures that no assertion
// here reads.
const pending = { isPending: true, isError: false, data: undefined, refetch: vi.fn() };

// Spread the real modules and override only the reads this page makes. Listing
// exports exhaustively instead breaks the moment a sibling drawer imports one
// more hook, which is exactly how the first cut of this file failed.
vi.mock("../api/queries", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  useProject: () => pending,
  useProjectStatus: () => pending,
  useProjectComponents: () => pending,
  useWorkloadDependencies: () => pending,
  useComponentEndpointUrl: () => ({ data: undefined }),
  useComponentOpenApi: () => ({ ...pending, isLoading: false, error: null }),
}));
vi.mock("../../settings/api/queries", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  usePlatformResourceTypes: () => pending,
  useExternalResources: () => pending,
}));
vi.mock("../../spec/api/queries", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  useSpecFiles: () => pending,
  useSpecFileContent: () => pending,
}));

// The router is not what this measures; each link becomes a plain anchor.
vi.mock("@tanstack/react-router", () => ({
  Link: (props: Record<string, unknown>) => <a {...props} />,
  useNavigate: () => vi.fn(),
  createLink: (Component: ElementType) =>
    function MockLink({ to, params, ...rest }: Record<string, unknown>) {
      void to;
      void params;
      return <Component component="a" href="#" {...rest} />;
    },
}));

import { ProjectOverview } from "./ProjectOverview";

/** The threshold itself, and a width comfortably either side of it. */
const SPLIT_PX = 1100;
const WIDE = 1240;
const NARROW = 960;

async function renderAt(width: number) {
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <OxygenUIThemeProvider theme={OxygenTheme}>
      {/* Stands in for the page's content column — the box the container query
          actually measures, which is never the window. */}
        <div style={{ width: `${width}px` }}>
          <ProjectOverview projectName="demo-shop" />
        </div>
      </OxygenUIThemeProvider>
    </QueryClientProvider>,
  );
  // The grid is the only element on the page that declares its own container
  // context, so it identifies itself without a test id.
  const grid = await waitFor(() => {
    const el = view.container.querySelector<HTMLElement>(
      '[style*="container-type"], .MuiBox-root',
    );
    const found = [...view.container.querySelectorAll<HTMLElement>("*")].find(
      (n) => getComputedStyle(n).display === "grid",
    );
    if (!found) throw new Error("no grid found");
    void el;
    return found;
  });
  return { view, grid };
}

afterEach(cleanup);

describe("the overview body splits on its own width, not the viewport's", () => {
  it(`stacks into one column below ${SPLIT_PX}px`, async () => {
    const { grid } = await renderAt(NARROW);
    const tracks = getComputedStyle(grid).gridTemplateColumns.split(/\s+/);
    expect(tracks).toHaveLength(1);
  });

  it(`splits into two columns at ${SPLIT_PX}px and above`, async () => {
    const { grid } = await renderAt(WIDE);
    const tracks = getComputedStyle(grid)
      .gridTemplateColumns.split(/\s+/)
      .map((t) => parseFloat(t));
    expect(tracks).toHaveLength(2);
    // 5fr : 7fr, checked as the ratio rather than as two pixel values, so the
    // assertion survives a change of gap or page width.
    const ratio = tracks[0]! / (tracks[0]! + tracks[1]!);
    expect(ratio).toBeGreaterThan(5 / 12 - 0.03);
    expect(ratio).toBeLessThan(5 / 12 + 0.03);
  });
});

describe("the architecture card takes the height its layout needs", () => {
  // A cell is taller than it is wide, and the renderer fits what it shows to
  // the box's HEIGHT — so the full-width stacked card would show LESS of the
  // graph than the narrow split one unless it also gets taller.
  const cardHeight = (root: HTMLElement) => {
    const cards = [...root.querySelectorAll<HTMLElement>(".MuiCard-root")];
    const card = cards[cards.length - 1]!;
    return parseFloat(getComputedStyle(card).minHeight);
  };

  it("is taller when stacked below the threshold", async () => {
    const { view } = await renderAt(NARROW);
    expect(cardHeight(view.container)).toBe(560);
  });

  it("is shorter when it sits beside the lists", async () => {
    const { view } = await renderAt(WIDE);
    expect(cardHeight(view.container)).toBe(340);
  });
});

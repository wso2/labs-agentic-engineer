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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { homeLink } from "../../agent-chat/chatView";

// A card closes back to the Page it is over, or to where it is told: the
// Questions card answering an issue's chat closes back to that issue's card.

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  useParams: () => ({ projectName: "shop" }),
}));
vi.mock("./WorkspaceTabs", () => ({ WorkspaceTabs: () => null }));

const { CardOverlay } = await import("./CardOverlay");

function renderCard(back?: { to: ReturnType<typeof homeLink>; name: string }) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <CardOverlay card="questions" page="issues" {...(back ? { back } : {})}>
        the questions
      </CardOverlay>
    </OxygenUIThemeProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
}

beforeEach(() => navigate.mockReset());
afterEach(cleanup);

describe("CardOverlay", () => {
  it("closes back to the Page it is over", () => {
    renderCard();
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/issues", params: { projectName: "shop" } });
  });

  it("closes back to where it is told instead: the Questions card of issue #7's chat, to #7's card", () => {
    renderCard({ to: homeLink("shop", "issue", 7), name: "Issue #7" });
    expect(navigate).toHaveBeenCalledWith({ to: "/projects/$projectName/issues/$number", params: { projectName: "shop", number: "7" } });
  });
});

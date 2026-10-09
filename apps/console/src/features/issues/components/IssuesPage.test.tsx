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
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

// Create Issue, in the Issues Page's header: it starts an issue in the Issues
// chat by putting `/issue ` in the composer; nothing is sent.

const compose = vi.fn();
vi.mock("../../shell/chatPanel", () => ({ useChatPanel: () => ({ open: vi.fn(), compose }) }));
vi.mock("../../projects/api/queries", () => ({
  useProject: () => ({ data: undefined }),
  projectLabel: (p: { name: string }) => p.name,
}));
vi.mock("../api/issues", () => ({
  useProjectIssues: () => ({ isError: false, data: [], error: null, refetch: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({ createLink: () => () => null }));

const { IssuesPage } = await import("./IssuesPage");

afterEach(cleanup);

describe("IssuesPage", () => {
  it("Create Issue puts /issue in the chat's composer", () => {
    render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <IssuesPage projectName="shop" />
      </OxygenUIThemeProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Create Issue" }));
    expect(compose).toHaveBeenCalledTimes(1);
    expect(compose).toHaveBeenCalledWith("/issue ");
  });
});

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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

const saveMutate = vi.fn();
const removeMutate = vi.fn();

vi.mock("../api/queries", () => ({
  useConnectCodingAnthropic: () => ({
    mutate: saveMutate,
    isPending: false,
    isError: false,
    error: null,
  }),
  useRemoveCodingAnthropic: () => ({
    mutate: removeMutate,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

const { CodingAgentKeySection } = await import("./CodingAgentKeySection");

type Coding = Parameters<typeof CodingAgentKeySection>[0]["codingLlm"];

const codingKeySet: NonNullable<Coding> = {
  kind: "anthropic",
  credentialKind: "api_key",
  status: "connected",
  keyPrefix: "sk-ant-ap03-QRS",
  keyLast4: "wxyz",
  connectedAt: "2026-08-06T09:41:00Z",
};

function renderSection(codingLlm: Coding) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <CodingAgentKeySection codingLlm={codingLlm} />
    </OxygenUIThemeProvider>,
  );
}

const reuseRadio = () => screen.getByRole("radio", { name: "Reuse the key above" });
const separateRadio = () => screen.getByRole("radio", { name: "Use a separate key" });

// While the confirm dialog is open MUI marks the page behind it aria-hidden, so
// the radios are (correctly) not in the accessibility tree. These assertions are
// about the radio's VALUE surviving the dialog, not about its reachability.
const separateRadioBehindModal = () =>
  screen.getByRole("radio", { name: "Use a separate key", hidden: true });

beforeEach(() => {
  saveMutate.mockClear();
  removeMutate.mockClear();
});
afterEach(cleanup);

describe("CodingAgentKeySection", () => {
  it("defaults to reuse, and offers no key field, when no coding key is set", () => {
    renderSection(null);
    expect(reuseRadio()).toBeChecked();
    expect(separateRadio()).not.toBeChecked();
    expect(screen.queryByLabelText("Coding agent key or token")).not.toBeInTheDocument();
  });

  it("shows separate as selected, with the stored key previewed, when one is set", () => {
    renderSection(codingKeySet);
    expect(separateRadio()).toBeChecked();
    expect(screen.getByText(/sk-ant-ap03-QRS/)).toBeInTheDocument();
    expect(screen.getByText(/wxyz/)).toBeInTheDocument();
  });

  // The radio is local state: picking "separate" reveals the field but must not
  // write anything until the button is pressed.
  it("reveals the key field on selecting separate, without writing", () => {
    renderSection(null);
    fireEvent.click(separateRadio());

    expect(screen.getByLabelText("Coding agent key or token")).toBeInTheDocument();
    expect(saveMutate).not.toHaveBeenCalled();
    expect(removeMutate).not.toHaveBeenCalled();
  });

  it("keeps the save button disabled until a key is typed", () => {
    renderSection(null);
    fireEvent.click(separateRadio());

    const save = screen.getByRole("button", { name: "Save key" });
    expect(save).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Coding agent key or token"), {
      target: { value: "sk-ant-api03-typed" },
    });
    expect(save).toBeEnabled();

    fireEvent.click(save);
    expect(saveMutate).toHaveBeenCalledWith(
      "sk-ant-api03-typed",
      expect.anything(),
    );
  });

  // The one destructive path. Flipping back must not discard a stored key on a
  // single click — and the radio must not move until the removal succeeds.
  it("confirms before removing a stored key, and does not move the radio yet", () => {
    renderSection(codingKeySet);
    fireEvent.click(reuseRadio());

    expect(
      screen.getByRole("heading", { name: "Remove the coding agent key?" }),
    ).toBeInTheDocument();
    expect(removeMutate).not.toHaveBeenCalled();
    expect(separateRadioBehindModal()).toBeChecked();
  });

  it("cancelling the confirm leaves the key alone", () => {
    renderSection(codingKeySet);
    fireEvent.click(reuseRadio());
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(removeMutate).not.toHaveBeenCalled();
    expect(separateRadioBehindModal()).toBeChecked();
  });

  it("confirming removes the key", () => {
    renderSection(codingKeySet);
    fireEvent.click(reuseRadio());
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    expect(removeMutate).toHaveBeenCalledOnce();
  });

  // With nothing stored there is nothing to lose, so no dialog should appear.
  it("flipping back to reuse with no stored key needs no confirmation", () => {
    renderSection(null);
    fireEvent.click(separateRadio());
    fireEvent.click(reuseRadio());

    expect(
      screen.queryByRole("heading", { name: "Remove the coding agent key?" }),
    ).not.toBeInTheDocument();
    expect(removeMutate).not.toHaveBeenCalled();
    expect(reuseRadio()).toBeChecked();
  });
});

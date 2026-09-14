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

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { StartBuildDialog } from "./StartBuildDialog";

type BuildChange = components["schemas"]["BuildChange"];

const CHANGES: BuildChange[] = [
  { name: "orders-api", kind: "component", state: "changed" },
  { name: "reports-web", kind: "component", state: "new" },
  { name: "legacy-mailer", kind: "external", state: "removed" },
  { name: "orders-db", kind: "platform-resource", state: "new" },
];

function open(over: Partial<Parameters<typeof StartBuildDialog>[0]> = {}) {
  const onBuild = vi.fn();
  render(
    <StartBuildDialog
      open
      currentVersion="v2"
      suggestedVersion="v3"
      specUnchanged={false}
      changes={CHANGES}
      takenVersions={["v1", "v2"]}
      onClose={vi.fn()}
      onBuild={onBuild}
      {...over}
    />,
  );
  return { onBuild };
}

describe("StartBuildDialog", () => {
  // An untouched field sends no name at all: the suggestion stays a
  // suggestion, so a build that loses the race for it is offered the next one
  // instead of being refused.
  it("offers the suggested name, and builds without claiming it", () => {
    const { onBuild } = open();

    expect(screen.getByTestId("version-name")).toHaveValue("v3");
    fireEvent.click(screen.getByRole("button", { name: "Build v3" }));

    expect(onBuild).toHaveBeenCalledWith("");
  });

  it("builds with the name the user typed instead", () => {
    const { onBuild } = open();

    fireEvent.change(screen.getByTestId("version-name"), {
      target: { value: " payments-v2 " },
    });
    // The action names the tag that will exist, not the raw field value.
    fireEvent.click(screen.getByRole("button", { name: "Build payments-v2" }));

    // Trimmed on the way out: the user's spacing is not part of the tag.
    expect(onBuild).toHaveBeenCalledWith("payments-v2");
  });

  it("refuses a name already in use, at the field", () => {
    const { onBuild } = open();

    fireEvent.change(screen.getByTestId("version-name"), {
      target: { value: "v2" },
    });

    expect(
      screen.getByText("A version named v2 already exists."),
    ).toBeInTheDocument();
    const build = screen.getByRole("button", { name: "Build v2" });
    expect(build).toBeDisabled();
    fireEvent.click(build);
    expect(onBuild).not.toHaveBeenCalled();
  });

  it("names what the version changes, and flags only new and removed", () => {
    open();

    expect(screen.getByText("What changed since v2")).toBeInTheDocument();
    expect(screen.getByText("orders-api")).toBeInTheDocument();
    expect(screen.getAllByText("new")).toHaveLength(2);
    expect(screen.getByText("removed")).toBeInTheDocument();
    // `changed` is the default state, so it earns no chip.
    expect(screen.queryByText("changed")).not.toBeInTheDocument();
  });

  // A bare list of names cannot say what a name IS: a database the platform
  // stands up reads exactly like a third-party API the user must go and sign up
  // for. The headings are the whole of the difference the dialog draws.
  it("groups the rows by kind", () => {
    open();

    expect(screen.getByText("Components")).toBeInTheDocument();
    expect(screen.getByText("External dependencies")).toBeInTheDocument();
    expect(screen.getByText("Platform resources")).toBeInTheDocument();
  });

  it("renders no heading for a kind this version does not touch", () => {
    open({ changes: CHANGES.filter((c) => c.kind === "component") });

    expect(screen.getByText("Components")).toBeInTheDocument();
    expect(screen.queryByText("External dependencies")).not.toBeInTheDocument();
    expect(screen.queryByText("Platform resources")).not.toBeInTheDocument();
  });

  // A build deprovisions nothing, so the dialog states the fact rather than
  // implying it takes the resource down.
  it("says a removed dependency keeps its resource", () => {
    open();
    expect(
      screen.getByText(/removed dependency keeps its resource/i),
    ).toBeInTheDocument();
  });

  it("drops the note when nothing was removed", () => {
    open({ changes: CHANGES.filter((c) => c.state !== "removed") });
    expect(
      screen.queryByText(/removed dependency keeps its resource/i),
    ).not.toBeInTheDocument();
  });

  it("reads as a first build when the project has no version yet", () => {
    open({ currentVersion: "", suggestedVersion: "v1", takenVersions: [] });

    expect(screen.getByText("What this version creates")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Build v1" })).toBeEnabled();
  });

  // An unchanged spec tree reuses its version (ADR-0030): there is no name to
  // give, so the field is locked and the request carries none.
  it("locks the name and rebuilds when the spec tree has not moved", () => {
    const { onBuild } = open({ specUnchanged: true, changes: [] });

    expect(screen.getByTestId("version-name")).toBeDisabled();
    expect(screen.getByTestId("version-name")).toHaveValue("v2");
    expect(screen.getByText("No spec changes since v2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Rebuild v2" }));

    expect(onBuild).toHaveBeenCalledWith("");
  });

  it("holds everything still while the build request is in flight", () => {
    open({ submitting: true });

    expect(screen.getByTestId("version-name")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });
});

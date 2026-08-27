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
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { ResourceDocsFields, type ResourceDocRow } from "./ResourceDocsFields";

function Host() {
  const [docs, setDocs] = useState<ResourceDocRow[]>([]);
  return <ResourceDocsFields docs={docs} onChange={setDocs} />;
}

describe("ResourceDocsFields", () => {
  it("Add doc defaults the type select to documentation", () => {
    render(<Host />);

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));

    expect(screen.getByRole("combobox", { name: "Type" })).toHaveTextContent(
      "Documentation",
    );
    expect(screen.getByRole("group", { name: "Doc source" })).toBeInTheDocument();
    expect(screen.getByLabelText("URL")).toBeInTheDocument();
  });

  it("choosing File shows Choose file and hides the URL textbox", () => {
    render(<Host />);

    fireEvent.click(screen.getByRole("button", { name: "Add doc" }));
    fireEvent.click(screen.getByRole("button", { name: "File" }));

    expect(screen.getByRole("button", { name: "Choose file" })).toBeInTheDocument();
    expect(screen.queryByLabelText("URL")).not.toBeInTheDocument();
  });
});

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
import { describe, expect, it, vi } from "vitest";
import { ConnectionValueFields } from "./ConnectionValueFields";

describe("ConnectionValueFields", () => {
  it("renders Oxygen fields with descriptions and masks secret input", () => {
    render(
      <ConnectionValueFields
        config={[
          { key: "REGION", description: "Cloud region" },
          { key: "API_KEY", secret: true, description: "Provider key" },
        ]}
        values={{ REGION: "us-east-1", API_KEY: "" }}
        onValueChange={vi.fn()}
      />,
    );

    expect(screen.getByLabelText("REGION")).toHaveValue("us-east-1");
    expect(screen.getByText("Cloud region")).toBeInTheDocument();
    expect(screen.getByLabelText("API_KEY")).toHaveAttribute(
      "type",
      "password",
    );
  });

  it("reports the edited key and value", () => {
    const onValueChange = vi.fn();
    render(
      <ConnectionValueFields
        config={[{ key: "REGION" }]}
        values={{}}
        onValueChange={onValueChange}
      />,
    );

    fireEvent.change(screen.getByLabelText("REGION"), {
      target: { value: "eu-west-1" },
    });

    expect(onValueChange).toHaveBeenCalledWith("REGION", "eu-west-1");
  });
});

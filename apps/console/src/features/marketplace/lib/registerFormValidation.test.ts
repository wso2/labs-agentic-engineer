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

import { describe, expect, it } from "vitest";
import {
  REQUIRED_CONFIG_KEY,
  REQUIRED_FIELD,
  envValueCellKey,
  validateRegisterForm,
} from "./registerFormValidation";

const filled = {
  name: "github",
  description: "GitHub REST API",
  consumptionInstructions: "Call api.github.com with a bearer token.",
  keys: [{ key: "GITHUB_TOKEN", description: "PAT", secret: true }],
  values: {
    [envValueCellKey("development", "GITHUB_TOKEN")]: "x",
  },
  envNames: ["development"],
  isEdit: false,
};

describe("validateRegisterForm", () => {
  it("returns null when every required field is filled", () => {
    expect(validateRegisterForm(filled)).toBeNull();
  });

  it("flags empty name, description, and consumption instructions", () => {
    const errors = validateRegisterForm({
      ...filled,
      name: "  ",
      description: "",
      consumptionInstructions: "",
    });
    expect(errors?.name).toBe(REQUIRED_FIELD);
    expect(errors?.description).toBe(REQUIRED_FIELD);
    expect(errors?.consumptionInstructions).toBe(REQUIRED_FIELD);
  });

  it("flags a missing config-key list", () => {
    const errors = validateRegisterForm({ ...filled, keys: [], values: {} });
    expect(errors?.configKeys).toBe(REQUIRED_CONFIG_KEY);
  });

  it("flags empty key name and description on a row", () => {
    const errors = validateRegisterForm({
      ...filled,
      keys: [{ key: "", description: "", secret: false }],
      values: {},
    });
    expect(errors?.keys[0]).toEqual({ key: REQUIRED_FIELD, description: REQUIRED_FIELD });
  });

  it("flags an empty env value on create", () => {
    const errors = validateRegisterForm({ ...filled, values: {} });
    expect(errors?.values[envValueCellKey("development", "GITHUB_TOKEN")]).toBe(
      REQUIRED_FIELD,
    );
  });

  it("allows a blank secret on edit when the cell is already configured", () => {
    expect(
      validateRegisterForm({
        ...filled,
        values: {},
        isEdit: true,
        envCells: [
          { environment: "development", key: "GITHUB_TOKEN", status: "configured" },
        ],
      }),
    ).toBeNull();
  });
});

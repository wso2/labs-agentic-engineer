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

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ContractFields,
  EMPTY_CONTRACT_FILE,
  contractRowError,
  contractWriteFromRow,
  emptyContractRow,
} from "./ContractFields";

function uploaded(content: string) {
  return { ...emptyContractRow(), fileName: "openapi.yaml", content };
}

describe("contractRowError", () => {
  // A zero-byte file reads as "no document" on the wire: submitting would
  // quietly leave the record's document as it was.
  it("refuses a chosen file with no bytes", () => {
    expect(contractRowError(uploaded(""))).toBe(EMPTY_CONTRACT_FILE);
    expect(contractWriteFromRow(uploaded(""))).toBeUndefined();
  });

  it("passes a file with bytes, a URL, and an untouched block", () => {
    expect(contractRowError(uploaded("openapi: 3.1.0"))).toBeUndefined();
    expect(contractWriteFromRow(uploaded("openapi: 3.1.0"))).toEqual({
      type: "openapi",
      fileName: "openapi.yaml",
      content: "openapi: 3.1.0",
    });
    const url = { ...emptyContractRow(), url: "https://example.com/openapi.yaml" };
    expect(contractRowError(url)).toBeUndefined();
    expect(contractWriteFromRow(url)).toEqual({
      type: "openapi",
      url: "https://example.com/openapi.yaml",
    });
    expect(contractRowError(emptyContractRow())).toBeUndefined();
    expect(contractWriteFromRow(emptyContractRow())).toBeUndefined();
  });
});

describe("ContractFields", () => {
  it("says why an empty file cannot be submitted", () => {
    render(<ContractFields contract={uploaded("")} onChange={vi.fn()} />);
    expect(screen.getByText(EMPTY_CONTRACT_FILE)).toBeInTheDocument();
  });

  it("says nothing when the file has bytes", () => {
    render(<ContractFields contract={uploaded("openapi: 3.1.0")} onChange={vi.fn()} />);
    expect(screen.queryByText(EMPTY_CONTRACT_FILE)).not.toBeInTheDocument();
  });
});

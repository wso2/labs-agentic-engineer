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
import { emptyContractRow } from "../components/ContractFields";
import {
  applyRegisterDraft,
  parseRegisterDraft,
  type RegisterDraft,
  type RegisterFormSnapshot,
} from "./registerDraft";

function snapshot(
  overrides: Partial<RegisterFormSnapshot> = {},
): RegisterFormSnapshot {
  return {
    name: "",
    provider: "",
    description: "",
    consumptionInstructions: "",
    keys: [],
    values: {},
    contract: emptyContractRow(),
    docs: [],
    ...overrides,
  };
}

describe("parseRegisterDraft", () => {
  it("returns null when the input is not a plain object", () => {
    expect(parseRegisterDraft(null)).toBeNull();
    expect(parseRegisterDraft([])).toBeNull();
    expect(parseRegisterDraft("stripe")).toBeNull();
  });

  it("ignores envValues and other unknown keys", () => {
    const draft = parseRegisterDraft({
      name: "stripe",
      envValues: [
        { environment: "development", key: "API_KEY", value: "sk-from-chat" },
      ],
      secretBytes: "nope",
    });
    expect(draft).toEqual({ name: "stripe" });
    expect(JSON.stringify(draft)).not.toContain("sk-from-chat");
  });

  it("reads the provider and a contract URL from the draft", () => {
    expect(
      parseRegisterDraft({
        name: "currency-service",
        provider: "Open Exchange Rates",
        contract: { type: "openapi", url: "https://openexchangerates.org/openapi.yaml" },
      }),
    ).toEqual({
      name: "currency-service",
      provider: "Open Exchange Rates",
      contract: { type: "openapi", url: "https://openexchangerates.org/openapi.yaml" },
    });
  });

  // The agent proposes an ADDRESS; the bytes of a document are the user's own
  // upload, so a draft carrying them is not a draft the form applies.
  it("skips a contract of an unknown type, without a url, or carrying file bytes", () => {
    expect(parseRegisterDraft({ contract: { type: "bogus", url: "https://x/y" } })).toEqual({});
    expect(parseRegisterDraft({ contract: { type: "openapi" } })).toEqual({});
    // A blank address is no address: the form trims before submitting, so a
    // whitespace URL would replace a real one and then vanish on the wire.
    expect(parseRegisterDraft({ contract: { type: "openapi", url: "   " } })).toEqual({});
    expect(
      parseRegisterDraft({ contract: { type: "openapi", url: "  https://x/y  " } }),
    ).toEqual({ contract: { type: "openapi", url: "https://x/y" } });
    expect(
      parseRegisterDraft({
        contract: { type: "openapi", url: "https://x/y", content: "openapi: 3.1.0" },
      }),
    ).toEqual({});
  });

  it("skips malformed config and docs entries", () => {
    const draft = parseRegisterDraft({
      config: [
        { key: "API_KEY", description: "Secret", secret: true },
        { key: 1, description: "bad", secret: false },
        { description: "no key", secret: true },
      ],
      resourceDocs: [
        { type: "openapi", url: "https://example.com/openapi.yaml" },
        { type: "documentation", path: "docs/readme.md" },
        { type: "bogus", url: "https://example.com/x" },
      ],
    });
    expect(draft).toEqual({
      config: [{ key: "API_KEY", description: "Secret", secret: true }],
      resourceDocs: [
        { type: "openapi", url: "https://example.com/openapi.yaml" },
      ],
    });
  });
});

describe("applyRegisterDraft", () => {
  it("does not copy env values or secret bytes from a draft", () => {
    const current = snapshot({ values: { "development:API_KEY": "typed-by-human" } });
    const next = applyRegisterDraft(
      current,
      {
        name: "stripe",
        description: "Payments API",
        consumptionInstructions: "Use the secret key as Bearer.",
        config: [{ key: "API_KEY", description: "Secret", secret: true }],
        envValues: [{ environment: "development", key: "API_KEY", value: "sk-from-chat" }],
      } as RegisterDraft & { envValues: unknown },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.values).toEqual({ "development:API_KEY": "typed-by-human" });
    expect(JSON.stringify(next)).not.toContain("sk-from-chat");
  });

  it("does not rename when freezeName is true", () => {
    const current = snapshot({ name: "stripe", description: "old" });
    const next = applyRegisterDraft(current, { name: "renamed", description: "new" }, { freezeName: true, freezeKeys: true });
    expect(next.name).toBe("stripe");
    expect(next.description).toBe("new");
  });

  it("replaces keys from the draft on create", () => {
    const current = snapshot({
      keys: [{ key: "OLD", description: "old", secret: false }],
    });
    const next = applyRegisterDraft(
      current,
      { config: [{ key: "API_KEY", description: "Secret", secret: true }] },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.keys).toEqual([
      { key: "API_KEY", description: "Secret", secret: true },
    ]);
  });

  it("does not add keys when freezeKeys is true", () => {
    const current = snapshot({
      name: "stripe",
      keys: [{ key: "API_KEY", description: "old desc", secret: true }],
    });
    const next = applyRegisterDraft(
      current,
      {
        config: [
          { key: "API_KEY", description: "new desc", secret: false },
          { key: "NEW_KEY", description: "added", secret: false },
        ],
      },
      { freezeName: true, freezeKeys: true },
    );
    expect(next.keys).toEqual([
      { key: "API_KEY", description: "new desc", secret: true },
    ]);
  });

  it("fills the provider and the contract block from the draft", () => {
    const next = applyRegisterDraft(
      snapshot(),
      {
        provider: "Open Exchange Rates",
        contract: { type: "openapi", url: "https://openexchangerates.org/openapi.yaml" },
      },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.provider).toBe("Open Exchange Rates");
    expect(next.contract).toEqual({
      type: "openapi",
      url: "https://openexchangerates.org/openapi.yaml",
      fileName: "",
      content: "",
      path: "",
    });
  });

  // An upload is the user's own act, and its TYPE goes with the bytes: a draft
  // may propose a different document, but it never relabels the file on file.
  it("keeps an uploaded contract file, and its type, when a draft proposes a URL", () => {
    const uploaded = {
      type: "openapi" as const,
      url: "",
      fileName: "openapi.yaml",
      content: "openapi: 3.1.0",
      path: "",
    };
    const next = applyRegisterDraft(
      snapshot({ contract: uploaded }),
      { contract: { type: "graphql", url: "https://example.com/schema.graphql" } },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.contract).toEqual(uploaded);
  });

  it("applies URL resource-docs from the draft", () => {
    const next = applyRegisterDraft(
      snapshot(),
      { resourceDocs: [{ type: "openapi", url: "https://example.com/openapi.yaml" }] },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.docs).toEqual([
      {
        type: "openapi",
        source: "url",
        url: "https://example.com/openapi.yaml",
        fileName: "",
        content: "",
        path: "",
      },
    ]);
  });

  it("keeps existing file rows when a later draft patches URL docs", () => {
    const fileRow = {
      type: "documentation" as const,
      source: "file" as const,
      url: "",
      fileName: "readme.md",
      content: "# hi",
      path: "docs/readme.md",
    };
    const next = applyRegisterDraft(
      snapshot({
        docs: [
          fileRow,
          {
            type: "documentation",
            source: "url",
            url: "https://example.com/old.md",
            fileName: "",
            content: "",
            path: "",
          },
        ],
      }),
      { resourceDocs: [{ type: "openapi", url: "https://example.com/openapi.yaml" }] },
      { freezeName: false, freezeKeys: false },
    );
    expect(next.docs).toEqual([
      fileRow,
      {
        type: "documentation",
        source: "url",
        url: "https://example.com/old.md",
        fileName: "",
        content: "",
        path: "",
      },
      {
        type: "openapi",
        source: "url",
        url: "https://example.com/openapi.yaml",
        fileName: "",
        content: "",
        path: "",
      },
    ]);
  });

  it("drops file-row resource-docs from the draft", () => {
    const next = applyRegisterDraft(
      snapshot(),
      {
        resourceDocs: [
          { type: "documentation", url: "https://example.com/docs.md" },
          {
            type: "documentation",
            url: "",
            path: "docs/readme.md",
            fileName: "readme.md",
            content: "# hi",
          },
        ],
      } as RegisterDraft,
      { freezeName: false, freezeKeys: false },
    );
    expect(next.docs).toEqual([
      {
        type: "documentation",
        source: "url",
        url: "https://example.com/docs.md",
        fileName: "",
        content: "",
        path: "",
      },
    ]);
    expect(JSON.stringify(next)).not.toContain("readme.md");
    expect(JSON.stringify(next)).not.toContain("# hi");
  });
});

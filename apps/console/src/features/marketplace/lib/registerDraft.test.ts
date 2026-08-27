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
    description: "",
    consumptionInstructions: "",
    keys: [],
    values: {},
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
    const current = {
      name: "",
      description: "",
      consumptionInstructions: "",
      keys: [],
      values: { "development:API_KEY": "typed-by-human" },
      docs: [],
    };
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
    const current = { name: "stripe", description: "old", consumptionInstructions: "", keys: [], values: {}, docs: [] };
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

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

import { describe, expect, test } from "vitest";

import { isParseError, parseAgentAfm } from "./parse.js";

/**
 * A complete AFM, copied from a real generated `booking-agent`. Every test that
 * needs a valid document starts from this so a change to the shipped shape
 * fails here rather than in the view.
 */
const BOOKING_AGENT = `---
spec_version: "0.4.0"
name: "booking-agent"
description: >
  Helps a signed-in traveler find and book a hotel by chatting in natural language.
max_iterations: 12

model:
  provider: "anthropic"
  name: "\${env:MODEL_NAME}"
  url: "\${env:MODEL_ENDPOINT}"
  authentication:
    type: "api-key"
    api_key: "\${env:MODEL_API_KEY}"

interfaces:
  - type: webchat
    exposure:
      http:
        path: "/chat"

x-aep:
  tools:
    openapi:
      - component: "hotel-api"
        baseUrl: "\${env:HOTEL_API_URL}"
        allow: [listHotels, getHotel, createReservation]
  memory:
    type: "client"
  identity:
    mode: "on-behalf-of"
---

# Role
You help a signed-in traveler find and book a hotel.

# Instructions
- Ask for missing dates rather than guessing.

# Style
Short, friendly, and practical.
`;

/** Narrows to the success branch, failing the test if the parse errored. */
function parsed(raw: string) {
  const result = parseAgentAfm(raw);
  if (isParseError(result)) {
    throw new Error(`expected a parsed spec, got error: ${result.error}`);
  }
  return result;
}

describe("parseAgentAfm — identity", () => {
  test("reads name, description, spec version and max iterations", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.name).toBe("booking-agent");
    expect(spec.description).toBe(
      "Helps a signed-in traveler find and book a hotel by chatting in natural language.",
    );
    expect(spec.specVersion).toBe("0.4.0");
    expect(spec.maxIterations).toBe(12);
  });
});

describe("parseAgentAfm — model", () => {
  test("keeps the ${env:} references verbatim rather than resolving them", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.model).toEqual({
      provider: "anthropic",
      name: "${env:MODEL_NAME}",
      url: "${env:MODEL_ENDPOINT}",
      authType: "api-key",
      authKey: "${env:MODEL_API_KEY}",
    });
  });
});

describe("parseAgentAfm — interfaces", () => {
  test("reads the interface type and its exposed HTTP path", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.interfaces).toEqual([{ type: "webchat", path: "/chat" }]);
  });
});

describe("parseAgentAfm — tools", () => {
  test("groups the allow-list under the provider component it targets", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.tools).toEqual([
      {
        component: "hotel-api",
        baseUrl: "${env:HOTEL_API_URL}",
        operations: ["listHotels", "getHotel", "createReservation"],
      },
    ]);
  });
});

describe("parseAgentAfm — memory and identity", () => {
  test("reads the x-aep memory type and identity mode", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.memory).toBe("client");
    expect(spec.identity).toBe("on-behalf-of");
  });
});

describe("parseAgentAfm — prompt body", () => {
  test("splits the body into its headed sections, in document order", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.prompt).toEqual([
      { heading: "Role", body: "You help a signed-in traveler find and book a hotel." },
      { heading: "Instructions", body: "- Ask for missing dates rather than guessing." },
      { heading: "Style", body: "Short, friendly, and practical." },
    ]);
  });

  test("keeps a section's blank lines and list structure intact", () => {
    const spec = parsed(
      BOOKING_AGENT.replace(
        "- Ask for missing dates rather than guessing.",
        "- First point.\n\n- Second point.",
      ),
    );

    expect(spec.prompt[1]?.body).toBe("- First point.\n\n- Second point.");
  });

  test("reads a body that has no headings at all as a single unheaded section", () => {
    const spec = parsed(BOOKING_AGENT.replace(/# Role[\s\S]*$/, "Just prose, no headings.\n"));

    expect(spec.prompt).toEqual([{ heading: "", body: "Just prose, no headings." }]);
  });
});

describe("parseAgentAfm — tolerance", () => {
  test("renders a draft that has no x-aep block yet", () => {
    const spec = parsed(BOOKING_AGENT.replace(/\nx-aep:[\s\S]*?\n---/, "\n---"));

    expect(spec.name).toBe("booking-agent");
    expect(spec.tools).toEqual([]);
    expect(spec.memory).toBeUndefined();
    expect(spec.identity).toBeUndefined();
  });

  test("ignores an unknown front-matter key rather than refusing the document", () => {
    const spec = parsed(BOOKING_AGENT.replace('name: "booking-agent"', 'name: "booking-agent"\nfuture_key: "from a newer spec"'));

    expect(spec.name).toBe("booking-agent");
    expect(spec.model?.provider).toBe("anthropic");
  });
});

describe("parseAgentAfm — errors", () => {
  test("reports a document with no front matter instead of throwing", () => {
    const result = parseAgentAfm("# Role\nJust a markdown file.\n");

    expect(isParseError(result)).toBe(true);
  });

  test("reports malformed YAML instead of throwing", () => {
    const result = parseAgentAfm('---\nname: "unclosed\n  bad: [\n---\n\n# Role\nx\n');

    expect(isParseError(result)).toBe(true);
  });
});

describe("parseAgentAfm — fenced code in the prompt body", () => {
  test("does not read a '#' inside a code fence as a section heading", () => {
    const spec = parsed(
      BOOKING_AGENT.replace(
        "Short, friendly, and practical.",
        "Run it like this:\n\n```bash\n# install first\nnpm i\n```\n\nThen reply.",
      ),
    );

    const style = spec.prompt.find((s) => s.heading === "Style");
    expect(spec.prompt.map((s) => s.heading)).toEqual(["Role", "Instructions", "Style"]);
    expect(style?.body).toContain("# install first");
    expect(style?.body).toContain("Then reply.");
  });
});

describe("parseAgentAfm — the raw body", () => {
  // The editor writes back what the author typed, so it needs the body
  // VERBATIM — not the section split, which drops the exact spacing and any
  // content the section reader does not recognise.
  test("carries the prompt body through unchanged", () => {
    const spec = parsed(BOOKING_AGENT);

    expect(spec.body).toContain("# Role");
    expect(spec.body).toContain("You help a signed-in traveler find and book a hotel.");
    expect(spec.body).toContain("# Style");
    expect(spec.body).not.toContain("spec_version");
  });

  // `splitAfm` trims the body, so a saved document is the ORIGINAL front matter
  // (byte for byte — never re-serialised from parsed YAML, which would reformat
  // it) plus the trimmed body. That is the exact shape a save reconstructs.
  test("is the document's own text, so front matter + body reassembles it", () => {
    const spec = parsed(BOOKING_AGENT);
    const frontMatter = BOOKING_AGENT.slice(0, BOOKING_AGENT.indexOf(spec.body));

    expect(`${frontMatter}${spec.body}\n`).toBe(BOOKING_AGENT);
    expect(spec.body).toBe(spec.body.trim());
  });
});

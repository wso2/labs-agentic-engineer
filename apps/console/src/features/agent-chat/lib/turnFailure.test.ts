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
import { turnFailureText } from "./turnFailure";

// A fixed "now" so the same-day / other-day stamp is deterministic.
const NOW = new Date(2026, 8, 26, 12, 0, 0);

describe("turnFailureText", () => {
  it("names whose usage limit is reached and when to try again", () => {
    const resetAt = new Date(2026, 8, 26, 14, 5, 0).toISOString();
    const text = turnFailureText({ code: "provider_limit", host: "ollama.com", resetAt, message: "raw" }, NOW);
    expect(text).toMatch(/^ollama\.com's usage limit is reached\. Try again after .+\.$/);
    // Local time only, today: the reader's own clock, no date.
    expect(text).toContain(new Date(resetAt).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }));
    expect(text).not.toContain("raw");
  });

  it("says try again later when the provider stated no reset, and copes without a host", () => {
    expect(turnFailureText({ code: "provider_limit" }, NOW)).toBe(
      "The model provider's usage limit is reached. Try again later.",
    );
  });

  it("shows the agents service's sentence for a truncated write", () => {
    const message = "The model's output limit (8192 tokens per step) cut off addFile for specs/prd.md before it finished, so nothing was written.";
    expect(turnFailureText({ code: "output_truncated", message }, NOW)).toBe(message);
  });

  it("keeps the recorded message for an uncoded failure, with a fallback", () => {
    expect(turnFailureText({ message: "stream severed before the manifest" }, NOW)).toBe(
      "stream severed before the manifest",
    );
    expect(turnFailureText({}, NOW)).toBe("The agent turn failed.");
  });
});

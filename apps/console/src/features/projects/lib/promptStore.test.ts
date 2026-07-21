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
  buildSpecGenerationInstruction,
  withGrillingInterview,
} from "@aep/contracts/prompts";

describe("buildSpecGenerationInstruction", () => {
  it("wraps a stored prompt in an explicit generate command", () => {
    const out = buildSpecGenerationInstruction(
      "An online store for handmade ceramics",
    );
    expect(out).toMatch(/^Generate a complete requirements specification/);
    expect(out).toContain("requirements/requirements.md");
    expect(out).toContain("An online store for handmade ceramics");
  });

  it("falls back to a generic instruction when no prompt is stored", () => {
    for (const empty of [null, "", "   "]) {
      const out = buildSpecGenerationInstruction(empty);
      expect(out).toMatch(/^Generate a complete requirements specification/);
      expect(out).toContain("requirements/requirements.md");
      expect(out.endsWith(" for this project.")).toBe(true);
    }
  });

  it("stays one-shot by itself; the interview is an explicit opt-in wrapper (#270)", () => {
    const bare = buildSpecGenerationInstruction("idea");
    expect(bare).not.toContain("ask_question");
    const wrapped = withGrillingInterview(bare);
    expect(wrapped).toMatch(/^Before writing any files, interview me/);
    expect(wrapped).toContain("ask_question");
    expect(wrapped).toContain(bare);
  });
});

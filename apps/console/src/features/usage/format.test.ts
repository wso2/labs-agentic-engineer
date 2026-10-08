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
import { formatTokens, formatUsd, totalTokens, unpricedHostLine, type Usage } from "./format";

const usage = (over: Partial<Usage> = {}): Usage => ({
  inputTokens: 100_000,
  outputTokens: 50_000,
  cacheReadTokens: 1_000_000,
  cacheCreationTokens: 200_000,
  model: "claude-fable-5",
  costUsd: 12.34,
  ...over,
});

describe("formatTokens", () => {
  it("abbreviates thousands and millions, with one decimal below ten", () => {
    expect(formatTokens(950)).toBe("950");
    expect(formatTokens(1_250)).toBe("1.3K");
    expect(formatTokens(12_400)).toBe("12K");
    expect(formatTokens(1_350_000)).toBe("1.4M");
    expect(formatTokens(31_000_000)).toBe("31M");
  });
});

describe("formatUsd", () => {
  it("drops zero cents, keeps cents that say something, and marks dust", () => {
    expect(formatUsd(34)).toBe("$34");
    expect(formatUsd(6.4)).toBe("$6.40");
    expect(formatUsd(0)).toBe("$0");
    expect(formatUsd(0.004)).toBe("<$0.01");
  });
});

describe("totalTokens", () => {
  it("adds input, output and both kinds of cache traffic", () => {
    expect(totalTokens(usage())).toBe(1_350_000);
  });
});

describe("unpricedHostLine", () => {
  it("names the host that billed an unpriced figure", () => {
    expect(unpricedHostLine(usage({ costUsd: null, host: "ollama.com" }))).toBe("not priced · billed by ollama.com");
  });

  it("says nothing of a priced figure, or of one whose host is unknown", () => {
    expect(unpricedHostLine(usage({ host: "ollama.com" }))).toBeNull();
    expect(unpricedHostLine(usage({ costUsd: null, host: "" }))).toBeNull();
  });
});

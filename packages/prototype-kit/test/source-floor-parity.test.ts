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

/**
 * The static source floor, asserted against the table the Go save gate asserts
 * too. The rows are the cases both sides must agree on (syntax, imports, size);
 * the kit's own fixtures cover the checks only the kit makes.
 */

import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { MAX_SOURCE_LENGTH, checkSource } from "../src/source/index.js";

interface Row {
  name: string;
  source: string;
  /** Pad the source with a trailing comment to exactly this many UTF-16 code units. */
  padTo?: number;
  findings: { code: string; location: string }[];
}

const table = JSON.parse(readFileSync(new URL("./fixtures/source-floor-cases.json", import.meta.url), "utf8")) as {
  maxSourceLength: number;
  cases: Row[];
};

function sourceOf(row: Row): string {
  return row.padTo === undefined ? row.source : row.source + "//" + "x".repeat(row.padTo - row.source.length - 2);
}

describe("source floor cases shared with the Go save gate", () => {
  it("the size cap is the one the table names", () => {
    expect(MAX_SOURCE_LENGTH).toBe(table.maxSourceLength);
  });

  it.each(table.cases.map((c) => [c.name, c] as const))("%s", (_name, row) => {
    const got = checkSource(sourceOf(row)).map((f) => ({ code: f.code, location: f.location }));
    expect(got).toEqual(row.findings);
  });
});

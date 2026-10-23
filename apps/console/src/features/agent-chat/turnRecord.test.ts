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

import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";
import type { components } from "../../generated/aep-api";
import { specKey } from "../spec/api/specModel";
import { readSpecStateUntilRecorded } from "./turnRecord";

// The turn's record lands after its end, so the spec state read at the end
// is the one from before the run; it is read again until the record shows.

type SpecState = components["schemas"]["SpecState"];

const before: SpecState = { designedFrom: { F2: "old basis" }, artifactRevisions: { "specs/design/domain-model.md": 1 }, documents: [] };
const after: SpecState = { designedFrom: { F2: "new basis" }, artifactRevisions: { "specs/design/domain-model.md": 2 }, documents: [] };

let unsubscribe = () => {};
afterEach(() => unsubscribe());

/** A spec state shown on screen (an active query), served from `states` in turn, the last one for good. */
async function shown(states: SpecState[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let reads = 0;
  const observer = new QueryObserver(queryClient, {
    queryKey: specKey("lunch"),
    queryFn: () => states[Math.min(reads++, states.length - 1)]!,
  });
  unsubscribe = observer.subscribe(() => {});
  await observer.refetch();
  return { queryClient, reads: () => reads };
}

const DELAYS = [1, 1, 1, 1];

describe("readSpecStateUntilRecorded", () => {
  it("reads again until the turn's record shows, then stops", async () => {
    // At the end, then twice more before the record lands, then once with it.
    const spec = await shown([before, before, before, after]);
    await readSpecStateUntilRecorded(spec.queryClient, "lunch", DELAYS, new AbortController().signal);
    expect(spec.queryClient.getQueryData(specKey("lunch"))).toEqual(after);
    expect(spec.reads()).toBe(4);
  });

  it("keeps reading when only revisions move, until the basis the build gate reads changes", async () => {
    const revisionsOnly: SpecState = {
      designedFrom: { F2: "old basis" },
      artifactRevisions: { "specs/design/domain-model.md": 2 },
      documents: [],
    };
    const basis: SpecState = { ...revisionsOnly, designedFrom: { F2: "new basis" } };
    // At the end, then a read where the record's revisions have landed and its basis has not, then the basis.
    const spec = await shown([before, revisionsOnly, basis]);
    await readSpecStateUntilRecorded(spec.queryClient, "lunch", DELAYS, new AbortController().signal);
    expect(spec.queryClient.getQueryData(specKey("lunch"))).toEqual(basis);
    expect(spec.reads()).toBe(3);
  });

  it("stops at the first read when the record had already landed", async () => {
    const spec = await shown([before, after]);
    await readSpecStateUntilRecorded(spec.queryClient, "lunch", DELAYS, new AbortController().signal);
    expect(spec.reads()).toBe(2);
  });

  it("reads a bounded number of times when the turn changed nothing the record decides", async () => {
    const spec = await shown([before]);
    await readSpecStateUntilRecorded(spec.queryClient, "lunch", DELAYS, new AbortController().signal);
    expect(spec.reads()).toBe(1 + 1 + DELAYS.length);
  });

  it("reads once after a failed turn", async () => {
    const spec = await shown([before]);
    await readSpecStateUntilRecorded(spec.queryClient, "lunch", [], new AbortController().signal);
    expect(spec.reads()).toBe(2);
  });

  it("stops when a later turn's end takes over", async () => {
    const spec = await shown([before]);
    const controller = new AbortController();
    const reading = readSpecStateUntilRecorded(spec.queryClient, "lunch", [50, 50, 50], controller.signal);
    controller.abort();
    await reading;
    expect(spec.reads()).toBe(2);
  });

  it("reads once a spec state nothing has read yet", async () => {
    const queryClient = new QueryClient();
    await readSpecStateUntilRecorded(queryClient, "lunch", DELAYS, new AbortController().signal);
    expect(queryClient.getQueryData(specKey("lunch"))).toBeUndefined();
  });
});

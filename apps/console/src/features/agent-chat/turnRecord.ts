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

import type { QueryClient } from "@tanstack/react-query";
import type { components } from "../../generated/aep-api";
import { specKey } from "../spec/api/specModel";

// A turn's record reaches aep-api after the turn's end. The design agent ends
// the turn's stream first, then hands the turn's record to its usage outbox,
// which posts it through ae-studio-tools (ae-design-agent `TurnDesk.finish`).
// What aep-api works out from that record is in the spec state only once it
// lands: a design run's basis, read where the run ended (`designedFrom`), and
// each design file's revision (`artifactRevisions`). Read only at the turn's
// end, the spec state is usually the one from before the run, and the Design
// card would say the design it just made is out of date — or not designed
// yet — until a reload.
//
// So after a completed turn the spec state is read again, backing off, until
// the basis changes, for a bounded while (the outbox retries every 2 s while
// its socket is down, then the tools side holds the record for its coalesce
// window). Revisions can move on an earlier read, while the commit the basis
// is taken from is not readable yet. The build gate reads the basis ("not
// designed yet", "design out of date"), so that read is not the one to stop
// on. A turn that changed none of it (an interview, a reply) reads it a few
// more times and stops.

type SpecState = components["schemas"]["SpecState"];

/** The waits between reads after a completed turn: about 15 s in all. */
export const RECORD_READ_DELAYS_MS: readonly number[] = [500, 1_000, 2_000, 4_000, 8_000];

/** The basis the build gate reads, comparable; undefined before the state was read. */
function recorded(state: SpecState | undefined): string | undefined {
  return state && JSON.stringify(state.designedFrom);
}

function wait(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}

/**
 * Read the project's spec state again now, then after each of `delays` until
 * the design basis (`designedFrom`) differs from what it was when the turn
 * ended. No delays (a failed turn) reads it once. A state never read (nothing
 * shows it) is read once: whatever shows it later reads it fresh. `signal`
 * stops the reads (a later turn's end, or the shell unmounting).
 */
export async function readSpecStateUntilRecorded(
  queryClient: QueryClient,
  projectName: string,
  delays: readonly number[],
  signal: AbortSignal,
): Promise<void> {
  const key = specKey(projectName);
  const atEnd = recorded(queryClient.getQueryData<SpecState>(key));
  await queryClient.invalidateQueries({ queryKey: key });
  if (atEnd === undefined) return;
  for (const ms of delays) {
    if (signal.aborted || recorded(queryClient.getQueryData<SpecState>(key)) !== atEnd) return;
    await wait(ms, signal);
    if (signal.aborted) return;
    await queryClient.invalidateQueries({ queryKey: key });
  }
}

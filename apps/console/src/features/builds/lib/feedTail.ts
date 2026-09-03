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

import type { RunProgressPhase } from "../hooks/useRunProgress";

// THE FOOTNOTE UNDER A RUN FEED, in two halves that answer different questions.
//
// `connectionTail` is about the STREAM — is the console attached? Every surface
// that opens a run feed reports it, so the sentence lives here rather than being
// retyped per surface (it was, in RunFeed and RunSpine, and only one of the two
// would have been fixed).
//
// `settledLabel` is about the RUN — how did it end? It labels the section
// HEADER, beside the title, the way the Tasks header carries its counts; the
// body keeps the connection half, because stream health is not a fact about the
// run and the header cannot know it.

/** What the CONNECTION is doing, when that is worth saying at all. A live or
 *  never-opened stream says nothing — silence is the healthy state. */
export function connectionTail(phase: RunProgressPhase): string | undefined {
  switch (phase) {
    case "connecting":
      return "Attaching to the run feed…";
    case "reconnecting":
      return "Connection lost — reconnecting…";
    default:
      return undefined;
  }
}

/**
 * How the run ENDED, as the sentence beside the section title.
 *
 * This used to read `run settled — succeeded` under the log: the platform's own
 * term for the transition, with the raw state pasted after it in the lower-case
 * spelling the wire uses.
 *
 * It is derived from the RUN, not from the stream's `done` frame, even though
 * that frame is the more immediate signal. The header's other half — the
 * streaming note — already reads the run list, and two halves of one label
 * drawn from two sources can contradict each other for as long as they disagree.
 * One source, one answer.
 *
 * Deliberately UNTONED: it renders as the same secondary caption the Tasks
 * header uses for its counts, so the two section headers read as one system.
 * The run's outcome is already carried in colour by the page header's own
 * status chip; a second toned pill here would compete with it.
 *
 * This switches over the same states as `runStatus` in `runView`, and that
 * duplication is deliberate rather than a helper waiting to be extracted: the
 * two answer different questions in different slots. `runStatus` names the run
 * ("Succeeded", with a tone, in a chip); this names what HAPPENED to it, as a
 * sentence, untoned. Folding them together would force one phrasing into both.
 * They must stay on one VOCABULARY — a state `runStatus` learns to name is a
 * state this owes a sentence — which is what the default case is for until it
 * has one.
 */
export function settledLabel(state: string | undefined): string {
  switch (state) {
    case "succeeded":
      return "Run finished successfully";
    case "failed":
      return "Run failed";
    case "cancelled":
      return "Run cancelled";
    case "blocked":
      return "Run blocked";
    default:
      // An unrecognised state renders rather than hiding, for the same reason
      // `runStatus` renders one: a state nobody wrote a sentence for is still a
      // fact about the run, and swallowing it leaves the section looking
      // cleanly finished when the platform said something else.
      return state ? `Run ended — ${state}` : "Run finished";
  }
}

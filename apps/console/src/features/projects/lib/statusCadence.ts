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

import type { components } from "../../../generated/aep-api";

type ProjectStatus = components["schemas"]["ProjectStatus"];

/**
 * Which project states put the status poll on its fast cadence.
 *
 * The read behind it is not cheap — it fans out to four backends, OpenChoreo
 * included — and the hook that calls it runs on EVERY route, because the
 * toolbar badge is unconditional. So the question this answers is not "might
 * something change" but "is something changing NOW, and will it stop": a state
 * that can persist indefinitely must not appear here, however interesting it is.
 */
export function statusIsMoving(status: ProjectStatus): boolean {
  return (
    // An agent writing the spec is the FIRST thing that moves on a new project
    // and the longest stretch a first-timer watches (#562): the kickoff fires
    // at creation, so the overview's opening minutes are exactly this state.
    // Without it the spec card would sit on the idle interval through the whole
    // interview and take up to 30s to notice it had finished.
    status.spec.agent === "working" ||
    // Mid-interview: a turn HAS run (so not "never-started") and written no
    // spec yet, and `agent` returns to "" in every gap between turns — keying
    // only on "working" drops the whole interview onto the idle cadence and
    // leaves every surface up to 30s behind the agent it describes.
    //
    // Scoped to a project that actually has turn history, so an abandoned or
    // never-started one is not parked on the fast cadence forever: this poll
    // fans out to four backend sources, OpenChoreo included.
    (status.spec.agent === "" && !status.spec.exists) ||
    status.build.status === "running" ||
    status.deploy.status === "deploying" ||
    // A validation CYCLE in flight, and the coding cycle repairing a failed one.
    // Both are bounded work ending in a verdict, and without them the verdict
    // itself lands up to 30s late (#649). `awaiting-fix` is the narrower of the
    // two by some way — it needs a LIVE run already holding a fatal verdict,
    // which in practice means an `unreported` re-dispatch — but it costs nothing
    // and it is the state the rail renders while the platform self-heals.
    status.deploy.validation === "running" ||
    status.deploy.validation === "awaiting-fix" ||
    status.repoStatus === "pending" ||
    status.repoStatus === "cloning"
    // `none` is deliberately NOT here, though it is the state the page spends
    // the deploy→validating gap in and the obvious thing to add.
    //
    // It is two states wearing one name: a verdict is expected, or none is ever
    // coming. A validation run that settles without recording one — quota
    // blocked, publisher credentials, its re-dispatch budget spent — closes the
    // version's task on the way out, so nothing restarts it, and the read model
    // reports `none` for good. `build.status` cannot screen those off either: it
    // is derived from the newest DEV run, which succeeded. Every clause anyone
    // can write here would be true forever, on every route, for a project in a
    // state a person is not even watching.
    //
    // The wait itself is answered where it is caused: a settling run now
    // reconciles its own milestone, so the gap is seconds rather than a sweep
    // interval, and what remains is one idle poll before "Validating" appears.
    // Telling the two apart needs the read model to stop conflating them, which
    // is a contract change and its own issue.
  );
}

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

import type { ProjectBuild } from "../features/builds/api/builds";
import { runScript, type RunInput, type RunScript } from "./fixtures/buildRun";

// Each project's builds, as the mock server keeps them: the version, the
// features it built and the spec each was built from (the provisional list,
// mock-only until B1; see features/builds/api/builds.ts), and the run that
// builds it (fixtures/buildRun.ts), scheduled from its start time.
//
// Kept in sessionStorage, as the conversation is (chatServer.ts): a reload
// lands in the middle of a run and reattaches, and the design the build was
// made from outlives the reload too. A build's status is read off its run's
// clock, so "building" turns into "built" by itself.

export interface MockBuild {
  projectName: string;
  build: Omit<ProjectBuild, "status">;
  run: RunInput;
}

const KEY = "aep:mock:builds";

function read(): MockBuild[] {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (raw) return JSON.parse(raw) as MockBuild[];
  } catch {
    // unreadable: start over
  }
  return [];
}

function write(builds: MockBuild[]): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(builds));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}

const scripts = new Map<string, RunScript>();

/** The build's run script, made once per run. */
export function scriptOf(build: MockBuild): RunScript {
  let script = scripts.get(build.run.runId);
  if (!script) {
    script = runScript(build.run);
    scripts.set(build.run.runId, script);
  }
  return script;
}

/** How far into its run a build is, in ms. */
export function elapsed(build: MockBuild, now = Date.now()): number {
  return now - build.run.startedAt;
}

/** The project's builds as the mock keeps them, oldest first. */
export function mockBuilds(projectName: string): MockBuild[] {
  return read().filter((b) => b.projectName === projectName);
}

/** The project's builds, oldest first, each with its status read off its run. */
export function projectBuilds(projectName: string, now = Date.now()): ProjectBuild[] {
  return mockBuilds(projectName).map((b) => ({
    ...b.build,
    status: elapsed(b, now) >= scriptOf(b).end ? "built" : "building",
  }));
}

export function addBuild(build: MockBuild): void {
  write([...read(), build]);
}

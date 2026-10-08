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

import { useEffect, useRef, useState } from "react";
import { chatStore } from "../../agent-chat/useProjectChat";
import { useBuilds, type BuildStatus } from "../api/builds";
import { finishedNote, nextSteps } from "../model/nextSteps";
import { useBuildOutcome } from "./useBuildOutcome";
import { useNextInterview } from "./useNextInterview";

/**
 * The chat's one line when a build ends, wherever the user is in the project:
 * its result, and what it offers next. A build that ends while the page is
 * watching is announced once; one that ended before the page loaded is not,
 * as the conversation's history does not carry these lines.
 */
export function useBuildNotes(projectName: string): void {
  const builds = useBuilds(projectName).data;
  const seen = useRef<Map<string, BuildStatus> | null>(null);
  const [finished, setFinished] = useState<string | null>(null);

  useEffect(() => {
    if (!builds) return;
    const before = seen.current;
    seen.current = new Map(builds.map((b) => [b.version, b.status]));
    const ended = before && builds.find((b) => b.status !== "building" && before.get(b.version) === "building");
    if (ended) setFinished(ended.version);
  }, [builds]);

  const result = useBuildOutcome(projectName, finished ?? undefined);
  const nextInterview = useNextInterview(projectName);

  useEffect(() => {
    if (!finished || !builds || !result.outcome || !result.groups) return;
    const fix = builds.find((b) => b.fixes === finished);
    const next = nextSteps({
      version: finished,
      outcome: result.outcome,
      fixedBy: fix ? { version: fix.version, building: fix.status === "building" } : null,
      latest: builds.at(-1)?.version ?? finished,
      nextInterview,
    });
    const note = finishedNote(finished, result.groups, result.outcome, next);
    chatStore.post(projectName, note.text, note.actions);
    setFinished(null);
  }, [finished, builds, result.outcome, result.groups, nextInterview, projectName]);
}

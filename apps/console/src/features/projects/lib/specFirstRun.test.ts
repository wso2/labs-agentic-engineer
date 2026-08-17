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

// The first-run derivation (#485). Two sources, and which one wins when: the
// backend's kickoff report knows the seconds before any turn exists and knows a
// kickoff that DIED; the live chat log knows what the turn is doing right now,
// seconds ahead of the status poll.

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import type { InterviewState } from "../../agent-chat/interviewState";
import { specFirstRunView } from "./specFirstRun";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type Kickoff = ProjectStatus["spec"]["kickoff"];

const IDLE: InterviewState = { turnRunning: false, pendingQuestions: 0 };

function status(kickoff: Kickoff, exists = false): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: exists,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: { exists, version: "", dirty: false, design: false, kickoff },
    build: { version: "", status: "idle" },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
    },
  };
}

describe("specFirstRunView", () => {
  it("says nothing before the status poll has landed", () => {
    expect(specFirstRunView(undefined, IDLE).stage).toBe("none");
  });

  it("says nothing for a project that never had a kickoff", () => {
    expect(specFirstRunView(status({ status: "none", reason: "" }), IDLE).stage).toBe("none");
  });

  it("reports a claimed kickoff before any turn reaches this browser", () => {
    const view = specFirstRunView(status({ status: "pending", reason: "" }), IDLE);
    expect(view.stage).toBe("starting");
    expect(view.open).toBe(true);
    expect(view.line).toBe("Agent is starting the interview");
  });

  it("reports the running turn once the log has it", () => {
    const view = specFirstRunView(status({ status: "started", reason: "" }), {
      turnRunning: true,
      pendingQuestions: 0,
    });
    expect(view.stage).toBe("reading");
    expect(view.line).toBe("Agent is looking at your idea");
  });

  // The log is ahead of the 5s poll, so a user watching questions arrive must
  // not be told the agent is still "starting".
  it("lets pending questions win over both the claim and the running turn", () => {
    const view = specFirstRunView(status({ status: "pending", reason: "" }), {
      turnRunning: true,
      pendingQuestions: 3,
    });
    expect(view.stage).toBe("questions");
    expect(view.questions).toBe(3);
    expect(view.line).toBe("Agent has 3 questions about your idea");
  });

  it("carries the failure's reason, and closes the interview", () => {
    const view = specFirstRunView(
      status({ status: "failed", reason: "The agents service was unreachable." }),
      IDLE,
    );
    expect(view.stage).toBe("failed");
    expect(view.open).toBe(false);
    expect(view.reason).toBe("The agents service was unreachable.");
  });

  // Once a document exists it is the answer, so nothing here has anything left
  // to say — which is also why the backend stops reporting on it.
  it("falls silent once the spec exists, whatever the claim says", () => {
    const view = specFirstRunView(status({ status: "failed", reason: "stale" }, true), IDLE);
    expect(view.stage).toBe("none");
    expect(view.reason).toBe("");
  });

  // An older backend serves a spec stage with no kickoff at all. That is not a
  // first run, and it must not throw on a rendering path.
  it("survives a spec stage with no kickoff field", () => {
    const stale = status({ status: "pending", reason: "" });
    delete (stale.spec as { kickoff?: unknown }).kickoff;
    expect(specFirstRunView(stale, IDLE).stage).toBe("none");
  });
});

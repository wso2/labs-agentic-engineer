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
import type { components } from "../../../generated/aep-api";
import { trackView, type LegState } from "./track";

type ProjectStatus = components["schemas"]["ProjectStatus"];

function status(over: {
  spec?: Partial<ProjectStatus["spec"]>;
  build?: Partial<ProjectStatus["build"]>;
  deploy?: Partial<ProjectStatus["deploy"]>;
}): ProjectStatus {
  return {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: "",
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "",
    designStatus: "",
    spec: { exists: true, version: "", dirty: false, design: false, agent: "", ...over.spec },
    build: { version: "", status: "idle", ...over.build },
    deploy: {
      version: "",
      status: "none",
      components: { total: 0, ready: 0 },
      validation: "none",
      ...over.deploy,
    },
  };
}

/** The three legs' states, in flow order — the shape most assertions want. */
const states = (s: ProjectStatus, engaged = false): LegState[] =>
  trackView(s, engaged).legs.map((l) => l.state);

const leg = (s: ProjectStatus, i: number, engaged = false) =>
  trackView(s, engaged).legs[i]!;

describe("the track is always three legs", () => {
  it("names them in flow order whatever the state", () => {
    const view = trackView(status({}), false);
    expect(view.legs.map((l) => l.name)).toEqual(["Spec", "Build", "Deploy"]);
  });

  it("points every leg at its own section", () => {
    const view = trackView(status({}), false);
    expect(view.legs.map((l) => l.to)).toEqual([
      "/projects/$projectName/spec",
      "/projects/$projectName/builds",
      "/projects/$projectName/deployments",
    ]);
  });
});

describe("the spec leg", () => {
  it("says nothing is written when the kickoff never ran", () => {
    const l = leg(status({ spec: { exists: false } }), 0);
    expect(l.state).toBe("waiting");
    expect(l.line).toBe("Nothing written yet");
    expect(l.version).toBe("");
  });

  it("names the work while the kickoff runs", () => {
    const l = leg(status({ spec: { exists: false, agent: "working" } }), 0);
    expect(l.state).toBe("live");
    expect(l.line).toBe("The agent is writing your requirements");
  });

  it("says the agent is on the spec once requirements exist", () => {
    const l = leg(status({ spec: { exists: true, agent: "working" } }), 0);
    expect(l.line).toBe("The agent is working on your spec");
  });

  // The state no server field can produce, and the reason the local chat log
  // is read at all.
  it("holds — not pulses — when the agent asked a question", () => {
    const l = leg(status({ spec: { version: "v2" } }), 0, true);
    expect(l.state).toBe("hold");
    expect(l.line).toBe("The agent has questions for you");
  });

  it("keeps the version under a live line", () => {
    const l = leg(status({ spec: { version: "v2", agent: "working" } }), 0);
    expect(l.version).toBe("v2");
  });

  it("fails only when the kickoff wrote nothing at all", () => {
    expect(leg(status({ spec: { exists: false, agent: "failed" } }), 0).state).toBe("failed");
    // A failed design turn over a published spec is not a spec that failed to
    // start — the spec view banners that, the track does not.
    expect(leg(status({ spec: { version: "v1", agent: "failed" } }), 0).state).toBe("done");
  });

  // The one leg state that is somewhere to GO AND ACT rather than somewhere to
  // look. The wording matches the chat panel's questions pointer on purpose:
  // that pointer's handler is `openSpec`, which is this leg's `to`, so both
  // surfaces name one action that lands in one place.
  it("tells the reader how to answer the agent's questions", () => {
    const l = leg(status({ spec: { version: "v2" } }), 0, true);
    expect(l.cta).toBe("Answer them");
  });

  it("offers the retry as a call to action, not as a second sentence", () => {
    const l = leg(status({ spec: { exists: false, agent: "failed" } }), 0);
    expect(l.line).toBe("The agent couldn't start");
    expect(l.cta).toBe("Try again");
  });

  // A cta on a leg with nothing to do would make every visit look like a
  // request. Only the two states above carry one.
  it.each([
    ["published", { version: "v1" }],
    ["a live agent turn", { agent: "working" }],
    ["a draft", { version: "" }],
  ] as const)("leaves %s without a call to action", (_name, spec) => {
    expect(leg(status({ spec }), 0).cta).toBeUndefined();
  });

  it("holds a draft nobody published", () => {
    expect(leg(status({ spec: { version: "" } }), 0).state).toBe("hold");
    expect(leg(status({ spec: { version: "v1", dirty: true } }), 0).line).toBe(
      "Draft changes, not published",
    );
  });
});

describe("the build leg", () => {
  it("says the user's situation, not the system's dependency", () => {
    const l = leg(status({}), 1);
    expect(l.line).toBe("Nothing built yet");
    // No em-dash placeholder for a version that does not exist.
    expect(l.version).toBe("");
  });

  it.each([
    ["running", "live", "Building"],
    ["succeeded", "done", "Built"],
    ["failed", "failed", "Build failed"],
  ] as const)("renders %s", (state, expected, line) => {
    const l = leg(status({ build: { version: "v1", status: state } }), 1);
    expect(l.state).toBe(expected);
    expect(l.line).toBe(line);
    expect(l.version).toBe("v1");
  });
});

describe("the deploy leg", () => {
  it("counts components while deploying", () => {
    const l = leg(
      status({
        deploy: { version: "v1", status: "deploying", components: { total: 2, ready: 1 } },
      }),
      2,
    );
    expect(l.state).toBe("live");
    expect(l.line).toBe("Deploying · 1/2 components");
  });

  // Validation rides this line rather than becoming a fourth leg.
  it("appends the validation state to a live deployment", () => {
    const l = leg(
      status({ deploy: { version: "v1", status: "deployed", validation: "running" } }),
      2,
    );
    expect(l.line).toMatch(/^Live in dev · /);
  });

  // Being a phase of DEPLOYING means the leg stays unsettled through it. This
  // used to settle to `done` the moment the components came up, so the track
  // went dark in the middle of the stage it was reporting on — the deployments
  // board was the only page that admitted validation was still running.
  //
  // `live`, not `hold`, for both: the platform is doing the work and it ends on
  // its own. `awaiting-fix` is a coding cycle repairing what validation found,
  // which is no more the reader's turn than the validation cycle was.
  it.each(["running", "awaiting-fix"] as const)(
    "keeps the deploy leg live while validation is %s",
    (validation) => {
      const l = leg(
        status({ deploy: { version: "v1", status: "deployed", validation } }),
        2,
      );
      expect(l.state).toBe("live");
      expect(l.version).toBe("v1");
    },
  );

  // The binding read lags the run: a validation cycle can be in flight while
  // the deploy status still reads `none`. The stage is what is happening, not
  // what the slower of two reads has caught up to.
  it("lights the leg for a validation in flight the deploy read has not caught up to", () => {
    const l = leg(
      status({ deploy: { version: "v1", status: "none", validation: "running" } }),
      2,
    );
    expect(l.state).toBe("live");
  });

  // A verdict is not work. Nothing is running, so nothing pulses — the whole
  // point of reserving `live` is that it keeps meaning "the platform is busy".
  it.each([
    ["passed", "done"],
    ["partial", "done"],
    ["skipped", "done"],
    ["inconclusive", "done"],
  ] as const)("settles the leg on the %s verdict", (validation, state) => {
    const l = leg(
      status({ deploy: { version: "v1", status: "deployed", validation } }),
      2,
    );
    expect(l.state).toBe(state);
  });

  it("fails the leg on a red verdict but keeps the version", () => {
    const l = leg(
      status({ deploy: { version: "v1", status: "deployed", validation: "failed" } }),
      2,
    );
    // The version really is what is running in dev — losing the chip here
    // would read as nothing deployed.
    expect(l.version).toBe("v1");
    expect(l.state).toBe("failed");
  });

  it("trusts validation over a lagging deploy read", () => {
    const l = leg(status({ deploy: { version: "v1", validation: "running" } }), 2);
    expect(l.line).toMatch(/^Live in dev/);
    expect(l.state).not.toBe("waiting");
  });
});

// The scenario that killed the "exactly one active card" rule the stage cards
// were built on.
describe("more than one leg can be unsettled", () => {
  it("holds the spec and pulses the build during an amendment", () => {
    const s = status({
      spec: { version: "v1", dirty: true },
      build: { version: "v1", status: "running" },
    });
    expect(states(s)).toEqual(["hold", "live", "waiting"]);
  });
});

describe("the summary earns its slot or is absent", () => {
  it("says nothing when the legs already said it", () => {
    expect(trackView(status({ build: { version: "v1", status: "running" } }), false).summary)
      .toBeNull();
    expect(trackView(status({ build: { version: "v1", status: "failed" } }), false).summary)
      .toBeNull();
    expect(trackView(status({ spec: { version: "v1" } }), false).summary).toBeNull();
  });

  it("warns that draft changes are not in the running build", () => {
    const view = trackView(
      status({
        spec: { version: "v1", dirty: true },
        build: { version: "v1", status: "running" },
      }),
      false,
    );
    expect(view.summary?.tone).toBe("warning");
    expect(view.summary?.text).toContain("without your draft changes");
  });

  it("relates a newer build to the version still serving dev", () => {
    const view = trackView(
      status({
        spec: { version: "v2" },
        build: { version: "v2", status: "running" },
        deploy: { version: "v1", status: "deployed" },
      }),
      false,
    );
    expect(view.summary?.text).toBe(
      "Building v2. v1 stays live in dev until it deploys.",
    );
  });

  // Both triggers at once: the one the user can act on wins.
  it("prefers the amendment warning over the drift note", () => {
    const view = trackView(
      status({
        spec: { version: "v2", dirty: true },
        build: { version: "v2", status: "running" },
        deploy: { version: "v1", status: "deployed" },
      }),
      false,
    );
    expect(view.summary?.tone).toBe("warning");
  });
});

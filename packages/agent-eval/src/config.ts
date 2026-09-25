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

import type { ScenarioFile } from "./scenario.js";

export const THRESHOLD = 0.8;

/**
 * A scenario file becomes a promptfoo config.
 *
 * `mustCover` lines are weighted rubric assertions and average into the
 * score. `mustNot` lines are NOT — each gets `weight: 0` so it takes no share
 * of that mean (leaving it in, even at the default weight, would let a
 * passing mustNot inflate the score and a failing one deflate it, so "0.8 of
 * mustCover weight" would stop meaning what it says). A mustNot's veto still
 * happens, just not through the mean: `threshold: 1` fails its OWN assertion
 * outright, and the verdict reads that failure straight from
 * `componentResults` rather than from the blended score. A rubric that
 * tolerates inventing a price 20% of the time is not a rubric.
 */
export function buildPromptfooConfig(
  file: ScenarioFile,
  opts: { providerPath: string; graderModel: string; providerConfig?: Record<string, unknown> },
): unknown {
  // An unpinned or missing grader makes a score meaningless between runs —
  // this is the whole harness's unit of measure, so refuse to build a config
  // that would silently grade with "whatever the caller forgot to set".
  if (opts.graderModel.trim() === "") {
    throw new Error("buildPromptfooConfig: graderModel must not be blank");
  }
  return {
    description: `agent evaluation — ${file.component}`,
    // The provider's `config` is how the agent under test reaches this run:
    // `vars` is JSON, so it can carry a scenario but never the FUNCTION that
    // talks to an agent. Everything here is serializable and none of it is a
    // credential — the config file lands in the build's output directory.
    providers: [
      { id: `file://${opts.providerPath}`, config: opts.providerConfig ?? {} },
    ],
    prompts: ["{{scenario.brief.goal}}"],
    defaultTest: { options: { provider: opts.graderModel } },
    tests: file.scenarios.map((s) => ({
      description: `${s.id}: ${s.brief.goal}`,
      vars: { scenario: s },
      assert: [
        ...s.rubric.mustCover.map((m) => ({
          type: "llm-rubric",
          metric: m.id,
          weight: m.weight,
          value: `${m.must}\n\nTranscript:\n{{output}}`,
        })),
        ...s.rubric.mustNot.map((m) => ({
          type: "llm-rubric",
          metric: m.id,
          weight: 0,
          threshold: 1,
          value: `The agent did NOT do this: ${m.mustNot}\n\nTranscript:\n{{output}}`,
        })),
      ],
    })),
  };
}

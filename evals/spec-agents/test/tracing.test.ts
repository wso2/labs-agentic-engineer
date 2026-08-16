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
 * Session visibility in a run's transcript (#486). A grilling session and a
 * one-form interview are both "the agent asked questions"; the checklist is the
 * only thing that tells them apart, so a reviewer reading a transcript has to
 * see it without decoding tool-call JSON.
 */

import { strict as assert } from "node:assert";
import { test } from "node:test";
import { newTurnRecord, renderSessionLine, renderTranscript } from "../src/tracing.js";

test("renderSessionLine: every area shows, marked by its state", () => {
  const line = renderSessionLine({
    title: "Voting & nominations",
    areas: [
      { name: "Eligibility", state: "done" },
      { name: "Quorum", state: "now" },
      { name: "Nominee limits", state: "todo" },
    ],
  });
  assert.equal(line, 'session "Voting & nominations": ✔ Eligibility · ▸ Quorum · ○ Nominee limits');
});

test("renderSessionLine: a titleless session still renders its areas", () => {
  assert.equal(renderSessionLine({ areas: [{ name: "Reviews", state: "now" }] }), "session: ▸ Reviews");
});

test("renderTranscript: a session round is labelled as one", () => {
  const rec = newTurnRecord("requirements", 2, "grill me properly on the voting rules");
  rec.questions = ["Who may vote?"];
  rec.session = { title: "Voting rules", areas: [{ name: "Eligibility", state: "now" }] };
  const out = renderTranscript("case", [rec], {});
  assert.match(out, /### Grilling session round/);
  assert.match(out, /▸ Eligibility/);
});

test("renderTranscript: a one-form interview gets no session heading", () => {
  const rec = newTurnRecord("requirements", 1, "/start");
  rec.questions = ["Who may vote?"];
  const out = renderTranscript("case", [rec], {});
  assert.doesNotMatch(out, /Grilling session round/);
});

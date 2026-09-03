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

// The `/design` mock stream (#576): a design run that DECLARES its plan.
//
// Exercises every grilled state: the plan arriving in two waves (the cell
// first — only the cell fixes the component set), the union dedupe (wave two
// restates design.md and nothing doubles), planned → writing → done off the
// ordinary file frames, and — with `fail` in the instruction — a turn that
// dies mid-write, leaving done ticks, one error, and a ghost.
//
// Wave one publishes through the streamed-input path (tool-input-start/delta/
// end) AND the complete tool-call, the belt-and-braces pair the fold dedupes;
// wave two is a bare tool-call. Both real paths stay covered.

import { DECLARE_PLAN_TOOL } from "@aep/agent-stream";

const CELL = "specs/design/design.cell";
const OVERVIEW = "specs/design/design.md";
const SECURITY = "specs/design/security.json";
const PORTAL = "specs/design/components/expense-portal/design.json";
const API = "specs/design/components/expense-api/design.json";
const CRITERIA = "specs/validation/validation-criteria.json";

// The document each write delivers, as plain text — `addFile` below is what
// escapes it for the JSON input stream, so a document may hold quotes and
// newlines without being hand-escaped here.
const CONTENT: Record<string, string> = {
  [CELL]:
    "cell expense-approval {\n  component expense-portal kind: web-app\n  component expense-api kind: service\n}",
  [OVERVIEW]:
    "# Design overview\n\nTwo components: the portal is the user-facing surface, the API owns the expense record.",
  [SECURITY]: JSON.stringify(
    {
      version: 1,
      coldStartRole: "approver",
      publicComponents: [],
      roles: [
        {
          name: "approver",
          description: "Approves submitted expenses.",
          stories: [1],
          grantedBy: "Platform IdP",
          permissions: [{ component: "expense-api", actions: ["approve"] }],
        },
      ],
      testUsers: [{ username: "test-approver", role: "approver" }],
      thunder: { name: "expense-approval", type: "browser" },
    },
    null,
    2,
  ),
  [PORTAL]: '{"componentName":"expense-portal","kind":"web-app"}',
  [API]: '{"componentName":"expense-api","kind":"service"}',
  [CRITERIA]: '{"criteria":[{"story":1,"then":"submission is refused without a receipt"}]}',
};

type MockFrame = Record<string, unknown>;

/** The start/delta/end/result quartet of one mock file write. */
function addFile(id: string, path: string): MockFrame[] {
  return [
    { type: "tool-input-start", id, toolName: "addFile" },
    { type: "tool-input-delta", id, delta: `{"path":"${path}","content":"` },
    // JSON.stringify minus its own quotes: the document goes down the wire
    // escaped for the string it is being spliced into, so a quote or a newline
    // in the content cannot break the input the fold parses.
    {
      type: "tool-input-delta",
      id,
      delta: `${JSON.stringify(CONTENT[path] ?? "").slice(1, -1)}"}`,
    },
    { type: "tool-input-end", id },
    {
      type: "tool-result",
      toolName: "addFile",
      toolCallId: id,
      input: { path },
      output: { ok: true, op: "add", path, status: "applied" },
    },
  ];
}

export function designPlanFrames(turnId: string, failing: boolean): MockFrame[] {
  const wave1 = { paths: [CELL, OVERVIEW, SECURITY] };
  // OVERVIEW is restated on purpose — the union must dedupe it.
  const wave2 = { paths: [OVERVIEW, PORTAL, API, CRITERIA] };
  const frames: MockFrame[] = [
    { type: "text-delta", delta: "Working on the design — reading the requirements first. " },
    // Wave one via the streamed-input path plus the complete call.
    { type: "tool-input-start", id: `plan-1-${turnId}`, toolName: DECLARE_PLAN_TOOL },
    {
      type: "tool-input-delta",
      id: `plan-1-${turnId}`,
      delta: JSON.stringify(wave1),
    },
    { type: "tool-input-end", id: `plan-1-${turnId}` },
    {
      type: "tool-call",
      toolCallId: `plan-1-${turnId}`,
      toolName: DECLARE_PLAN_TOOL,
      input: wave1,
    },
    {
      type: "tool-result",
      toolName: DECLARE_PLAN_TOOL,
      toolCallId: `plan-1-${turnId}`,
      input: wave1,
      output: { status: "ok", ...wave1 },
    },
    { type: "text-delta", delta: "Drawing the architecture… " },
    ...addFile(`f-cell-${turnId}`, CELL),
    { type: "text-delta", delta: "The architecture is settled — two components. " },
    {
      type: "tool-call",
      toolCallId: `plan-2-${turnId}`,
      toolName: DECLARE_PLAN_TOOL,
      input: wave2,
    },
    { type: "text-delta", delta: "Writing the design overview… " },
    ...addFile(`f-overview-${turnId}`, OVERVIEW),
    { type: "text-delta", delta: "Writing the security design… " },
    ...addFile(`f-security-${turnId}`, SECURITY),
    { type: "text-delta", delta: "Writing the design for the expense portal… " },
    ...addFile(`f-portal-${turnId}`, PORTAL),
    { type: "text-delta", delta: "Writing the design for the expense API… " },
  ];
  if (failing) {
    // Dies mid-write: the API design's input opens and its path resolves
    // (planned → writing), then the turn fails — error on the API design,
    // the validation criteria still a ghost.
    frames.push(
      { type: "tool-input-start", id: `f-api-${turnId}`, toolName: "addFile" },
      { type: "tool-input-delta", id: `f-api-${turnId}`, delta: `{"path":"${API}","content":"{` },
      { type: "turn-failed", message: "Mock design failure (instruction contained 'fail')." },
    );
    return frames;
  }
  frames.push(
    ...addFile(`f-api-${turnId}`, API),
    { type: "text-delta", delta: "Writing the validation criteria… " },
    ...addFile(`f-criteria-${turnId}`, CRITERIA),
    { type: "text-delta", delta: "\n\nThe design is ready — read it at your pace." },
    { type: "turn-committed", noChanges: true },
  );
  return frames;
}

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
 * What resolveHandoff derives from a caller's ae_create_issue arguments: the
 * label set every SRE-filed issue carries. Adoption is not among these — it is
 * entirely aep-api's call (classification-driven), never a caller or operator
 * override; CreateIssueRequest in packages/contracts/api/v1/openapi.yaml has
 * no such property, and forwarding one 400s the whole request.
 *
 * project/componentName are trusted straight from the call. Both the RCA and
 * remediation OpenChoreo agents receive their project/component/environment
 * scope as platform-injected, non-negotiable input (their own prompts declare
 * "SCOPE ENFORCEMENT (non-negotiable)") — not something guessed from
 * telemetry — so a model-stated component value here is the value the platform
 * already gave the model. It is not a dedupe key. Dedupe is owned by aep-api,
 * which derives the stable incident identity from the trusted request context
 * and the create request fields it already validates.
 */

/**
 * Applied to every issue filed through this server.
 *
 * `incident` is load-bearing, not a human convenience: aep-api's recurrence
 * lookup queries GitHub with the dedupe label AND this one together
 * (internal/sourcecontrol/issue_service.go), so an issue filed without it drops
 * out of recurrence detection and a real recurrence reads as a first filing.
 * The authoritative name is `LabelSREAgent`, declared in
 * internal/sourcecontrol/issue_recurrence.go; this is the one copy TypeScript
 * cannot import, so a test in handoffContext.test.ts reads that declaration
 * out of the Go source and pins this copy against it.
 */
export const HANDOFF_LABELS: readonly string[] = ["bug", "incident"];

export interface ResolvedHandoff {
  project: string;
  componentName?: string;
  labels: string[];
  actionStatuses: (string | null)[];
}

export function resolveHandoff(args: {
  project: string;
  componentName?: string;
  labels?: string[];
  actionStatuses: (string | null)[];
}): ResolvedHandoff {
  const labels = [...(args.labels ?? [])];
  for (const label of HANDOFF_LABELS) {
    if (!labels.includes(label)) labels.push(label);
  }

  const resolved: ResolvedHandoff = {
    project: args.project,
    labels,
    actionStatuses: args.actionStatuses,
  };
  if (args.componentName !== undefined) {
    resolved.componentName = args.componentName;
  }
  return resolved;
}

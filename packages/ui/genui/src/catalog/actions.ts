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

import { z } from "zod";
import type { GenUiActionDef } from "./types.js";

/**
 * The actions a generated UI may trigger. Declaring an action here only lets a
 * spec reference it; the host app decides what it does by passing a handler.
 */
export const genUiActions = {
  openTask: {
    params: z.object({ taskNumber: z.number().int().positive() }),
    description: "Open a delivery task (its GitHub issue) by number.",
  },
  approveDependency: {
    params: z.object({ dependencyId: z.string().min(1) }),
    description: "Approve a declared dependency so it can be provisioned.",
  },
  rejectDependency: {
    params: z.object({
      dependencyId: z.string().min(1),
      reason: z.string().optional(),
    }),
    description: "Reject a declared dependency, optionally saying why.",
  },
  openDeployments: {
    params: z.object({ version: z.string().min(1).optional() }),
    description: "Open the project's Deployments view, optionally at a version.",
  },
  editExternalResource: {
    params: z.object({ resourceName: z.string().min(1) }),
    description:
      "Open the configuration of an external resource (e.g. sendgrid) for editing.",
  },
} as const satisfies Record<string, GenUiActionDef>;

export type GenUiActionName = keyof typeof genUiActions;

export type GenUiActionParams<K extends GenUiActionName> = z.output<
  (typeof genUiActions)[K]["params"]
>;

/** Host-supplied handlers. An action without a handler is reported, not run. */
export type GenUiActionHandlers = {
  [K in GenUiActionName]?: (
    params: GenUiActionParams<K>,
  ) => void | Promise<void>;
};

/** What happened when a spec fired an action. */
export type GenUiDispatchOutcome =
  | { status: "handled"; action: GenUiActionName }
  | { status: "unhandled"; action: GenUiActionName }
  | { status: "unknown-action"; action: string }
  | { status: "invalid-params"; action: GenUiActionName; issues: string[] }
  | { status: "failed"; action: GenUiActionName; error: unknown };

function isGenUiActionName(name: string): name is GenUiActionName {
  return Object.hasOwn(genUiActions, name);
}

/**
 * The one gate every adapter routes actions through: the name must be in the
 * catalog and the params must pass its schema before any host code runs. A
 * model can therefore never reach a handler with an action or params the
 * catalog does not allow, whichever renderer library is underneath.
 */
export async function dispatchGenUiAction(
  handlers: GenUiActionHandlers,
  action: string,
  rawParams: unknown,
): Promise<GenUiDispatchOutcome> {
  if (!isGenUiActionName(action)) {
    return { status: "unknown-action", action };
  }
  const parsed = genUiActions[action].params.safeParse(rawParams ?? {});
  if (!parsed.success) {
    return {
      status: "invalid-params",
      action,
      issues: parsed.error.issues.map(
        (issue) => `${issue.path.join(".") || "(root)"}: ${issue.message}`,
      ),
    };
  }
  const handler = handlers[action] as
    | ((params: unknown) => void | Promise<void>)
    | undefined;
  if (!handler) {
    return { status: "unhandled", action };
  }
  try {
    // Sound: parsed.data was produced by genUiActions[action].params, the same
    // schema GenUiActionParams<typeof action> is derived from.
    await handler(parsed.data);
  } catch (error) {
    return { status: "failed", action, error };
  }
  return { status: "handled", action };
}

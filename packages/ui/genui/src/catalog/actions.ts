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
  replyToAgent: {
    // Generic on purpose: in chat the agent decides which fields to ask for,
    // so the answers' shape is the form's, not a fixed schema. The agent side
    // checks them against the form it sent (required fields, emails) and
    // answers with field errors in the usual shape.
    params: z.object({
      answers: z.record(z.string(), z.union([z.string(), z.boolean()])),
    }),
    description:
      "Send the user's answers to the agent that asked. params: { answers: { \"$state\": \"/answers\" } }, the form's fields bound under /answers.",
  },
  openDeployments: {
    params: z.object({ version: z.string().min(1).optional() }),
    description: "Open the project's Deployments view, optionally at a version.",
  },
  createCustomer: {
    // The request body of POST /api/customers. The messages are what a user
    // sees beside the field when a value is missing or malformed.
    params: z.object({
      name: z.string({ error: "Enter the customer's name." }).trim().min(1, {
        error: "Enter the customer's name.",
      }),
      contactName: z.string({ error: "Enter a contact name." }).trim().min(1, {
        error: "Enter a contact name.",
      }),
      contactEmail: z
        .email({ error: "Enter a valid email address, like name@example.com." }),
      contactPhone: z.string().optional(),
    }),
    description:
      "Create a customer. Params come from the form's fields; the host sends the request.",
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

/** Field name → the message to show beside that field. */
export type GenUiFieldErrors = Record<string, string>;

/**
 * Thrown by a host handler when the request was refused for a reason the user
 * can fix, e.g. the server's own validation. The message is shown to the user
 * and each field error beside its field. Any other thrown error shows only a
 * generic message, so internal details never reach the screen.
 */
export class GenUiActionError extends Error {
  readonly fieldErrors: GenUiFieldErrors;

  constructor(message: string, fieldErrors: GenUiFieldErrors = {}) {
    super(message);
    this.name = "GenUiActionError";
    this.fieldErrors = fieldErrors;
  }
}

/** What happened when a spec fired an action. */
export type GenUiDispatchOutcome =
  | { status: "handled"; action: GenUiActionName }
  | { status: "unhandled"; action: GenUiActionName }
  | { status: "unknown-action"; action: string }
  | {
      status: "invalid-params";
      action: GenUiActionName;
      issues: string[];
      fieldErrors: GenUiFieldErrors;
    }
  | { status: "failed"; action: GenUiActionName; error: unknown };

/**
 * Where a generated UI can read an action's progress: its state at
 * `/actions/<actionName>`. The view writes it; a spec shows it with `$state`
 * bindings and `visible` conditions.
 */
export interface GenUiActionState {
  status: "pending" | "success" | "error";
  /** For the user: what went wrong, or nothing on success. */
  message?: string;
  fieldErrors: GenUiFieldErrors;
}

export const genUiActionStatePath = (action: GenUiActionName): string =>
  `/actions/${action}`;

const GENERIC_FAILURE = "Something went wrong. Try again.";

/** The state a spec sees once an action has settled. */
export function actionStateFor(outcome: GenUiDispatchOutcome): GenUiActionState {
  switch (outcome.status) {
    case "handled":
      return { status: "success", fieldErrors: {} };
    case "invalid-params":
      return {
        status: "error",
        message: "Check the highlighted fields.",
        fieldErrors: outcome.fieldErrors,
      };
    case "failed":
      return outcome.error instanceof GenUiActionError
        ? {
            status: "error",
            message: outcome.error.message,
            fieldErrors: outcome.error.fieldErrors,
          }
        : { status: "error", message: GENERIC_FAILURE, fieldErrors: {} };
    case "unhandled":
    case "unknown-action":
      return { status: "error", message: "This action is not available here.", fieldErrors: {} };
  }
}

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
    const fieldErrors: GenUiFieldErrors = {};
    for (const issue of parsed.error.issues) {
      const field = issue.path[0];
      // The first issue per field is the one to fix first.
      if (field !== undefined && !(String(field) in fieldErrors)) {
        fieldErrors[String(field)] = issue.message;
      }
    }
    return {
      status: "invalid-params",
      action,
      issues: parsed.error.issues.map(
        (issue) => `${issue.path.join(".") || "(root)"}: ${issue.message}`,
      ),
      fieldErrors,
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

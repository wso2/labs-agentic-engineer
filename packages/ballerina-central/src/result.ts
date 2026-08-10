/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Failures are values, not thrown classes.
 *
 * The reader has exactly one caller shape — a command whose stdout is a
 * Ballerina file and whose stderr is one JSON object — so the question every
 * failure has to answer is "what does the agent reading this do next". A
 * discriminated union answers it exhaustively: `exitCodeFor` switches over
 * `kind` with a `never` fallthrough, so a new failure mode fails the build
 * until someone decides what it costs the caller.
 */

/** One place a payload stopped matching the schema. */
export interface SchemaIssue {
  /** Dotted path into the Central payload, e.g. `docsData.modules.0.records`. */
  readonly path: string;
  readonly message: string;
}

export type Failure =
  /** The caller's arguments are wrong — nothing upstream was contacted. */
  | { readonly kind: "validation"; readonly message: string; readonly suggestion: string }
  /** Central has no such package, or no such version of it. */
  | { readonly kind: "package-not-found"; readonly qualified: string; readonly suggestion: string }
  /** Central answered, but not usefully — a 4xx/5xx, a network error, a bad body. */
  | {
      readonly kind: "upstream";
      readonly url: string;
      readonly attempts: number;
      readonly message: string;
      readonly status?: number;
    }
  /** Central did not answer inside the budget. */
  | { readonly kind: "timeout"; readonly url: string; readonly budgetMs: number }
  /** Central answered with a shape this reader does not understand. */
  | { readonly kind: "schema-drift"; readonly qualified: string; readonly issues: readonly SchemaIssue[] }
  /** The package is fine, but no module of it carries a written guide. */
  | { readonly kind: "no-readme"; readonly qualified: string; readonly suggestion: string };

export type Result<T> = { readonly ok: true; readonly value: T } | { readonly ok: false; readonly error: Failure };

export function ok<T>(value: T): Result<T> {
  return { ok: true, value };
}

export function err<T = never>(error: Failure): Result<T> {
  return { ok: false, error };
}

/**
 * Process exit code for a failure.
 *
 * Two codes, because the caller has two responses: a `validation` failure is
 * the caller's own mistake and re-running unchanged cannot help (exit 2, same
 * as a missing argument), while everything else is a fact about Central, the
 * network or the package itself, where running again could legitimately give a
 * different answer (exit 1). Each failure carries its own `suggestion` for what
 * to do about it; none of them is a licence to guess at a signature instead.
 */
export function exitCodeFor(failure: Failure): 1 | 2 {
  switch (failure.kind) {
    case "validation":
      return 2;
    case "package-not-found":
    case "upstream":
    case "timeout":
    case "schema-drift":
    case "no-readme":
      return 1;
    default: {
      const exhaustive: never = failure;
      return exhaustive;
    }
  }
}

/**
 * The one line a failing run writes to stderr. Kept as a single JSON object so
 * an agent can read it without a parser and a human can read it without tools;
 * `kind` is the field worth branching on.
 */
export function describeFailure(failure: Failure): string {
  return JSON.stringify(failure);
}

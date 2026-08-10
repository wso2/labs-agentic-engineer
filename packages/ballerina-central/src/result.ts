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
      readonly suggestion: string;
      readonly status?: number;
    }
  /** Central did not answer inside the budget. */
  | { readonly kind: "timeout"; readonly url: string; readonly budgetMs: number; readonly suggestion: string }
  /** Central answered with a shape this reader does not understand. */
  | {
      readonly kind: "schema-drift";
      readonly qualified: string;
      readonly issues: readonly SchemaIssue[];
      readonly suggestion: string;
    }
  /**
   * The package parsed, but no declaration matched the name the caller asked
   * for. `candidates` is what the index does hold — either near-misses of the
   * requested name, or the whole roster when there were none.
   */
  | {
      readonly kind: "symbol-not-found";
      readonly qualified: string;
      readonly requested: readonly string[];
      readonly candidates: readonly string[];
      readonly suggestion: string;
    };

/**
 * The suggestions for the three failures that fire when the outside world is
 * the problem, kept here rather than at each construction site because more than
 * one module raises each of them and an agent branching on `kind` should never
 * see two different instructions for the same condition.
 *
 * The `upstream` and `timeout` ones are what an agent does next during a Central
 * outage, which is when they fire and when the reader has nothing else to offer.
 * `schema-drift` deliberately addresses a human: no argument the agent can
 * change will make a payload this reader cannot parse parse.
 */
export const UPSTREAM_SUGGESTION =
  "Central answered badly. Run the same command once more; if it persists, write the code from what you already know and let `bal build` name what is wrong.";

export const TIMEOUT_SUGGESTION =
  "Central did not answer in time. Run the same command once more; a large package is slow on a cold fetch.";

export const SCHEMA_DRIFT_SUGGESTION =
  "Central's payload no longer matches this reader, so no change of arguments will help. Report the `issues` paths; do not treat this as licence to guess a signature.";

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
 * Two codes, because the caller has two responses: exit 2 means the recovery is
 * a change to the caller's own arguments and re-running them unchanged cannot
 * help, while exit 1 is a fact about Central, the network or the package, where
 * running again could legitimately give a different answer. `symbol-not-found`
 * is exit 2 on that rule and not by analogy: both its recoveries — a different
 * name, or `--refresh` — are edits to the argument list.
 *
 * Every failure carries its own `suggestion` for what to do about it, including
 * the three that used to omit one; none of them is a licence to guess at a
 * signature instead. `schema-drift`'s suggestion is the odd one out in that it
 * addresses a human rather than the agent, because that is whose problem it is.
 */
export function exitCodeFor(failure: Failure): 1 | 2 {
  switch (failure.kind) {
    case "validation":
    case "symbol-not-found":
      return 2;
    case "package-not-found":
    case "upstream":
    case "timeout":
    case "schema-drift":
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

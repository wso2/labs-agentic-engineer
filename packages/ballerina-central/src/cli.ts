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
 * `bal-library` — the command the ballerina skill drives.
 *
 * The stream discipline is the contract:
 *
 *   stdout   the requested document, and nothing else — no progress, no banner
 *   stderr   on failure, one JSON object matching `Failure`; or usage text
 *   exit 0   success, and stdout is COMPLETE
 *   exit 1   Central could not answer — retryable in principle, and never a
 *            licence to guess a signature
 *   exit 2   the arguments are wrong, `--help`, an unknown verb, an ambiguous
 *            client, or a `type` name that does not resolve
 *
 * VERB-FIRST, not mode flags. Four distinct nouns should not be four modifiers on
 * one command, and a LEADING verb is the form that is safe under version skew: a
 * verb has no `/`, so a stale binary fails it against the qualified-name regex at
 * exit 2. A verb placed after the package is the unsafe form — a stale binary
 * reads it as a version and reports `package-not-found` at exit 1, which the skill
 * teaches means "retry" — and that is why it stays rejected.
 *
 * Disambiguation, stated once: a first positional containing `/` is a package;
 * otherwise it is a verb.
 */

import { parseArgs as parseArgv } from "node:util";
import { NULL_CACHE } from "./cache/store.js";
import type { HttpOptions } from "./central/client.js";
import { loadPackage, type LoadedPackage, type ResolveOptions } from "./library.js";
import { parseQualifiedName, type QualifiedName } from "./qualified.js";
import { toSyntaxString } from "./render/document.js";
import { describeFailure, err, exitCodeFor, ok, type Failure, type Result } from "./result.js";
import { usage, VERBS, type Verb } from "./usage.js";
import { renderOpsView } from "./views/ops.js";
import { renderOverview } from "./views/overview.js";
import { renderTypeView } from "./views/type.js";

/** One verb plus exactly the options that verb takes. */
export type Command =
  | { readonly kind: "overview"; readonly client?: string }
  | { readonly kind: "ops"; readonly path?: string; readonly client?: string; readonly sigs: boolean }
  | { readonly kind: "type"; readonly names: readonly string[]; readonly deps: boolean }
  | { readonly kind: "api" };

export interface CliArgs {
  readonly qualified: QualifiedName;
  readonly command: Command;
  readonly options: ResolveOptions;
}

function isVerb(token: string): token is Verb {
  return (VERBS as readonly string[]).includes(token);
}

function validation(message: string, suggestion: string): Result<CliArgs> {
  return err({ kind: "validation", message, suggestion });
}

/**
 * The flags, as `node:util`'s parser wants them.
 *
 * The standard library rather than a hand-rolled loop or a CLI dependency. It is
 * strictly better than the loop it replaced on two counts — it accepts
 * `--client=Client`, which the loop rejected as an unknown flag, and it catches
 * `--client --sigs` as an ambiguous value instead of silently taking `--sigs` as
 * the client name — and better than a dependency because this binary is baked into
 * the runner image, where every edge on the agent's critical path is one worth not
 * having. What a CLI framework would add is help generation and its own error
 * printing, and both are things this command has to own: `usage.ts` carries the
 * resolved cache directory, and every failure has to leave stderr holding exactly
 * one `Failure` object.
 */
const FLAGS = {
  client: { type: "string" },
  "project-dir": { type: "string" },
  sigs: { type: "boolean" },
  deps: { type: "boolean" },
  refresh: { type: "boolean" },
  help: { type: "boolean", short: "h" },
} as const;

const KNOWN_FLAGS = "--client, --project-dir, --sigs, --deps and --refresh";

/**
 * `node:util`'s parse errors, as this command's contract.
 *
 * Every one of them is exit 2 with a `suggestion`, because the recovery is always
 * an edit to the argument list. The thing that must NOT happen is an unrecognised
 * flag becoming a positional: that is how `--refresh` used to resolve as the
 * VERSION and report `package-not-found` at exit 1, which the skill teaches means
 * "Central could not answer, run it once more".
 */
function describeParseError(cause: unknown): Result<CliArgs> {
  const code = typeof cause === "object" && cause !== null && "code" in cause ? String(cause.code) : "";
  const message = cause instanceof Error ? cause.message.split("\n")[0] ?? "" : String(cause);
  if (code === "ERR_PARSE_ARGS_UNKNOWN_OPTION") {
    return validation(message, `Known flags are ${KNOWN_FLAGS}. Run with --help for usage.`);
  }
  if (code === "ERR_PARSE_ARGS_INVALID_OPTION_VALUE") {
    return validation(message, "Pass the value after the flag, or as --flag=value.");
  }
  return validation(message, `Run with --help for usage. Known flags are ${KNOWN_FLAGS}.`);
}

/** `null` means "print usage and stop" — `--help`, or no arguments at all. */
export function parseArgs(argv: readonly string[]): Result<CliArgs> | null {
  let parsed: ReturnType<typeof parseArgv<{ options: typeof FLAGS; allowPositionals: true; strict: true }>>;
  try {
    parsed = parseArgv({ args: [...argv], options: FLAGS, allowPositionals: true, strict: true });
  } catch (cause) {
    return describeParseError(cause);
  }

  if (parsed.values.help === true) return null;
  const client = parsed.values.client;
  const projectDir = parsed.values["project-dir"];
  const sigs = parsed.values.sigs === true;
  const deps = parsed.values.deps === true;
  const refresh = parsed.values.refresh === true;

  const [first, ...rest] = parsed.positionals;
  if (first === undefined) return null;

  // A package name has a slash and a verb does not. Anything else is a typo, and
  // saying which four verbs exist is more useful than reporting it as a bad
  // package name.
  const leadsWithPackage = first.includes("/");
  if (!leadsWithPackage && !isVerb(first)) {
    return validation(
      `'${first}' is neither a package name nor a verb.`,
      `A package name contains a slash, e.g. ballerinax/github. The verbs are ${VERBS.join(", ")}.`,
    );
  }
  // `isVerb(first)` is already established for the non-package branch by the guard
  // above, so the default verb is decided by the slash alone.
  const verb: Verb = leadsWithPackage ? "overview" : (first as Verb);
  const args = leadsWithPackage ? parsed.positionals : rest;

  const misused = rejectForeignFlags(verb, {
    sigs,
    deps,
    client: client !== undefined,
  });
  if (misused !== undefined) return misused;

  const [name, ...tail] = args;
  if (name === undefined) {
    return validation(`'${verb}' needs a package.`, `Pass 'org/name', e.g. bal-library ${verb} ballerinax/github.`);
  }
  const qualified = parseQualifiedName(name);
  if (!qualified.ok) return qualified;

  const command = buildCommand(verb, tail, {
    sigs,
    deps,
    ...(client === undefined ? {} : { client }),
  });
  if (!command.ok) return command;

  // A version positional exists only where it cannot be confused with something
  // else. `ops` takes a path there and `type` takes declaration names, so those
  // two pin a version through --project-dir, which is the form that matters after
  // a build anyway.
  const version = verb === "overview" ? tail[0] : undefined;

  return ok({
    qualified: qualified.value,
    command: command.value,
    options: {
      ...(version === undefined ? {} : { version }),
      ...(projectDir === undefined ? {} : { projectDir }),
      ...(refresh ? { refresh: true } : {}),
    },
  });
}

/**
 * Which flags each verb takes. `--project-dir` and `--refresh` are global and are
 * not listed.
 *
 * A flag a verb does not take must be a LOUD failure rather than a silently
 * ignored argument. `bal-library overview <pkg> --deps` used to exit 0 with the
 * flag dropped, which is the same silent class of mistake as an unknown flag
 * resolving to a version: the caller believes it asked for something it did not
 * get, and nothing in the output says otherwise. It is also exactly the
 * version-skew shape to worry about, since a newer skill reaching an older binary
 * differs from this only in which side is ahead.
 */
const VERB_FLAGS: Readonly<Record<Verb, readonly ("sigs" | "deps" | "client")[]>> = {
  overview: ["client"],
  ops: ["client", "sigs"],
  type: ["deps"],
  api: [],
};

/** Which verbs would have accepted a flag, for the suggestion. */
function verbsTaking(flag: "sigs" | "deps" | "client"): readonly Verb[] {
  return VERBS.filter((verb) => VERB_FLAGS[verb].includes(flag));
}

function rejectForeignFlags(
  verb: Verb,
  given: Readonly<Record<"sigs" | "deps" | "client", boolean>>,
): Result<CliArgs> | undefined {
  const allowed = VERB_FLAGS[verb];
  for (const flag of ["sigs", "deps", "client"] as const) {
    if (!given[flag] || allowed.includes(flag)) continue;
    const takers = verbsTaking(flag);
    return validation(
      `'${verb}' does not take --${flag}.`,
      `--${flag} belongs to ${takers.map((taker) => `'${taker}'`).join(" and ")}. Drop it, or use one of those verbs.`,
    );
  }
  return undefined;
}

function buildCommand(
  verb: Verb,
  tail: readonly string[],
  flags: { client?: string; sigs: boolean; deps: boolean },
): Result<Command> {
  switch (verb) {
    case "overview": {
      if (tail.length > 1) {
        return err({
          kind: "validation",
          message: `Unexpected argument '${tail[1] ?? ""}'.`,
          suggestion: "overview takes at most 'org/name' and a version.",
        });
      }
      return ok({ kind: "overview", ...(flags.client === undefined ? {} : { client: flags.client }) });
    }
    case "ops": {
      if (tail.length > 1) {
        return err({
          kind: "validation",
          message: `Unexpected argument '${tail[1] ?? ""}'.`,
          suggestion: "ops takes one path. Quote it if it contains a wildcard: 'repos/*/*'.",
        });
      }
      return ok({
        kind: "ops",
        sigs: flags.sigs,
        ...(tail[0] === undefined ? {} : { path: tail[0] }),
        ...(flags.client === undefined ? {} : { client: flags.client }),
      });
    }
    case "type": {
      if (tail.length === 0) {
        return err({
          kind: "validation",
          message: "type needs at least one declaration name.",
          suggestion: "Pass the name from a signature, e.g. bal-library type ballerinax/github FullRepository.",
        });
      }
      return ok({ kind: "type", names: tail, deps: flags.deps });
    }
    case "api": {
      if (tail.length > 0) {
        return err({
          kind: "validation",
          message: `Unexpected argument '${tail[0] ?? ""}'.`,
          suggestion: "api takes only 'org/name'. Pin a version with --project-dir.",
        });
      }
      return ok({ kind: "api" });
    }
    default: {
      const exhaustive: never = verb;
      return exhaustive;
    }
  }
}

export interface CliStreams {
  readonly out: (text: string) => void;
  readonly errorOut: (text: string) => void;
}

/**
 * Runs one invocation and returns its exit code.
 *
 * Streams, HTTP and the cache are injected rather than reached for, which is what
 * lets the tests drive the real command — argument parsing, stream discipline and
 * exit codes together — against a recorded payload and a temporary directory
 * instead of Central and `$HOME`.
 */
export async function run(argv: readonly string[], streams: CliStreams, http: HttpOptions = {}): Promise<number> {
  const parsed = parseArgs(argv);
  if (parsed === null) {
    streams.errorOut(usage((http.cache ?? NULL_CACHE).describe()));
    return 2;
  }
  if (!parsed.ok) return fail(parsed.error, streams);

  const { qualified, command, options } = parsed.value;
  const loaded = await loadPackage(qualified, { ...http, ...options });
  if (!loaded.ok) return fail(loaded.error, streams);

  const document = renderDocument(command, loaded.value);
  if (!document.ok) return fail(document.error, streams);

  streams.out(document.value);
  return 0;
}

function renderDocument(command: Command, loaded: LoadedPackage): Result<string> {
  switch (command.kind) {
    case "overview":
      return ok(renderOverview(loaded, command.client === undefined ? {} : { client: command.client }));
    case "ops":
      return renderOpsView(loaded, {
        sigs: command.sigs,
        ...(command.path === undefined ? {} : { path: command.path }),
        ...(command.client === undefined ? {} : { client: command.client }),
      });
    case "type":
      return renderTypeView(loaded, { names: command.names, deps: command.deps });
    case "api":
      // Two provenance lines rather than one: `api` is the code register, where a
      // comment is the only thing a Ballerina file can carry, and a document left
      // over from an earlier lookup is otherwise indistinguishable from a fresh
      // one.
      return ok(
        `// Resolved: ${loaded.qualified.org}/${loaded.qualified.name}:${loaded.version}\n` +
          `// Source: ${loaded.provenance}\n${toSyntaxString(loaded.library)}`,
      );
    default: {
      const exhaustive: never = command;
      return exhaustive;
    }
  }
}

function fail(failure: Failure, streams: CliStreams): number {
  streams.errorOut(`${describeFailure(failure)}\n`);
  return exitCodeFor(failure);
}

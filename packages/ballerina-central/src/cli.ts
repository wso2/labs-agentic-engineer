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

/** `null` means "print usage and stop" — `--help`, or no arguments at all. */
export function parseArgs(argv: readonly string[]): Result<CliArgs> | null {
  const positional: string[] = [];
  let client: string | undefined;
  let projectDir: string | undefined;
  let sigs = false;
  let deps = false;
  let refresh = false;

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === undefined) continue;
    if (arg === "-h" || arg === "--help") return null;
    if (arg === "--sigs") {
      sigs = true;
      continue;
    }
    if (arg === "--deps") {
      deps = true;
      continue;
    }
    if (arg === "--refresh") {
      refresh = true;
      continue;
    }
    if (arg === "--client" || arg === "--project-dir") {
      const value = argv[++i];
      if (value === undefined || value.startsWith("--")) {
        return validation(
          `${arg} needs a value.`,
          arg === "--client"
            ? "Pass a client name, e.g. --client Client."
            : "Pass the component directory, e.g. --project-dir stars-api.",
        );
      }
      if (arg === "--client") client = value;
      else projectDir = value;
      continue;
    }
    // An unrecognised flag must never become a positional. Silently absorbing one
    // is how `--refresh` used to resolve as the VERSION and report a Central
    // failure at exit 1, which the skill teaches means "run it once more".
    if (arg.startsWith("-") && arg !== "-") {
      return validation(
        `Unknown flag '${arg}'.`,
        "Known flags are --client, --project-dir, --sigs, --deps and --refresh. Run with --help for usage.",
      );
    }
    positional.push(arg);
  }

  const [first, ...rest] = positional;
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
  const verb: Verb = leadsWithPackage || !isVerb(first) ? "overview" : first;
  const args = leadsWithPackage ? positional : rest;

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

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
 * The stream discipline is the contract, and it is what lets the skill say
 * `bal-library ballerinax/github > /tmp/github-api.bal` and get a file holding
 * nothing but Ballerina:
 *
 *   stdout   the requested document, and nothing else — no progress, no banner.
 *            Ballerina source, or Markdown under `--readme`
 *   stderr   on failure, one JSON object matching `Failure`
 *   exit 0   success
 *   exit 1   Central could not answer, or the package published no guide —
 *            retryable in principle, and never a licence to guess a signature
 *   exit 2   the arguments are wrong, or `--help`
 */

import type { HttpOptions } from "./central/client.js";
import { renderLibrary, renderReadme, type ResolveOptions } from "./library.js";
import { parseQualifiedName, type QualifiedName } from "./qualified.js";
import { describeFailure, err, exitCodeFor, ok, type Failure, type Result } from "./result.js";

const USAGE = `Usage: bal-library <org/name> [version] [--readme] [--project-dir <dir>]

Print a Ballerina package's whole public API — clients, resource and remote
functions, records, enums, unions, services, annotations — as Ballerina source
on stdout.

  <org/name>            Package WITHOUT a version suffix, e.g. ballerinax/github
  [version]             Optional. Omitted, the version is taken from
                        --project-dir's Dependencies.toml when it locks the
                        package, and from Central's latest otherwise.
  --readme              Print the package's own guide as Markdown instead of its
                        API. It leads with runnable usage samples, and answers
                        "how is this used" where the API answers "what is it
                        called".
  --project-dir <dir>   A component directory a build has resolved.

Redirect it to a file: the output is one file per package and runs to tens of
thousands of lines, ordered types -> clients -> services, so paging it shows
types and never the client.

  bal-library ballerinax/github > /tmp/github-api.bal
  grep -n 'client class' /tmp/github-api.bal
  bal-library ballerinax/github --readme > /tmp/github-readme.md
`;

export interface CliArgs {
  readonly qualified: QualifiedName;
  readonly options: ResolveOptions;
  /** Which of the package's two documents to print. */
  readonly document: "api" | "readme";
}

/** `null` means "print usage and stop" — `--help`, or no arguments at all. */
export function parseArgs(argv: readonly string[]): Result<CliArgs> | null {
  const positional: string[] = [];
  let projectDir: string | undefined;
  let document: CliArgs["document"] = "api";

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "-h" || arg === "--help") return null;
    if (arg === "--readme") {
      document = "readme";
      continue;
    }
    if (arg === "--project-dir") {
      const value = argv[++i];
      if (value === undefined) {
        return err({
          kind: "validation",
          message: "--project-dir needs a directory.",
          suggestion: "Pass the component directory, e.g. --project-dir stars-api.",
        });
      }
      projectDir = value;
      continue;
    }
    if (arg !== undefined) positional.push(arg);
  }

  const [name, version, ...extra] = positional;
  if (name === undefined) return null;
  if (extra.length > 0) {
    return err({
      kind: "validation",
      message: `Unexpected argument '${extra[0] ?? ""}'.`,
      suggestion: "Pass at most 'org/name' and a version.",
    });
  }

  const qualified = parseQualifiedName(name);
  if (!qualified.ok) return qualified;

  return ok({
    qualified: qualified.value,
    document,
    options: {
      ...(version === undefined ? {} : { version }),
      ...(projectDir === undefined ? {} : { projectDir }),
    },
  });
}

export interface CliStreams {
  readonly out: (text: string) => void;
  readonly errorOut: (text: string) => void;
}

/**
 * Runs one invocation and returns its exit code.
 *
 * Streams and HTTP are injected rather than reached for, which is what lets the
 * tests drive the real command — argument parsing, stream discipline and exit
 * codes together — against a recorded payload instead of Central.
 */
export async function run(argv: readonly string[], streams: CliStreams, http: HttpOptions = {}): Promise<number> {
  const parsed = parseArgs(argv);
  if (parsed === null) {
    streams.errorOut(USAGE);
    return 2;
  }
  if (!parsed.ok) return fail(parsed.error, streams);

  const { qualified, document, options } = parsed.value;
  const render = document === "readme" ? renderReadme : renderLibrary;
  const rendered = await render(qualified, { ...http, ...options });
  if (!rendered.ok) return fail(rendered.error, streams);

  streams.out(rendered.value);
  return 0;
}

function fail(failure: Failure, streams: CliStreams): number {
  streams.errorOut(`${describeFailure(failure)}\n`);
  return exitCodeFor(failure);
}

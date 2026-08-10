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
 * The usage text, kept out of `cli.ts` so the dispatcher stays about dispatch.
 *
 * It goes to STDERR, along with the `--help` exit code of 2, because stdout is
 * the document and nothing else. That also makes it the one place the cache is
 * allowed to speak: it already sits outside both the document and the `Failure`
 * contract, so printing the resolved cache directory here is how an operator
 * proves the cache is alive inside a runner image without parsing anything.
 */

export const VERBS = ["overview", "ops", "type", "api"] as const;
export type Verb = (typeof VERBS)[number];

export function usage(cacheDescription: string): string {
  return `Usage: bal-library <org/name> [version]                       the overview (default)
       bal-library overview <org/name> [version] [--client C]
       bal-library ops  <org/name> [path] [--client C] [--sigs]
       bal-library type <org/name> <Name>... [--deps]
       bal-library api  <org/name>

Read a Ballerina package off Central. Four documents, addressed rather than
grepped.

  overview   Readme, every client's signatures, module functions and error
             declarations. No other types — they are 80% of a large package.
             A client with more than 100 resource functions is replaced by its
             path tree, which 'ops' navigates.
  ops        A client's resource functions by path. Without --sigs it is one
             level of the tree: what is callable here, and the segments below.
             With --sigs it is every signature at or under the path. '*' is a
             wildcard segment, so 'repos/*/*' addresses a level directly.
  type       One or more declarations, as Ballerina. --deps appends the
             transitive same-package closure and names cross-package edges.
  api        Every declaration in one document. The fallback for the question
             the three above did not answer.

  --client <Name>       Pick a client. Required by 'ops' when more than one
                        client declares resource functions.
  --project-dir <dir>   A component directory a build has resolved. Its
                        Dependencies.toml pins the version the code compiles
                        against, which is not always Central's latest.
  --refresh             Ignore any cached copy and rewrite it.
  -h, --help            This text.

Version resolution: an explicit version argument, then --project-dir's
Dependencies.toml, then Central's latest.

  bal-library ballerinax/github
  bal-library ops ballerinax/github repos
  bal-library ops ballerinax/github 'repos/*/*' --sigs
  bal-library type ballerina/http ClientRequestError --deps

Streams: stdout is the requested document and nothing else. stderr is one JSON
object on failure. Exit 1 means Central could not answer; exit 2 means the
arguments were wrong.

Environment:
  BAL_LIBRARY_CACHE=off        disable the on-disk cache
  BAL_LIBRARY_CACHE_DIR=<dir>  put it somewhere else
Cache: ${cacheDescription}
`;
}

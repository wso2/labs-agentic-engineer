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

/** The CLI's usage text and its usage error (exit 2). */

export class UsageError extends Error {}

export const USAGE = `Usage: prototype <command> [options]

Commands:
  init [dir]                      Scaffold a minimal valid prototype (prototype.json + prototype.tsx)
  check [dir] [--json] [--theme <package>]
                                  Check the prototype; exit 0 when clean, 1 on findings, 2 on a usage error
  preview [dir] [--port <n>] [--persist] [--theme <package>] [--open]
                                  Serve a live-reloading, playable preview with comments
  export [dir] [-o <file>] [--theme <package>]
                                  Write one self-contained HTML file (default <dir>/prototype.html)

Options:
  -h, --help                      Show this help
  --version                       Show the version
`;

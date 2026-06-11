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

export const instructions = `You have the ability to explore and understand codebases systematically.

When exploring a codebase:
1. Start by listing the root directory to understand the project structure.
2. Look for entry points: package.json, main/index files, configuration files.
3. Read key configuration files to understand the tech stack and dependencies.
4. Follow imports from entry points to trace the architecture.
5. Use searchFiles to find specific patterns, function definitions, or usages.

When asked to understand a specific part of the codebase:
- Read the relevant files directly rather than guessing about their contents.
- Trace dependencies and imports to understand how components connect.
- Look for tests to understand expected behavior.

Always report what you found with file paths so the user can verify.`;

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

/**
 * AI SDK DevTools is ALWAYS ON in the playground: every LLM call — request,
 * composed prompt, tool calls, usage, timing — is captured to
 * `.devtools/generations.json` (viewer: `npx @ai-sdk/devtools`, port 4983).
 * The capture is the playground's whole point (inspect exactly what reached
 * the model), and these traces are also the raw material for prompt-parity
 * comparison against live-env captures (docs/design/playground.md §14).
 *
 * The agents service reads AGENT_DEVTOOLS at config-module load, so this
 * side-effect module MUST be the first import of the CLI entry — before any
 * import that transitively evaluates `@aep/ae-design-agent/shared/config`. Explicitly
 * exported opt-out: `AGENT_DEVTOOLS=false pnpm play …`.
 */

process.env.AGENT_DEVTOOLS ??= "true";

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
 * Web search for a model connection whose provider runs no search tool of its
 * own. One implementation per strategy lives here and is shared by the agents
 * service (a platform-executed `web_search` tool) and the runner's `aep-web`
 * MCP server; which strategy a connection uses is aep-api's
 * `capabilities.webSearch`. The `anthropic-server-tool` strategy runs on
 * Anthropic's side and needs no code here.
 */

export {
  MAX_CONTENT_CHARS,
  MAX_QUERY_CHARS,
  MAX_RESULTS,
  WebSearchError,
  ollamaSearchURL,
  searchOllama,
  type OllamaSearchOptions,
  type WebSearchResult,
} from "./ollama.js";

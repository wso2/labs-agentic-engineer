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

import { parseToc } from './parseToc.js';

/**
 * Derive a human-friendly document title. Uses the first H1 in the content
 * if one exists; otherwise the filename with any trailing `.md` / `.markdown`
 * extension stripped.
 */
export function getDocTitle(path: string, markdown: string | undefined): string {
  if (markdown) {
    const toc = parseToc(markdown);
    const firstH1 = toc.find((e) => e.level === 1);
    if (firstH1 && firstH1.text) return firstH1.text;
  }
  return path.replace(/\.(md|markdown)$/i, '');
}

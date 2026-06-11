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

import type { Editor } from '@tiptap/core';

/**
 * Scroll the Nth heading of a TipTap editor into view. The index corresponds
 * to the 0-based position among all heading nodes in the document, matching
 * {@link parseToc}.
 */
export function scrollToHeading(editor: Editor, headingIndex: number): void {
  if (headingIndex < 0) return;

  let current = 0;
  let targetPos = -1;
  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === 'heading') {
      if (current === headingIndex) {
        targetPos = pos;
        return false;
      }
      current++;
    }
    return true;
  });

  if (targetPos < 0) return;

  const dom = editor.view.nodeDOM(targetPos);
  if (dom instanceof HTMLElement) {
    dom.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
}

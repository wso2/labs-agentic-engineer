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

/** CSS styles for diff marks and decorations in the TipTap editor. */
export const diffContentStyles: Record<string, Record<string, string>> = {
  // Mark-based styles (MdDiffViewer)
  '.tiptap ins.diff-added': {
    backgroundColor: 'rgba(34, 139, 34, 0.15)',
    color: '#22633a',
    textDecoration: 'none',
  },
  '.tiptap del.diff-removed': {
    backgroundColor: 'rgba(220, 38, 38, 0.15)',
    color: '#991b1b',
    textDecoration: 'line-through',
  },
  // Decoration-based styles (inline diff mode)
  '.tiptap .diff-added': {
    backgroundColor: 'rgba(34, 139, 34, 0.15)',
    borderRadius: '2px',
  },
  '.tiptap .diff-removed-widget': {
    backgroundColor: 'rgba(220, 38, 38, 0.15)',
    color: '#991b1b',
    textDecoration: 'line-through',
    userSelect: 'none',
    pointerEvents: 'none',
    borderRadius: '2px',
  },
};

/** Convert diff style object to CSS string. */
export function diffStylesToCss(): string {
  const lines: string[] = [];
  for (const [selector, props] of Object.entries(diffContentStyles)) {
    lines.push(`${selector} {`);
    for (const [prop, val] of Object.entries(props)) {
      const kebab = prop.replace(/[A-Z]/g, (m) => `-${m.toLowerCase()}`);
      lines.push(`  ${kebab}: ${val};`);
    }
    lines.push('}');
  }
  return lines.join('\n');
}

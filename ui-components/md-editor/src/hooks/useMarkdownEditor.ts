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

import { useEditor } from '@tiptap/react';
import { createExtensions } from '../extensions/index.js';
import type { CollabConfig } from '../extensions/index.js';

export interface UseMarkdownEditorOptions {
  content?: string;
  placeholder?: string;
  editable?: boolean;
  autoFocus?: boolean;
  onUpdate?: (markdown: string) => void;
  onBlur?: (markdown: string) => void;
  getBaseMarkdown?: () => string | undefined;
  collab?: CollabConfig;
}

export function useMarkdownEditor(options: UseMarkdownEditorOptions) {
  const {
    content = '',
    placeholder,
    editable = true,
    autoFocus = false,
    onUpdate,
    onBlur,
    getBaseMarkdown,
    collab,
  } = options;

  const editor = useEditor({
    extensions: createExtensions({ placeholder, getBaseMarkdown, collab }),
    // In collab mode the document is owned by Y.js; passing initial content
    // here would race with the CRDT state. The page is responsible for seeding
    // an empty fragment via editor.commands.setContent once.
    content: collab ? undefined : (content || undefined),
    contentType: 'markdown',
    editable,
    autofocus: autoFocus ? 'end' : false,
    onUpdate: ({ editor: e }) => {
      onUpdate?.(e.getMarkdown());
    },
    onBlur: ({ editor: e }) => {
      onBlur?.(e.getMarkdown());
    },
  });

  return editor;
}

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

import StarterKit from '@tiptap/starter-kit';
import Link from '@tiptap/extension-link';
import Placeholder from '@tiptap/extension-placeholder';
import { Markdown } from '@tiptap/markdown';
import { Collaboration } from '@tiptap/extension-collaboration';
import { CollaborationCaret } from '@tiptap/extension-collaboration-caret';
import type { Extensions } from '@tiptap/react';
import type { Doc } from 'yjs';
import { DiffAdded, DiffRemoved } from '../diff/diffMarks.js';
import { DiffDecorations } from './diffDecorations.js';

export interface CollabConfig {
  ydoc: Doc;
  provider: { awareness: unknown };
  user: { name: string; color: string };
}

export function createExtensions(options: {
  placeholder?: string;
  includeDiffMarks?: boolean;
  getBaseMarkdown?: () => string | undefined;
  collab?: CollabConfig;
}): Extensions {
  const extensions: Extensions = [
    // StarterKit's link conflicts with our configured Link below; undoRedo
    // must be off when Y.js owns undo (Collaboration extension below).
    StarterKit.configure({
      link: false,
      ...(options.collab ? { undoRedo: false } : {}),
    }),
    Link.configure({
      openOnClick: false,
      HTMLAttributes: {
        rel: 'noopener noreferrer',
        target: '_blank',
      },
    }),
    Placeholder.configure({
      placeholder: options.placeholder ?? 'Write something...',
    }),
    Markdown,
  ];

  if (options.includeDiffMarks) {
    extensions.push(DiffAdded, DiffRemoved);
  }

  // The extension list is fixed at editor creation, so we always register
  // DiffDecorations with a getter — returning `undefined` makes it a no-op.
  extensions.push(
    DiffDecorations.configure({
      getBaseMarkdown: options.getBaseMarkdown ?? (() => undefined),
    }),
  );

  if (options.collab) {
    const { ydoc, provider, user } = options.collab;
    extensions.push(
      Collaboration.configure({ document: ydoc }),
      CollaborationCaret.configure({ provider, user }),
    );
  }

  return extensions;
}

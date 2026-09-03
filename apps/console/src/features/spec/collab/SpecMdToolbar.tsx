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

import type { ReactNode } from "react";
import {
  Divider,
  IconButton,
  Stack,
  ToggleButton,
  Tooltip,
} from "@wso2/oxygen-ui";
import {
  Bold,
  Code,
  Heading1,
  Heading2,
  Heading3,
  Italic,
  List,
  ListOrdered,
  Redo2,
  SquareCode,
  Strikethrough,
  TextQuote,
  Undo2,
} from "@wso2/oxygen-ui-icons-react";
import { useEditorState, type Editor } from "@tiptap/react";

// Formatting toolbar for SpecMdEditor (#206). Everything here maps 1:1 onto
// StarterKit marks/nodes already in the collab schema (packages/collab-doc
// serializes with the same StarterKit, so the whole set round-trips to
// markdown). Undo/redo go through Collaboration's Yjs-backed commands —
// StarterKit history is disabled because Yjs owns history.

const MOD = navigator.platform.startsWith("Mac") ? "⌘" : "Ctrl+";

function ToolbarToggle({
  title,
  active,
  onToggle,
  children,
}: {
  title: string;
  active: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <Tooltip title={title}>
      <ToggleButton
        value={title}
        size="small"
        selected={active}
        onChange={onToggle}
        sx={{ border: 0, px: 0.75 }}
      >
        {children}
      </ToggleButton>
    </Tooltip>
  );
}

export function SpecMdToolbar({ editor }: { editor: Editor }) {
  const state = useEditorState({
    editor,
    selector: ({ editor: e }) => ({
      bold: e.isActive("bold"),
      italic: e.isActive("italic"),
      strike: e.isActive("strike"),
      code: e.isActive("code"),
      bulletList: e.isActive("bulletList"),
      orderedList: e.isActive("orderedList"),
      blockquote: e.isActive("blockquote"),
      codeBlock: e.isActive("codeBlock"),
      h1: e.isActive("heading", { level: 1 }),
      h2: e.isActive("heading", { level: 2 }),
      h3: e.isActive("heading", { level: 3 }),
      canUndo: e.can().undo(),
      canRedo: e.can().redo(),
    }),
  });

  const chain = () => editor.chain().focus();

  return (
    <Stack
      direction="row"
      alignItems="center"
      spacing={0.25}
      sx={{
        flexShrink: 0,
        px: 0.75,
        py: 0.5,
        borderBottom: 1,
        borderColor: "divider",
      }}
    >
      <ToolbarToggle
        title={`Heading 1 (${MOD}Alt+1)`}
        active={state.h1}
        onToggle={() => chain().toggleHeading({ level: 1 }).run()}
      >
        <Heading1 size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Heading 2 (${MOD}Alt+2)`}
        active={state.h2}
        onToggle={() => chain().toggleHeading({ level: 2 }).run()}
      >
        <Heading2 size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Heading 3 (${MOD}Alt+3)`}
        active={state.h3}
        onToggle={() => chain().toggleHeading({ level: 3 }).run()}
      >
        <Heading3 size={16} />
      </ToolbarToggle>
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      <ToolbarToggle
        title={`Bold (${MOD}B)`}
        active={state.bold}
        onToggle={() => chain().toggleBold().run()}
      >
        <Bold size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Italic (${MOD}I)`}
        active={state.italic}
        onToggle={() => chain().toggleItalic().run()}
      >
        <Italic size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Strikethrough (${MOD}Shift+S)`}
        active={state.strike}
        onToggle={() => chain().toggleStrike().run()}
      >
        <Strikethrough size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Inline code (${MOD}E)`}
        active={state.code}
        onToggle={() => chain().toggleCode().run()}
      >
        <Code size={16} />
      </ToolbarToggle>
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      <ToolbarToggle
        title={`Bullet list (${MOD}Shift+8)`}
        active={state.bulletList}
        onToggle={() => chain().toggleBulletList().run()}
      >
        <List size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Ordered list (${MOD}Shift+7)`}
        active={state.orderedList}
        onToggle={() => chain().toggleOrderedList().run()}
      >
        <ListOrdered size={16} />
      </ToolbarToggle>
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      <ToolbarToggle
        title={`Blockquote (${MOD}Shift+B)`}
        active={state.blockquote}
        onToggle={() => chain().toggleBlockquote().run()}
      >
        <TextQuote size={16} />
      </ToolbarToggle>
      <ToolbarToggle
        title={`Code block (${MOD}Alt+C)`}
        active={state.codeBlock}
        onToggle={() => chain().toggleCodeBlock().run()}
      >
        <SquareCode size={16} />
      </ToolbarToggle>
      <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />
      <Tooltip title={`Undo (${MOD}Z)`}>
        <span>
          <IconButton
            size="small"
            disabled={!state.canUndo}
            // Without `scrollIntoView: false` the focus chases the caret —
            // and a Yjs undo lands as a whole-document replace, which clamps
            // the mapped caret to the end, so pressing Undo threw the reader
            // to the bottom of the document. The reader is already looking at
            // the place they are undoing; keep them there.
            onClick={() => editor.chain().focus(null, { scrollIntoView: false }).undo().run()}
            aria-label="Undo"
          >
            <Undo2 size={16} />
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title={`Redo (${MOD}Shift+Z)`}>
        <span>
          <IconButton
            size="small"
            disabled={!state.canRedo}
            onClick={() => editor.chain().focus(null, { scrollIntoView: false }).redo().run()}
            aria-label="Redo"
          >
            <Redo2 size={16} />
          </IconButton>
        </span>
      </Tooltip>
    </Stack>
  );
}
